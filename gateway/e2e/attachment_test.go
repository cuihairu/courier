// support.md v1.1「工单附件」(F44)验收:申请直传返回短时效预签名 URL、详情
// attachments[] 回显、限额/上限/归属/状态与存储 fail-closed。存储面是确定性
// fake Signer(生产 main.go 不内置假存储)。
package e2e

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cuihairu/courier/gateway/auth"
	"github.com/cuihairu/courier/gateway/middleware"
	"github.com/cuihairu/courier/gateway/providers"
	"github.com/cuihairu/courier/gateway/providers/account"
	"github.com/cuihairu/courier/gateway/providers/croupier"
	"github.com/cuihairu/courier/gateway/routing"
)

// staticSigner 确定性预签名:URL 含 key 与 ttl,headers 回显签名字段。
type staticSigner struct {
	bases []string
}

func (s *staticSigner) SignPresign(base string, ttl time.Duration) (string, map[string]string) {
	s.bases = append(s.bases, base)
	return "https://storage.local/" + base + "?ttl=" + ttl.String() + "&sig=static",
		map[string]string{"X-Storage-Signature": "sig-" + base}
}

type attachmentHarness struct {
	srv      *httptest.Server
	croupier *croupier.SupportProvider
}

func newAttachmentHarness(t *testing.T, signer croupier.Signer) *attachmentHarness {
	t.Helper()
	reg := providers.NewRegistry()
	acc := account.New(account.Options{Iterations: 1000})
	reqAuth := auth.RequireAuth(acc.Verifier())
	// 附件限额取契约缺省(3 个 / 10 MiB),TTL 缩到 2 分钟便于断言 expiresAt 偏移。
	cp := croupier.New(croupier.Options{
		RequireAuth:      reqAuth,
		AttachmentSigner: signer,
		AttachmentTTL:    2 * time.Minute,
	})
	if err := reg.Register(acc); err != nil {
		t.Fatalf("register account: %v", err)
	}
	if err := reg.Register(cp); err != nil {
		t.Fatalf("register croupier: %v", err)
	}
	cfg := providers.Config{
		providers.CapIdentity: {Primary: account.DefaultName},
		providers.CapSupport:  {Primary: croupier.DefaultName},
	}
	chain := middleware.Chain(middleware.NewRateLimiter(1000, 1000))
	srv := httptest.NewServer(routing.New(reg, cfg, chain))
	t.Cleanup(srv.Close)
	return &attachmentHarness{srv: srv, croupier: cp}
}

