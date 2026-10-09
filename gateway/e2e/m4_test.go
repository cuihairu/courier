// 批次 9(M4)网关级验收:助手检索问答——命中返回知识条目原样快照、未命中
// suggestTransfer 引导转人工;转人工链路端到端:助手未命中 → support 域提单 →
// 坐席回复(REPLIED)→ 玩家详情可见(复用 M2 链路)。
// 装配与 cmd/gateway/main.go 同构(account + croupier + sage)。
package e2e

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cuihairu/courier/gateway/auth"
	"github.com/cuihairu/courier/gateway/middleware"
	"github.com/cuihairu/courier/gateway/providers"
	"github.com/cuihairu/courier/gateway/providers/account"
	"github.com/cuihairu/courier/gateway/providers/croupier"
	"github.com/cuihairu/courier/gateway/providers/sage"
	"github.com/cuihairu/courier/gateway/routing"
)

// m4Harness M4 装配:sage(assistant)+ croupier(support)+ account。
type m4Harness struct {
	srv      *httptest.Server
	sage     *sage.AssistantProvider
	croupier *croupier.SupportProvider
}

func newM4Harness(t *testing.T) *m4Harness {
	t.Helper()
	reg := providers.NewRegistry()
	acc := account.New(account.Options{Iterations: 1000})
	reqAuth := auth.RequireAuth(acc.Verifier())
	croupierP := croupier.New(croupier.Options{RequireAuth: reqAuth})
	sageP := sage.New(sage.Options{RequireAuth: reqAuth})
	for _, h := range []providers.Handler{acc, croupierP, sageP} {
		if err := reg.Register(h); err != nil {
			t.Fatalf("register: %v", err)
		}
	}
	cfg := providers.Config{
		providers.CapIdentity:  {Primary: account.DefaultName},
		providers.CapSupport:   {Primary: croupierP.Name()},
		providers.CapAssistant: {Primary: sageP.Name()},
	}
	chain := middleware.Chain(middleware.NewRateLimiter(120, 60))
	srv := httptest.NewServer(routing.New(reg, cfg, chain))
	t.Cleanup(srv.Close)
	return &m4Harness{srv: srv, sage: sageP, croupier: croupierP}
}

// login 游客登录取 token(deviceId 区分客户端)。
func (h *m4Harness) login(t *testing.T, deviceId string) string {
	t.Helper()
	body := `{"deviceId":"` + deviceId + `","platform":"linux"}`
	req, _ := http.NewRequest(http.MethodPost, h.srv.URL+"/v1/identity/guest", strings.NewReader(body))
	req.Header.Set("X-Courier-Game-Id", "game_demo")
	req.Header.Set("X-Courier-Env", "prod")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("guest login: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("guest login: status=%d body=%s", resp.StatusCode, b)
	}
	var out struct {
		Data struct {
			AccessToken string `json:"accessToken"`
		} `json:"data"`
	}
	if err := json.Unmarshal(b, &out); err != nil || out.Data.AccessToken == "" {
		t.Fatalf("login decode: err=%v body=%s", err, b)
	}
	return out.Data.AccessToken
}

