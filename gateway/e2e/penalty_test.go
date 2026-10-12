// support.md v1.1「举报与处罚申诉」(F45)验收:举报受理、处罚只读投影(分页/倒序)、
// 申诉单例语义与归属不泄露、投影源 fail-closed。投影源是确定性 fake(生产
// main.go 不内置假风控数据;处罚判定与执行全在管理面)。
package e2e

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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

// fakePenaltyStore 确定性投影源:预置处罚 + 申诉单例记账。
type fakePenaltyStore struct {
	issued   map[string][]croupier.PenaltyItem
	appealed map[string]bool
}

// seed 预置某 owner 的处罚(测试账号 id 由登录生成,登录后按 device 反查)。
func (s *fakePenaltyStore) seed(owner string, items ...croupier.PenaltyItem) {
	s.issued[owner] = items
}

func (s *fakePenaltyStore) List(owner string) []croupier.PenaltyItem {
	return s.issued[owner]
}

func (s *fakePenaltyStore) Get(owner, penaltyID string) (croupier.PenaltyItem, bool) {
	for _, it := range s.issued[owner] {
		if it.ID == penaltyID {
			return it, true
		}
	}
	return croupier.PenaltyItem{}, false
}

func (s *fakePenaltyStore) Appeal(owner, penaltyID string) (croupier.PenaltyItem, error) {
	items := s.issued[owner]
	for i, it := range items {
		if it.ID != penaltyID {
			continue
		}
		if s.appealed[penaltyID] {
			return croupier.PenaltyItem{}, croupier.ErrAppealExists
		}
		s.appealed[penaltyID] = true
		appeal := croupier.AppealItem{State: "PENDING", SubmittedAt: time.Now()}
		items[i].Appeal = &appeal
		return items[i], nil
	}
	return croupier.PenaltyItem{}, nil
}

type penaltyHarness struct {
	srv *httptest.Server
	acc *account.AccountProvider
}