// guestToken 游客登录取 token(deviceID 不同 = 不同账号)。
func (h *attachmentHarness) guestToken(t *testing.T, deviceID string) string {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, h.srv.URL+"/v1/identity/guest",
		strings.NewReader(`{"deviceId":"`+deviceID+`"}`))
	h.scope(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("guest: %v", err)
	}
	defer resp.Body.Close()
	var out struct {
		Data struct {
			AccessToken string `json:"accessToken"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("guest decode: %v", err)
	}
	if resp.StatusCode != http.StatusOK || out.Data.AccessToken == "" {
		t.Fatalf("guest status=%d", resp.StatusCode)
	}
	return out.Data.AccessToken
}

func (h *attachmentHarness) scope(r *http.Request) {
	r.Header.Set("X-Courier-Game-Id", "game_demo")
	r.Header.Set("X-Courier-Env", "prod")
}

// call 带 scope 与可选 Bearer 的请求,返回状态码与响应体。
func (h *attachmentHarness) call(t *testing.T, method, path, body, bearer string) (int, string) {
	t.Helper()
	var rd *strings.Reader
	if body == "" {
		rd = strings.NewReader("")
	} else {
		rd = strings.NewReader(body)
	}
	req, _ := http.NewRequest(method, h.srv.URL+path, rd)
	h.scope(req)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("读响应: %v", err)
	}
	return resp.StatusCode, string(raw)
}

type envBody struct {
	Data  json.RawMessage `json:"data"`
	Error struct {
		Code      string `json:"code"`
		Retryable bool   `json:"retryable"`
	} `json:"error"`
}

func mustEnv(t *testing.T, body string) envBody {
	t.Helper()
	var e envBody
	if err := json.Unmarshal([]byte(body), &e); err != nil {
		t.Fatalf("响应非 JSON: %v (raw=%s)", err, body)
	}
	return e
}

// TestAttachmentUpload_V11 附件全链路:申请直传 / 详情回显 / 限额 / 上限 /
// 归属 / 状态 / contentType 校验。
func TestAttachmentUpload_V11(t *testing.T) {
	signer := &staticSigner{}
	h := newAttachmentHarness(t, signer)
	tok := h.guestToken(t, "att-dev-1")

	// 建单(第 4 个附件前的载体)。
	status, body := h.call(t, http.MethodPost, "/v1/support/tickets",
		`{"title":"闪退反馈","body":"启动即崩溃"}`, tok)
	if status != http.StatusOK {
		t.Fatalf("create status=%d body=%s", status, body)
	}
	e := mustEnv(t, body)
	var tkt struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(e.Data, &tkt); err != nil {
		t.Fatalf("create decode: %v", err)
	}
	base := "/v1/support/tickets/" + tkt.ID + "/attachments"

	// 1) 申请直传:attachmentId / uploadUrl / method PUT / headers / expiresAt。
	status, body = h.call(t, http.MethodPost, base,
		`{"fileName":"crash.log","contentType":"text/plain","sizeBytes":1200,"kind":"LOG"}`, tok)
	if status != http.StatusOK {
		t.Fatalf("upload status=%d body=%s", status, body)
	}
	var up struct {
		AttachmentID string            `json:"attachmentId"`
		UploadURL    string            `json:"uploadUrl"`
		Method       string            `json:"method"`
		Headers      map[string]string `json:"headers"`
		ExpiresAt    string            `json:"expiresAt"`
	}
	if err := json.Unmarshal(mustEnv(t, body).Data, &up); err != nil {
		t.Fatalf("decode: %v (raw=%s)", err, body)
	}
	if !strings.HasPrefix(up.AttachmentID, "att_") {
		t.Fatalf("attachmentId = %s", up.AttachmentID)
	}
	if !strings.HasPrefix(up.UploadURL, "https://storage.local/support/uploads/") ||
		!strings.Contains(up.UploadURL, "?ttl=2m") {
		t.Fatalf("uploadUrl = %s", up.UploadURL)
	}
	if up.Method != "PUT" || len(up.Headers) == 0 || !strings.Contains(up.Headers["X-Storage-Signature"], "att_") {
		t.Fatalf("method/headers = %s / %v", up.Method, up.Headers)
	}
	if up.ExpiresAt == "" {
		t.Fatal("expiresAt 为空")
	}

	// 2) 详情 attachments[] 回显(不返回存储路径)。
	status, body = h.call(t, http.MethodGet, "/v1/support/tickets/"+tkt.ID, "", tok)
	if status != http.StatusOK {
		t.Fatalf("detail status=%d", status)
	}
	var det struct {
		Ticket struct {
			Status string `json:"status"`
		} `json:"ticket"`
		Attachments []struct {
			ID          string `json:"id"`
			FileName    string `json:"fileName"`
			ContentType string `json:"contentType"`
			SizeBytes   int64  `json:"sizeBytes"`
			Kind        string `json:"kind"`
			CreatedAt   string `json:"createdAt"`
			DownloadURL string `json:"downloadUrl"`
		} `json:"attachments"`
	}
	if err := json.Unmarshal(mustEnv(t, body).Data, &det); err != nil {
		t.Fatalf("detail decode: %v", err)
	}
	if len(det.Attachments) != 1 || det.Attachments[0].ID != up.AttachmentID ||
		det.Attachments[0].FileName != "crash.log" || det.Attachments[0].ContentType != "text/plain" ||
		det.Attachments[0].SizeBytes != 1200 || det.Attachments[0].Kind != "LOG" {
		t.Fatalf("attachments[0] = %+v", det.Attachments[0])
	}
	if !strings.HasPrefix(det.Attachments[0].DownloadURL, "https://storage.local/") {
		t.Fatal("downloadUrl 应为预签名 URL")
	}

	// 3) 每工单 ≤ 3 附件:第 2/3 个成功(第 1 个已建),第 4 个 409 SUPPORT_ATTACHMENT_LIMIT。
	for i, size := range []int{100, 200} {
		req := `{"fileName":"shot` + string(rune('0'+i)) + `.png","contentType":"image/png","sizeBytes":` +
			strconv.Itoa(size) + `,"kind":"IMAGE"}`
		if st, b := h.call(t, http.MethodPost, base, req, tok); st != http.StatusOK {
			t.Fatalf("附件 #%d: status=%d body=%s", i+1, st, b)
		}
	}
	st, b := h.call(t, http.MethodPost, base,
		`{"fileName":"fourth.png","contentType":"image/png","sizeBytes":100}`, tok)
	if st != http.StatusConflict || !strings.Contains(b, "SUPPORT_ATTACHMENT_LIMIT") ||
		!strings.Contains(b, `"retryable":false`) {
		t.Fatalf("第 4 个: status=%d body=%s", st, b)
	}

	// 4) 单文件上限(契约缺省 10 MiB):413 SUPPORT_ATTACHMENT_TOO_LARGE。
	st, b = h.call(t, http.MethodPost, "/v1/support/tickets",
		`{"title":"大文件","body":"超限"}`, tok)
	if st != http.StatusOK {
		t.Fatalf("create2 status=%d", st)
	}
	tkt2 := mustEnv(t, b)
	var t2 struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(tkt2.Data, &t2); err != nil {
		t.Fatalf("decode: %v", err)
	}
	st, b = h.call(t, http.MethodPost, "/v1/support/tickets/"+t2.ID+"/attachments",
		`{"fileName":"big.bin","contentType":"application/zip","sizeBytes":10485761}`, tok)
	if st != http.StatusRequestEntityTooLarge || !strings.Contains(b, "SUPPORT_ATTACHMENT_TOO_LARGE") ||
		!strings.Contains(b, `"retryable":false`) {
		t.Fatalf("超限: status=%d body=%s", st, b)
	}

	// 5) 已关闭工单:409 SUPPORT_TICKET_CLOSED。
	if err := h.croupier.AgentClose(tkt.ID); err != nil {
		t.Fatalf("AgentClose: %v", err)
	}
	st, b = h.call(t, http.MethodPost, base,
		`{"fileName":"after-close.log","contentType":"text/plain","sizeBytes":10}`, tok)
	if st != http.StatusConflict || !strings.Contains(b, "SUPPORT_TICKET_CLOSED") {
		t.Fatalf("已关闭: status=%d body=%s", st, b)
	}

	// 6) 他人工单:404 SUPPORT_TICKET_NOT_FOUND(不泄露存在性)。
	st, b = h.call(t, http.MethodPost, base,
		`{"fileName":"hack.png","contentType":"image/png","sizeBytes":10}`,
		h.guestToken(t, "att-dev-2"))
	if st != http.StatusNotFound || !strings.Contains(b, "SUPPORT_TICKET_NOT_FOUND") {
		t.Fatalf("他人工单: status=%d body=%s", st, b)
	}

	// 7) contentType 越界:400 COMMON_INVALID_ARGUMENT。
	st, b = h.call(t, http.MethodPost, base,
		`{"fileName":"x.exe","contentType":"application/x-executable","sizeBytes":10}`, tok)
	if st != http.StatusBadRequest || !strings.Contains(b, "COMMON_INVALID_ARGUMENT") {
		t.Fatalf("contentType 越界: status=%d body=%s", st, b)
	}
	// fileName 越界(>255)同样 400。
	st, b = h.call(t, http.MethodPost, base,
		`{"fileName":"`+strings.Repeat("f", 256)+`","contentType":"image/png","sizeBytes":10}`, tok)
	if st != http.StatusBadRequest || !strings.Contains(b, "COMMON_INVALID_ARGUMENT") {
		t.Fatalf("fileName 越界: status=%d body=%s", st, b)
	}

	// 直传面未占网关流量:全测试只经附件端点申请,未发生对象 PUT 到网关。
	seen := map[string]bool{}
	for _, b := range signer.bases { // 每附件签 PUT 与 GET 各一次,共 3 个 key
		seen[b] = true
	}
	if len(signer.bases) != 6 || len(seen) != 3 {
		t.Fatalf("签名次数 = %d / distinct key = %d,期望 6 / 3", len(signer.bases), len(seen))
	}
}

// TestAttachmentSignerMissing_V11 未配置存储:fail-closed 501,不落登记。
func TestAttachmentSignerMissing_V11(t *testing.T) {
	h := newAttachmentHarness(t, nil)
	tok := h.guestToken(t, "att-dev-3")

	st, b := h.call(t, http.MethodPost, "/v1/support/tickets",
		`{"title":"无存储","body":"验证 fail-closed"}`, tok)
	if st != http.StatusOK {
		t.Fatalf("create status=%d", st)
	}
	var t2 struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(mustEnv(t, b).Data, &t2); err != nil {
		t.Fatalf("decode: %v", err)
	}
	st, b = h.call(t, http.MethodPost, "/v1/support/tickets/"+t2.ID+"/attachments",
		`{"fileName":"a.png","contentType":"image/png","sizeBytes":10}`, tok)
	if st != http.StatusNotImplemented || !strings.Contains(b, "COMMON_CAPABILITY_DISABLED") ||
		!strings.Contains(b, `"retryable":false`) {
		t.Fatalf("未配置存储: status=%d body=%s", st, b)
	}

	st, b = h.call(t, http.MethodGet, "/v1/support/tickets/"+t2.ID, "", tok)
	if st != http.StatusOK {
		t.Fatalf("detail status=%d", st)
	}
	var det struct {
		Attachments []any `json:"attachments"`
	}
	if err := json.Unmarshal(mustEnv(t, b).Data, &det); err != nil {
		t.Fatalf("detail decode: %v", err)
	}
	if len(det.Attachments) != 0 {
		t.Fatalf("fail-closed 不得落登记: %v", det.Attachments)
	}
}
