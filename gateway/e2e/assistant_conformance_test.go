// 批次 70 助手域第二实现一致性 e2e(批次 18/64/67/68/69 先例同构):
// 同一 wire 场景对 sage(读时规范化:每次查询对每个条目重复 ToLower/Trim)
// 与 sage-alt(写时预计算:AddEntry 时规范化一次,查询零重复计算)各跑一遍、
// 同一断言——「助手 Provider 可换」有行为证明而非仅接口冻结。两实现
// 异构面不在本文件断言,由各自单测承载(sage 由 e2e/m4 承载 /
// providers/sagealt)。
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
	"github.com/cuihairu/courier/gateway/providers/sage"
	"github.com/cuihairu/courier/gateway/providers/sagealt"
	"github.com/cuihairu/courier/gateway/routing"
)

// assistantHarness 装配 account + 一个助手实现(经路由,与 main 同构)。
type assistantHarness struct {
	srv     *httptest.Server
	sage    *sage.AssistantProvider
	sageAlt *sagealt.AssistantProvider
}

func newAssistantHarness(t *testing.T, useAlt bool) *assistantHarness {
	t.Helper()
	reg := providers.NewRegistry()
	acc := account.New(account.Options{Iterations: 1000})
	reqAuth := auth.RequireAuth(acc.Verifier())

	var aP providers.Handler
	var sageP *sage.AssistantProvider
	var sageAltP *sagealt.AssistantProvider
	if useAlt {
		sageAltP = sagealt.New(sagealt.Options{RequireAuth: reqAuth})
		aP = sageAltP
	} else {
		sageP = sage.New(sage.Options{RequireAuth: reqAuth})
		aP = sageP
	}
	if err := reg.Register(aP); err != nil {
		t.Fatalf("register assistant provider: %v", err)
	}
	if err := reg.Register(acc); err != nil {
		t.Fatalf("register acc: %v", err)
	}
	cfg := providers.Config{
		providers.CapIdentity:  {Primary: account.DefaultName},
		providers.CapAssistant: {Primary: aP.Name()},
	}
	chain := middleware.Chain(middleware.NewRateLimiter(120, 60))
	srv := httptest.NewServer(routing.New(reg, cfg, chain))
	t.Cleanup(srv.Close)
	return &assistantHarness{srv: srv, sage: sageP, sageAlt: sageAltP}
}

func (h *assistantHarness) scopeHeaders(r *http.Request) {
	r.Header.Set("X-Courier-Game-Id", "game_demo")
	r.Header.Set("X-Courier-Env", "prod")
}

