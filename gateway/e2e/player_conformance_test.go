// 批次 67 玩家域第二实现一致性 e2e(批次 18 identity / 64 realname 先例同构):
// 同一 wire 场景对 archivist(状态表)与 archivist-alt(事件日志)各跑一遍、
// 同一断言——「玩家档案 Provider 可换」有行为证明而非仅接口冻结。
// 两实现异构面(状态表 vs 事件日志/投影重放)不在本文件断言,
// 由各自单测承载(archivist_test.go / providers/archivistalt)。
package e2e

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cuihairu/courier/gateway/auth"
	"github.com/cuihairu/courier/gateway/middleware"
	"github.com/cuihairu/courier/gateway/providers"
	"github.com/cuihairu/courier/gateway/providers/account"
	"github.com/cuihairu/courier/gateway/providers/archivist"
	"github.com/cuihairu/courier/gateway/providers/archivistalt"
	"github.com/cuihairu/courier/gateway/routing"
)

type playerHarness struct {
	srv *httptest.Server
}

// newPlayerHarness 装配 account + 一个玩家档案实现(经路由,与 main 同构)。
func newPlayerHarness(t *testing.T, useAlt bool) *playerHarness {
	t.Helper()
	reg := providers.NewRegistry()
	acc := account.New(account.Options{Iterations: 1000})
	reqAuth := auth.RequireAuth(acc.Verifier())
	if err := reg.Register(acc); err != nil {
		t.Fatalf("register acc: %v", err)
	}
	var playerP providers.Handler
	if useAlt {
		playerP = archivistalt.New(archivistalt.Options{RequireAuth: reqAuth})
	} else {
		playerP = archivist.New(archivist.Options{RequireAuth: reqAuth})
	}
	if err := reg.Register(playerP); err != nil {
		t.Fatalf("register player provider: %v", err)
	}
	cfg := providers.Config{
		providers.CapIdentity: {Primary: account.DefaultName},
		providers.CapPlayer:   {Primary: playerP.Name()},
	}
	chain := middleware.Chain(middleware.NewRateLimiter(120, 60))
	srv := httptest.NewServer(routing.New(reg, cfg, chain))
	t.Cleanup(srv.Close)
	return &playerHarness{srv: srv}
}

func (h *playerHarness) login(t *testing.T) string {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, h.srv.URL+"/v1/identity/guest",
		strings.NewReader(`{"deviceId":"player-e2e","platform":"linux"}`))
	req.Header.Set("X-Courier-Game-Id", "game_demo")
	req.Header.Set("X-Courier-Env", "prod")
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