func newPenaltyHarness(t *testing.T, store croupier.PenaltyStore) *penaltyHarness {
	t.Helper()
	reg := providers.NewRegistry()
	acc := account.New(account.Options{Iterations: 1000})
	reqAuth := auth.RequireAuth(acc.Verifier())
	cp := croupier.New(croupier.Options{
		RequireAuth:  reqAuth,
		PenaltyStore: store,
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
	return &penaltyHarness{srv: srv, acc: acc}
}

// accountID 由 access token 反查账号 id(便于按 owner 预置处罚数据)。
func (h *penaltyHarness) accountID(t *testing.T, token string) string {
	t.Helper()
	sess, ok := h.acc.Store().SessionByAccessToken(context.Background(), token)
	if !ok {
		t.Fatalf("session not found for token")
	}
	return sess.AccountID
}

// guestToken 游客登录取 token(deviceID 不同 = 不同账号)。
func (h *penaltyHarness) guestToken(t *testing.T, deviceID string) string {
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

func (h *penaltyHarness) scope(r *http.Request) {
	r.Header.Set("X-Courier-Game-Id", "game_demo")
	r.Header.Set("X-Courier-Env", "prod")
}

// call 带 scope 与可选 Bearer 的请求,返回状态码与响应体。
func (h *penaltyHarness) call(t *testing.T, method, path, body, bearer string) (int, string) {
	t.Helper()
	rd := strings.NewReader(body)
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

// TestReportAndAppeal_V11 举报受理 + 处罚投影 + 申诉单例 + 归属不泄露。
func TestReportAndAppeal_V11(t *testing.T) {
	store := &fakePenaltyStore{issued: map[string][]croupier.PenaltyItem{}, appealed: map[string]bool{}}
	h := newPenaltyHarness(t, store)
	tok := h.guestToken(t, "pen-dev-1")
	store.seed(h.accountID(t, tok),
		croupier.PenaltyItem{ID: "pen_1", Type: "MUTE", Reason: "禁言 7 天", State: "ACTIVE",
			IssuedAt: time.Now().Add(-2 * time.Hour)},
		croupier.PenaltyItem{ID: "pen_2", Type: "SUSPENSION", Reason: "停权 30 天", State: "ACTIVE",
			IssuedAt: time.Now().Add(-1 * time.Hour)},
	)

	// 1) 举报受理:返回 rpt_ id,仅 id(进展不反馈)。
	st, body := h.call(t, http.MethodPost, "/v1/support/reports",
		`{"targetType":"PLAYER","targetId":"acc_evil","reason":"CHEATING","detail":"自瞄"}`, tok)
	if st != http.StatusOK {
		t.Fatalf("report status=%d body=%s", st, body)
	}
	var rep struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(mustEnv(t, body).Data, &rep); err != nil {
		t.Fatalf("report decode: %v", err)
	}
	if !strings.HasPrefix(rep.ID, "rpt_") {
		t.Fatalf("report id = %s", rep.ID)
	}

	// 2) 我的处罚列表:issuedAt 倒序(pen_2 在前)。
	st, body = h.call(t, http.MethodGet, "/v1/support/penalties?limit=10", "", tok)
	if st != http.StatusOK {
		t.Fatalf("list status=%d body=%s", st, body)
	}
	var page struct {
		Items []struct {
			ID     string `json:"id"`
			Type   string `json:"type"`
			State  string `json:"state"`
			Reason string `json:"reason"`
		} `json:"items"`
	}
	if err := json.Unmarshal(mustEnv(t, body).Data, &page); err != nil {
		t.Fatalf("list decode: %v", err)
	}
	if len(page.Items) != 2 || page.Items[0].ID != "pen_2" || page.Items[1].ID != "pen_1" {
		t.Fatalf("list order = %+v", page.Items)
	}

	// 3) 分页:limit=1 → 首条 + nextCursor。
	st, body = h.call(t, http.MethodGet, "/v1/support/penalties?limit=1", "", tok)
	if st != http.StatusOK {
		t.Fatalf("page1 status=%d", st)
	}
	var p1 struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
		NextCursor string `json:"nextCursor"`
	}
	if err := json.Unmarshal(mustEnv(t, body).Data, &p1); err != nil {
		t.Fatalf("page1 decode: %v", err)
	}
	if len(p1.Items) != 1 || p1.Items[0].ID != "pen_2" || p1.NextCursor == "" {
		t.Fatalf("page1 = %+v", p1)
	}

	// 4) 详情含申诉状态:先无申诉 → 提交 → PENDING;重复 → 409 SUPPORT_APPEAL_EXISTS。
	st, body = h.call(t, http.MethodGet, "/v1/support/penalties/pen_1", "", tok)
	if st != http.StatusOK {
		t.Fatalf("detail status=%d body=%s", st, body)
	}
	var det struct {
		ID     string `json:"id"`
		Appeal *struct {
			State string `json:"state"`
		} `json:"appeal"`
	}
	if err := json.Unmarshal(mustEnv(t, body).Data, &det); err != nil {
		t.Fatalf("detail decode: %v", err)
	}
	if det.ID != "pen_1" || det.Appeal != nil {
		t.Fatalf("detail = %+v", det)
	}
	st, body = h.call(t, http.MethodPost, "/v1/support/penalties/pen_1/appeal", "", tok)
	if st != http.StatusOK {
		t.Fatalf("appeal status=%d body=%s", st, body)
	}
	var appealed struct {
		Appeal struct {
			State       string `json:"state"`
			SubmittedAt string `json:"submittedAt"`
		} `json:"appeal"`
	}
	if err := json.Unmarshal(mustEnv(t, body).Data, &appealed); err != nil {
		t.Fatalf("appeal decode: %v", err)
	}
	if appealed.Appeal.State != "PENDING" || appealed.Appeal.SubmittedAt == "" {
		t.Fatalf("appeal = %+v", appealed)
	}
	st, body = h.call(t, http.MethodPost, "/v1/support/penalties/pen_1/appeal", "", tok)
	if st != http.StatusConflict || !strings.Contains(body, "SUPPORT_APPEAL_EXISTS") ||
		!strings.Contains(body, `"retryable":false`) {
		t.Fatalf("重复申诉: status=%d body=%s", st, body)
	}

	// 5) 不存在 / 他人处罚:404 SUPPORT_PENALTY_NOT_FOUND(不泄露存在性)。
	st, body = h.call(t, http.MethodGet, "/v1/support/penalties/pen_nope", "", tok)
	if st != http.StatusNotFound || !strings.Contains(body, "SUPPORT_PENALTY_NOT_FOUND") {
		t.Fatalf("不存在: status=%d body=%s", st, body)
	}
	st, body = h.call(t, http.MethodGet, "/v1/support/penalties/pen_1", "",
		h.guestToken(t, "pen-dev-2"))
	if st != http.StatusNotFound || !strings.Contains(body, "SUPPORT_PENALTY_NOT_FOUND") {
		t.Fatalf("他人处罚: status=%d body=%s", st, body)
	}

	// 6) 参数校验:targetType / reason / targetId / detail 越界 → 400。
	for _, tc := range []struct{ name, body string }{
		{"targetType", `{"targetType":"GUILD","targetId":"x","reason":"SPAM"}`},
		{"reason", `{"targetType":"PLAYER","targetId":"x","reason":"HACK"}`},
		{"targetId", `{"targetType":"PLAYER","targetId":"","reason":"SPAM"}`},
		{"detail", `{"targetType":"PLAYER","targetId":"x","reason":"SPAM","detail":"` +
			strings.Repeat("d", 1001) + `"}`},
	} {
		st, body = h.call(t, http.MethodPost, "/v1/support/reports", tc.body, tok)
		if st != http.StatusBadRequest || !strings.Contains(body, "COMMON_INVALID_ARGUMENT") {
			t.Fatalf("%s: status=%d body=%s", tc.name, st, body)
		}
	}
}

// TestPenaltyStoreMissing_V11 未配置投影源:处罚/申诉 fail-closed 501,举报不受影响。
func TestPenaltyStoreMissing_V11(t *testing.T) {
	h := newPenaltyHarness(t, nil)
	tok := h.guestToken(t, "pen-dev-3")

	// 举报不依赖投影源:正常受理。
	st, body := h.call(t, http.MethodPost, "/v1/support/reports",
		`{"targetType":"TICKET","targetId":"tkt_x","reason":"ABUSE"}`, tok)
	if st != http.StatusOK {
		t.Fatalf("report status=%d body=%s", st, body)
	}

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/v1/support/penalties"},
		{http.MethodGet, "/v1/support/penalties/pen_1"},
		{http.MethodPost, "/v1/support/penalties/pen_1/appeal"},
	} {
		st, body = h.call(t, tc.method, tc.path, "", tok)
		if st != http.StatusNotImplemented || !strings.Contains(body, "COMMON_CAPABILITY_DISABLED") ||
			!strings.Contains(body, `"retryable":false`) {
			t.Fatalf("%s %s: status=%d body=%s", tc.method, tc.path, st, body)
		}
	}
}