func (h *assistantHarness) login(t *testing.T) string {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, h.srv.URL+"/v1/identity/guest",
		strings.NewReader(`{"deviceId":"assistant-e2e","platform":"linux"}`))
	h.scopeHeaders(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("guest login: %v", err)
	}
	defer resp.Body.Close()
	var out struct {
		Data struct {
			AccessToken string `json:"accessToken"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || out.Data.AccessToken == "" {
		t.Fatalf("login decode: status=%d err=%v", resp.StatusCode, err)
	}
	return out.Data.AccessToken
}

// addEntry 管理面登记知识条目(两实现同签名)。
func (h *assistantHarness) addEntry(t *testing.T, useAlt bool, question, answer string, keywords []string) {
	t.Helper()
	if useAlt {
		h.sageAlt.AddEntry(question, answer, keywords)
		return
	}
	h.sage.AddEntry(question, answer, keywords)
}

// stats 管理面检索量/命中率(两实现同语义)。
func (h *assistantHarness) stats(t *testing.T, useAlt bool) (queries, hits int64) {
	t.Helper()
	if useAlt {
		return h.sageAlt.QueryStats()
	}
	return h.sage.QueryStats()
}

func (h *assistantHarness) do(t *testing.T, token, method, path, body string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(method, h.srv.URL+path, strings.NewReader(body))
	h.scopeHeaders(req)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

// runAssistantContract 助手契约链:认证 → 路由形状 → 命中快照 → 未命中
// 引导 → 评分权重 → 校验 → 修剪 → 大小写不敏感 → 统计。
func runAssistantContract(t *testing.T, h *assistantHarness, useAlt bool) {
	t.Helper()
	token := h.login(t)

	// 匿名 → 401 COMMON_UNAUTHENTICATED(契约 assistant.md)。
	if code, body := h.do(t, "", http.MethodPost, "/v1/assistant/query", `{"text":"q"}`); code != http.StatusUnauthorized ||
		!strings.Contains(body, "COMMON_UNAUTHENTICATED") {
		t.Fatalf("anon = %d %s, want 401 COMMON_UNAUTHENTICATED", code, body)
	}
	// 路由形状:GET query → 404;未知路径 → 404。
	if code, _ := h.do(t, token, http.MethodGet, "/v1/assistant/query", ""); code != http.StatusNotFound {
		t.Fatalf("GET query = %d, want 404", code)
	}
	if code, _ := h.do(t, token, http.MethodPost, "/v1/assistant/chat", `{"text":"q"}`); code != http.StatusNotFound {
		t.Fatalf("unknown path = %d, want 404", code)
	}

	// 空库 → 未命中(200,非错误)。
	if code, body := h.do(t, token, http.MethodPost, "/v1/assistant/query", `{"text":"任意问题"}`); code != http.StatusOK ||
		!strings.Contains(body, `"matched":false`) {
		t.Fatalf("empty KB = %d %s", code, body)
	}

	// 登记:大小写/首尾空白混排(异构面行为等价:两实现都折叠)。
	h.addEntry(t, useAlt, "  How To Refund  ", " CLICK OK Button ", []string{" REFUND ", ""})
	h.addEntry(t, useAlt, "活动怎么参加", "见公告", []string{"活动"})
	h.addEntry(t, useAlt, "充值比例", "活动期间双倍", nil)

	// 命中:matched=true + answer 原样快照 + suggestTransfer=false。
	code, body := h.do(t, token, http.MethodPost, "/v1/assistant/query", `{"text":"how to refund"}`)
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
		t.Fatalf("decode: %v (%s)", err, body)
	}
	if !hit.Data.Matched || hit.Data.SuggestTransfer {
		t.Fatalf("hit flags = %s", body)
	}
	// 原样快照:wire 返回登记时的原始文本(含大小写与首尾空白),不改写。
	if hit.Data.Answer.Question != "  How To Refund  " ||
		hit.Data.Answer.Answer != " CLICK OK Button " {
		t.Fatalf("answer 非原样快照: %s", body)
	}
	if !strings.HasPrefix(hit.Data.Answer.ID, "faq_") {
		t.Fatalf("id 前缀: %s", body)
	}

	// 评分权重:问题命中(3)优于答案命中(1)。
	_, body = h.do(t, token, http.MethodPost, "/v1/assistant/query", `{"text":"活动"}`)
	if !strings.Contains(body, `"question":"活动怎么参加"`) {
		t.Fatalf("应命中问题条目(高分): %s", body)
	}

	// 未命中:200 + matched=false + suggestTransfer=true,不带 answer。
	code, body = h.do(t, token, http.MethodPost, "/v1/assistant/query", `{"text":"如何下载游戏"}`)
	if code != http.StatusOK || !strings.Contains(body, `"matched":false`) ||
		!strings.Contains(body, `"suggestTransfer":true`) || strings.Contains(body, `"answer"`) {
		t.Fatalf("miss = %d %s", code, body)
	}

	// 校验:空/全空白/超 500 字/非 JSON → 400 COMMON_INVALID_ARGUMENT。
	for _, bad := range []string{`{"text":""}`, `{"text":"   "}`,
		`{"text":"` + strings.Repeat("字", 501) + `"}`, `oops`} {
		if code, body := h.do(t, token, http.MethodPost, "/v1/assistant/query", bad); code != http.StatusBadRequest ||
			!strings.Contains(body, "COMMON_INVALID_ARGUMENT") {
			t.Fatalf("bad %s = %d %s", bad, code, body)
		}
	}
	// 修剪首尾 + 边界 500 字合法。
	if code, body := h.do(t, token, http.MethodPost, "/v1/assistant/query", `{"text":"  活动  "}`); code != http.StatusOK ||
		!strings.Contains(body, `"matched":true`) {
		t.Fatalf("trim = %d %s", code, body)
	}
	if code, _ := h.do(t, token, http.MethodPost, "/v1/assistant/query",
		`{"text":"`+strings.Repeat("字", 500)+`"}`); code != http.StatusOK {
		t.Fatalf("500 字应合法: %d", code)
	}

	// 统计:有效查询 6 次(空库 / how to refund / 活动 / 如何下载游戏 /
	// 修剪活动 / 500 字),命中 3 次;4 次参数错不计,401/404 不进检索。
	queries, hits := h.stats(t, useAlt)
	if queries != 6 || hits != 3 {
		t.Fatalf("stats = %d/%d, want 6/3", queries, hits)
	}
}

// TestAssistantContractConformance 两实现 × 同一契约,同一断言。
func TestAssistantContractConformance(t *testing.T) {
	for _, impl := range []struct {
		name   string
		useAlt bool
	}{
		{"sage", false},
		{"sage-alt", true},
	} {
		t.Run(impl.name, func(t *testing.T) {
			runAssistantContract(t, newAssistantHarness(t, impl.useAlt), impl.useAlt)
		})
	}
}