func (h *playerHarness) call(t *testing.T, method, path, token, body string, out any) (int, string) {
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
	var env struct {
		Data  json.RawMessage `json:"data"`
		Error *struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	errCode := ""
	if env.Error != nil {
		errCode = env.Error.Code
	}
	if out != nil && len(env.Data) > 0 {
		if err := json.Unmarshal(env.Data, out); err != nil {
			t.Fatalf("unmarshal %s data: %v", path, err)
		}
	}
	return resp.StatusCode, errCode
}

// runPlayerContract 玩家档案契约链:懒建档 → 改名修剪 → 校验 → 绑定幂等 →
// scope 隔离;认证/入参边缘(401/400)。
func runPlayerContract(t *testing.T, h *playerHarness) {
	t.Helper()
	token := h.login(t)

	// 未认证 → 401(通用码,两实现一致)。
	if code, errCode := h.call(t, http.MethodGet, "/v1/player/profile", "", "", nil); code != http.StatusUnauthorized ||
		errCode != "COMMON_UNAUTHENTICATED" {
		t.Fatalf("anon profile = %d %q, want 401 COMMON_UNAUTHENTICATED", code, errCode)
	}

	// 首次 GET 即建档:默认展示名 Player,createdAt=updatedAt。
	var prof struct {
		DisplayName string `json:"displayName"`
		AvatarURL   string `json:"avatarUrl"`
		CreatedAt   string `json:"createdAt"`
		UpdatedAt   string `json:"updatedAt"`
	}
	if code, _ := h.call(t, http.MethodGet, "/v1/player/profile", token, "", &prof); code != 200 ||
		prof.DisplayName != "Player" || prof.AvatarURL != "" || prof.CreatedAt == "" || prof.CreatedAt != prof.UpdatedAt {
		t.Fatalf("lazy profile = %d %+v", code, prof)
	}

	// PATCH 修剪首尾空白,响应体为准(契约客户端要求);再读回一致。
	if code, _ := h.call(t, http.MethodPatch, "/v1/player/profile", token,
		`{"displayName":"  新名字  ","avatarUrl":"https://cdn.example/a.png"}`, &prof); code != 200 ||
		prof.DisplayName != "新名字" || prof.AvatarURL != "https://cdn.example/a.png" {
		t.Fatalf("patch = %d %+v", code, prof)
	}
	if code, _ := h.call(t, http.MethodGet, "/v1/player/profile", token, "", &prof); code != 200 ||
		prof.DisplayName != "新名字" {
		t.Fatalf("readback = %d %+v", code, prof)
	}
	// 只改 displayName 不动 avatarUrl(字段级合并语义,两实现一致)。
	if code, _ := h.call(t, http.MethodPatch, "/v1/player/profile", token,
		`{"displayName":"三号"}`, &prof); code != 200 || prof.DisplayName != "三号" ||
		prof.AvatarURL != "https://cdn.example/a.png" {
		t.Fatalf("partial patch = %d %+v", code, prof)
	}

	// 校验:空名(修剪后)/超长 → 400 COMMON_INVALID_ARGUMENT。
	if code, errCode := h.call(t, http.MethodPatch, "/v1/player/profile", token,
		`{"displayName":"   "}`, nil); code != http.StatusBadRequest || errCode != "COMMON_INVALID_ARGUMENT" {
		t.Fatalf("blank name = %d %q", code, errCode)
	}
	if code, errCode := h.call(t, http.MethodPatch, "/v1/player/profile", token,
		`{"displayName":"`+strings.Repeat("字", 31)+`"}`, nil); code != http.StatusBadRequest ||
		errCode != "COMMON_INVALID_ARGUMENT" {
		t.Fatalf("long name = %d %q", code, errCode)
	}

	// 绑定:首绑 → 幂等重绑(列表不重复)。
	var bound struct {
		PlayerID string `json:"playerId"`
		BoundAt  string `json:"boundAt"`
	}
	if code, _ := h.call(t, http.MethodPost, "/v1/player/characters", token,
		`{"playerId":"char_001"}`, &bound); code != 200 || bound.PlayerID != "char_001" || bound.BoundAt == "" {
		t.Fatalf("bind = %d %+v", code, bound)
	}
	if code, _ := h.call(t, http.MethodPost, "/v1/player/characters", token,
		`{"playerId":"char_001"}`, &bound); code != 200 || bound.BoundAt == "" {
		t.Fatalf("idempotent bind = %d %+v", code, bound)
	}
	var page struct {
		Items []struct {
			PlayerID string `json:"playerId"`
		} `json:"items"`
	}
	if code, _ := h.call(t, http.MethodGet, "/v1/player/characters", token, "", &page); code != 200 ||
		len(page.Items) != 1 || page.Items[0].PlayerID != "char_001" {
		t.Fatalf("characters = %d %+v", code, page)
	}

	// 校验:空 playerId → 400。
	if code, errCode := h.call(t, http.MethodPost, "/v1/player/characters", token,
		`{"playerId":""}`, nil); code != http.StatusBadRequest || errCode != "COMMON_INVALID_ARGUMENT" {
		t.Fatalf("empty playerId = %d %q", code, errCode)
	}
}

// TestPlayerContractConformance 两实现 × 同一契约,同一断言。
func TestPlayerContractConformance(t *testing.T) {
	for _, impl := range []struct {
		name   string
		useAlt bool
	}{
		{"archivist", false},
		{"archivist-alt", true},
	} {
		t.Run(impl.name, func(t *testing.T) {
			runPlayerContract(t, newPlayerHarness(t, impl.useAlt))
		})
	}
}