// m4Do 带 Bearer + scope 的 JSON 请求。
func (h *m4Harness) m4Do(t *testing.T, token, method, path, body string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(method, h.srv.URL+path, strings.NewReader(body))
	req.Header.Set("X-Courier-Game-Id", "game_demo")
	req.Header.Set("X-Courier-Env", "prod")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestM4AssistantQueryHitAndMiss(t *testing.T) {
	h := newM4Harness(t)
	token := h.login(t, "m4-device-1")

	// 管理面登记知识条目(运营登记,契约:sage 自带知识库)。
	h.sage.AddEntry("怎么找回账号", "在登录页点击『忘记密码』,按提示验证即可。", []string{"找回", "账号"})

	// 命中:matched=true + answer 原样 + suggestTransfer=false。
	code, body := h.m4Do(t, token, http.MethodPost, "/v1/assistant/query", `{"text":"账号怎么找回"}`)
	if code != http.StatusOK {
		t.Fatalf("hit: %d %s", code, body)
	}
	var hit struct {
		Data struct {
			Matched bool `json:"matched"`
			Answer  struct {
				ID       string `json:"id"`
				Question string `json:"question"`
				Answer   string `json:"answer"`
			} `json:"answer"`
			SuggestTransfer bool `json:"suggestTransfer"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &hit); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !hit.Data.Matched || hit.Data.SuggestTransfer {
		t.Fatalf("hit flags = %s", body)
	}
	if hit.Data.Answer.Question != "怎么找回账号" ||
		hit.Data.Answer.Answer != "在登录页点击『忘记密码』,按提示验证即可。" {
		t.Fatalf("answer 非原样快照: %s", body)
	}
	if !strings.HasPrefix(hit.Data.Answer.ID, "faq_") {
		t.Fatalf("id 前缀: %s", body)
	}

	// 未命中:200 + matched=false + suggestTransfer=true(不是错误)。
	code, body = h.m4Do(t, token, http.MethodPost, "/v1/assistant/query", `{"text":"如何下载游戏"}`)
	if code != http.StatusOK {
		t.Fatalf("miss: %d %s", code, body)
	}
	if !strings.Contains(body, `"matched":false`) || !strings.Contains(body, `"suggestTransfer":true`) {
		t.Fatalf("miss flags = %s", body)
	}

	// 校验:空 text → 400 COMMON_INVALID_ARGUMENT(契约零新增错误码)。
	code, body = h.m4Do(t, token, http.MethodPost, "/v1/assistant/query", `{"text":"   "}`)
	if code != http.StatusBadRequest || !strings.Contains(body, "COMMON_INVALID_ARGUMENT") {
		t.Fatalf("empty text: %d %s", code, body)
	}

	// 命中率可统计(管理面):2 次有效查询、1 次命中。
	queries, hits := h.sage.QueryStats()
	if queries != 2 || hits != 1 {
		t.Fatalf("stats = %d/%d, want 2/1", queries, hits)
	}
}

func TestM4AssistantTransferToHuman(t *testing.T) {
	h := newM4Harness(t)
	token := h.login(t, "m4-device-2")

	// 未命中 → suggestTransfer → 客户端经 support 域提单(转人工复用 M2 链路)。
	_, body := h.m4Do(t, token, http.MethodPost, "/v1/assistant/query", `{"text":"充值没到账"}`)
	if !strings.Contains(body, `"suggestTransfer":true`) {
		t.Fatalf("应引导转人工: %s", body)
	}

	code, body := h.m4Do(t, token, http.MethodPost, "/v1/support/tickets",
		`{"title":"[助手转人工] 充值没到账","body":"助手未命中;玩家问题:充值没到账"}`)
	if code != http.StatusOK {
		t.Fatalf("create ticket: %d %s", code, body)
	}
	var created struct {
		Data struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &created); err != nil || created.Data.ID == "" {
		t.Fatalf("ticket decode: err=%v body=%s", err, body)
	}
	ticketID := created.Data.ID
	if created.Data.Status != "OPEN" {
		t.Fatalf("new ticket status = %s", body)
	}

	// 坐席侧(croupier 管理面)回复 → REPLIED + support.ticket_replied 广播(M2 链路)。
	if err := h.croupier.AgentReply(ticketID, "您好,已为您补发,请查收。"); err != nil {
		t.Fatalf("agent reply: %v", err)
	}

	// 玩家详情可见:状态 REPLIED + 坐席消息在列(转人工链路端到端)。
	code, body = h.m4Do(t, token, http.MethodGet, "/v1/support/tickets/"+ticketID, "")
	if code != http.StatusOK {
		t.Fatalf("detail: %d %s", code, body)
	}
	if !strings.Contains(body, `"status":"REPLIED"`) ||
		!strings.Contains(body, "已为您补发") {
		t.Fatalf("detail = %s", body)
	}
}
