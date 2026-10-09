// 批次 18:identity 域一致性 e2e——同一 wire 场景对两个 AccountProvider 实现
// (自建 account「不透明随机令牌+会话表」/ accountalt「HMAC 签名自验证令牌」)
// 各跑一遍、同一断言,全绿 = 「账号 Provider 可换」有行为证明而非仅接口冻结。
//
// 装配与 cmd/gateway/main.go 同构(registry + 路由表 + 冻结中间件链),身份源
// 经 auth.SwitchableVerifier 供 M2 域共享——跨域校验(换身份源后 herald 照常
// 认证)包含在场景内。
package e2e

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/cuihairu/courier/gateway/auth"
	"github.com/cuihairu/courier/gateway/middleware"
	"github.com/cuihairu/courier/gateway/providers"
	"github.com/cuihairu/courier/gateway/providers/account"
	"github.com/cuihairu/courier/gateway/providers/accountalt"
	"github.com/cuihairu/courier/gateway/providers/chirp"
	"github.com/cuihairu/courier/gateway/providers/croupier"
	"github.com/cuihairu/courier/gateway/providers/herald"
	"github.com/cuihairu/courier/gateway/routing"
)

// confClock 可推进的测试时钟(被测 Provider 的存储与验证器共享)。
type confClock struct {
	mu sync.Mutex
	t  time.Time
}

func newConfClock() *confClock          { return &confClock{t: time.Now()} }
func (c *confClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}
func (c *confClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// confHarness 一致性装配:被测身份 Provider + SwitchableVerifier + M2 三域。
type confHarness struct {
	srv   *httptest.Server
	clock *confClock
}

// newConformanceGateway 装配一个以 newIdentity 提供身份源的完整网关
// (registry + 路由表 primary 指向它 + 冻结中间件链,与 main 同构)。
func newConformanceGateway(t *testing.T,
	newIdentity func(clock func() time.Time) (providers.Handler, auth.Verifier)) *confHarness {
	t.Helper()
	clock := newConfClock()
	id, idVerifier := newIdentity(clock.Now)

	reg := providers.NewRegistry()
	ver := auth.NewSwitchable(idVerifier)
	reqAuth := auth.RequireAuth(ver)
	chirpP := chirp.New(chirp.Options{RequireAuth: reqAuth, Heartbeat: 25 * time.Second})
	heraldP := herald.New(herald.Options{RequireAuth: reqAuth, Notify: chirpP.Hub().Publish})
	croupierP := croupier.New(croupier.Options{RequireAuth: reqAuth, Notify: chirpP.Hub().Publish})
	for _, h := range []providers.Handler{id, heraldP, croupierP, chirpP} {
		if err := reg.Register(h); err != nil {
			t.Fatalf("register: %v", err)
		}
	}
	cfg := providers.Config{
		providers.CapIdentity:      {Primary: id.Name()},
		providers.CapAnnouncements: {Primary: herald.DefaultName},
		providers.CapSupport:       {Primary: croupier.DefaultName},
		providers.CapMessages:      {Primary: chirp.DefaultName},
	}
	chain := middleware.Chain(middleware.NewRateLimiter(1000, 1000))
	srv := httptest.NewServer(routing.New(reg, cfg, chain))
	t.Cleanup(srv.Close)
	return &confHarness{srv: srv, clock: clock}
}

type confResp struct {
	status    int
	code      string
	retryable bool
	data      json.RawMessage
	header    http.Header
}

// do 直连被测网关(真 HTTP;scope 头恒发,与 SDK 同口径)。
func (h *confHarness) do(t *testing.T, method, path, body, bearer, gameID string) confResp {
	t.Helper()
	var rd *bytes.Reader
	if body == "" {
		rd = bytes.NewReader(nil)
	} else {
		rd = bytes.NewReader([]byte(body))
	}
	req, err := http.NewRequest(method, h.srv.URL+path, rd)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if gameID == "" {
		gameID = "game_a"
	}
	req.Header.Set("X-Courier-Game-Id", gameID)
	req.Header.Set("X-Courier-Env", "prod")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	out := confResp{status: resp.StatusCode, header: resp.Header.Clone()}
	var env struct {
		Error *struct {
			Code      string `json:"code"`
			Retryable bool   `json:"retryable"`
		} `json:"error"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("%s %s: 响应非 JSON: %v", method, path, err)
	}
	if env.Error != nil {
		out.code = env.Error.Code
		out.retryable = env.Error.Retryable
	}
	out.data = env.Data
	return out
}

func (r confResp) sessionData(t *testing.T) map[string]any {
	t.Helper()
	var s map[string]any
	if err := json.Unmarshal(r.data, &s); err != nil {
		t.Fatalf("解析会话 data: %v (raw=%s)", err, r.data)
	}
	return s
}

func confStr(t *testing.T, m map[string]any, key string) string {
	t.Helper()
	s, _ := m[key].(string)
	return s
}

func confRegisterBody(email, password, device string) string {
	b := `{"email":"` + email + `","password":"` + password + `"`
	if device != "" {
		b += `,"deviceId":"` + device + `"`
	}
	return b + `}`
}

// confLogin 便捷:邮箱登录并断言成功,返回会话 map。
func (h *confHarness) confLogin(t *testing.T, email, password, device string) map[string]any {
	t.Helper()
	r := h.do(t, http.MethodPost, "/v1/identity/login", confRegisterBody(email, password, device), "", "")
	if r.status != http.StatusOK {
		t.Fatalf("login status = %d code=%s", r.status, r.code)
	}
	return r.sessionData(t)
}

// runIdentityContract 完整 wire 契约场景(与 providers/account 单测同口径,
// 但只说 wire:JSON 字段与冻结码,不碰任何实现内部类型)。
func runIdentityContract(t *testing.T, h *confHarness) {
	t.Run("注册+登录+会话信息", func(t *testing.T) {
		r := h.do(t, http.MethodPost, "/v1/identity/register",
			confRegisterBody("a@b.co", "password8", "dev-1"), "", "")
		if r.status != http.StatusOK {
			t.Fatalf("register status = %d code=%s", r.status, r.code)
		}
		s := r.sessionData(t)
		acc := s["account"].(map[string]any)
		if acc["type"] != "EMAIL" || acc["email"] != "a@b.co" {
			t.Fatalf("account = %v", acc)
		}
		if confStr(t, s, "accessToken") == "" || confStr(t, s, "refreshToken") == "" {
			t.Fatal("token 为空")
		}
		if s["deviceId"] != "dev-1" {
			t.Fatalf("deviceId = %v, 期望 dev-1", s["deviceId"])
		}

		s2 := h.confLogin(t, "a@b.co", "password8", "")
		if s2["account"].(map[string]any)["id"] == "" {
			t.Fatal("account.id 为空")
		}

		// 会话信息端点
		sr := h.do(t, http.MethodGet, "/v1/identity/session", "", confStr(t, s2, "accessToken"), "")
		if sr.status != http.StatusOK {
			t.Fatalf("session status = %d code=%s", sr.status, sr.code)
		}
		ss := sr.sessionData(t)
		if ss["account"].(map[string]any)["id"] != s2["account"].(map[string]any)["id"] {
			t.Fatal("session.account.id 与登录不一致")
		}
		if confStr(t, ss, "sessionId") == "" {
			t.Fatal("sessionId 为空")
		}
	})

	t.Run("防枚举:密码错与账号不存在返回一致", func(t *testing.T) {
		wrong := h.do(t, http.MethodPost, "/v1/identity/login", confRegisterBody("a@b.co", "wrong-pass-1", ""), "", "")
		unknown := h.do(t, http.MethodPost, "/v1/identity/login", confRegisterBody("ghost@b.co", "whatever123", ""), "", "")
		if wrong.status != http.StatusUnauthorized || wrong.code != "AUTH_INVALID_CREDENTIALS" {
			t.Fatalf("wrong: status=%d code=%s", wrong.status, wrong.code)
		}
		if unknown.status != wrong.status || unknown.code != wrong.code {
			t.Fatalf("unknown 与 wrong 不一致: %d/%s vs %d/%s",
				unknown.status, unknown.code, wrong.status, wrong.code)
		}
	})

	t.Run("注册校验:400 COMMON_INVALID_ARGUMENT", func(t *testing.T) {
		cases := map[string]string{
			"email 非法": confRegisterBody("not-an-email", "password8", ""),
			"密码过短":     confRegisterBody("x@b.co", "short", ""),
			"deviceId 过长": confRegisterBody("x@b.co", "password8", string(bytes.Repeat([]byte("d"), 129))),
		}
		for name, body := range cases {
			r := h.do(t, http.MethodPost, "/v1/identity/register", body, "", "")
			if r.status != http.StatusBadRequest || r.code != "COMMON_INVALID_ARGUMENT" || r.retryable {
				t.Fatalf("%s: status=%d code=%s retryable=%v", name, r.status, r.code, r.retryable)
			}
		}
	})

	t.Run("邮箱重复:409 AUTH_EMAIL_TAKEN", func(t *testing.T) {
		if r := h.do(t, http.MethodPost, "/v1/identity/register", confRegisterBody("dup@b.co", "password8", ""), "", ""); r.status != http.StatusOK {
			t.Fatalf("首次注册失败: %d", r.status)
		}
		r := h.do(t, http.MethodPost, "/v1/identity/register", confRegisterBody("dup@b.co", "password9", ""), "", "")
		if r.status != http.StatusConflict || r.code != "AUTH_EMAIL_TAKEN" || r.retryable {
			t.Fatalf("status=%d code=%s, 期望 409 AUTH_EMAIL_TAKEN", r.status, r.code)
		}
	})

	t.Run("游客按设备幂等", func(t *testing.T) {
		r1 := h.do(t, http.MethodPost, "/v1/identity/guest", `{"deviceId":"g-dev","platform":"ios"}`, "", "")
		r2 := h.do(t, http.MethodPost, "/v1/identity/guest", `{"deviceId":"g-dev"}`, "", "")
		if r1.status != http.StatusOK || r2.status != http.StatusOK {
			t.Fatalf("guest: %d/%d", r1.status, r2.status)
		}
		s1, s2 := r1.sessionData(t), r2.sessionData(t)
		if s1["account"].(map[string]any)["id"] != s2["account"].(map[string]any)["id"] {
			t.Fatal("同设备应回同一游客账号")
		}
		acc := s1["account"].(map[string]any)
		if acc["type"] != "GUEST" || acc["email"] != nil {
			t.Fatalf("游客账号字段异常: %v", acc)
		}
		if r := h.do(t, http.MethodPost, "/v1/identity/guest", `{}`, "", ""); r.status != http.StatusBadRequest {
			t.Fatalf("缺 deviceId 应 400, got %d", r.status)
		}
	})

	t.Run("游客转正 bind 链", func(t *testing.T) {
		guest := h.do(t, http.MethodPost, "/v1/identity/guest", `{"deviceId":"b-dev"}`, "", "").sessionData(t)
		guestID := guest["account"].(map[string]any)["id"].(string)

		r := h.do(t, http.MethodPost, "/v1/identity/bind",
			confRegisterBody("bind@b.co", "password8", ""), confStr(t, guest, "accessToken"), "")
		if r.status != http.StatusOK {
			t.Fatalf("bind status = %d code=%s", r.status, r.code)
		}
		var bound map[string]any
		if err := json.Unmarshal(r.data, &bound); err != nil {
			t.Fatalf("解析 bind data: %v", err)
		}
		if bound["type"] != "EMAIL" || bound["email"] != "bind@b.co" {
			t.Fatalf("bind account = %v", bound)
		}

		s := h.confLogin(t, "bind@b.co", "password8", "b-dev")
		if s["account"].(map[string]any)["id"].(string) != guestID {
			t.Fatal("bind 后账号应同一")
		}

		if r := h.do(t, http.MethodPost, "/v1/identity/bind",
			confRegisterBody("again@b.co", "password8", ""), confStr(t, guest, "accessToken"), ""); r.status != http.StatusBadRequest || r.code != "COMMON_INVALID_ARGUMENT" {
			t.Fatalf("EMAIL 再 bind: status=%d code=%s", r.status, r.code)
		}

		h.do(t, http.MethodPost, "/v1/identity/register", confRegisterBody("taken@b.co", "password8", ""), "", "")
		g2 := h.do(t, http.MethodPost, "/v1/identity/guest", `{"deviceId":"b-dev-2"}`, "", "").sessionData(t)
		if r := h.do(t, http.MethodPost, "/v1/identity/bind",
			confRegisterBody("taken@b.co", "password8", ""), confStr(t, g2, "accessToken"), ""); r.status != http.StatusConflict || r.code != "AUTH_EMAIL_TAKEN" {
			t.Fatalf("邮箱被占: status=%d code=%s", r.status, r.code)
		}
	})

	t.Run("refresh 轮换+旧 access 失效+重放吊销", func(t *testing.T) {
		h.do(t, http.MethodPost, "/v1/identity/register", confRegisterBody("rot@b.co", "password8", ""), "", "")
		s1 := h.confLogin(t, "rot@b.co", "password8", "")
		tok1, ref1 := confStr(t, s1, "accessToken"), confStr(t, s1, "refreshToken")

		r := h.do(t, http.MethodPost, "/v1/identity/refresh", `{"refreshToken":"`+ref1+`"}`, "", "")
		if r.status != http.StatusOK {
			t.Fatalf("refresh status = %d code=%s", r.status, r.code)
		}
		s2 := r.sessionData(t)
		tok2, ref2 := confStr(t, s2, "accessToken"), confStr(t, s2, "refreshToken")
		if tok2 == tok1 || ref2 == ref1 {
			t.Fatal("轮换应生成全新 token")
		}

		if old := h.do(t, http.MethodGet, "/v1/identity/session", "", tok1, ""); old.status != http.StatusUnauthorized || old.code != "AUTH_INVALID_CREDENTIALS" {
			t.Fatalf("旧 access: status=%d code=%s", old.status, old.code)
		}
		if got := h.do(t, http.MethodGet, "/v1/identity/session", "", tok2, ""); got.status != http.StatusOK {
			t.Fatalf("新 access: status=%d code=%s", got.status, got.code)
		}
		replay := h.do(t, http.MethodPost, "/v1/identity/refresh", `{"refreshToken":"`+ref1+`"}`, "", "")
		if replay.status != http.StatusUnauthorized || replay.code != "AUTH_REFRESH_REUSED" {
			t.Fatalf("重放: status=%d code=%s", replay.status, replay.code)
		}
		after := h.do(t, http.MethodPost, "/v1/identity/refresh", `{"refreshToken":"`+ref2+`"}`, "", "")
		if after.status != http.StatusUnauthorized || after.code != "AUTH_TOKEN_REVOKED" {
			t.Fatalf("吊销后: status=%d code=%s", after.status, after.code)
		}
	})

	t.Run("refresh 未知令牌:401 AUTH_INVALID_CREDENTIALS", func(t *testing.T) {
		r := h.do(t, http.MethodPost, "/v1/identity/refresh", `{"refreshToken":"deadbeef"}`, "", "")
		if r.status != http.StatusUnauthorized || r.code != "AUTH_INVALID_CREDENTIALS" {
			t.Fatalf("status=%d code=%s", r.status, r.code)
		}
	})

	t.Run("refresh 过期:31d 后 AUTH_TOKEN_EXPIRED", func(t *testing.T) {
		h.do(t, http.MethodPost, "/v1/identity/register", confRegisterBody("exp@b.co", "password8", ""), "", "")
		s := h.confLogin(t, "exp@b.co", "password8", "")
		h.clock.Advance(31 * 24 * time.Hour) // refresh TTL 默认 30d
		r := h.do(t, http.MethodPost, "/v1/identity/refresh", `{"refreshToken":"`+confStr(t, s, "refreshToken")+`"}`, "", "")
		if r.status != http.StatusUnauthorized || r.code != "AUTH_TOKEN_EXPIRED" {
			t.Fatalf("status=%d code=%s", r.status, r.code)
		}
	})

	t.Run("access 过期:16m 后 AUTH_TOKEN_EXPIRED,refresh 仍可用", func(t *testing.T) {
		h.do(t, http.MethodPost, "/v1/identity/register", confRegisterBody("acce@b.co", "password8", ""), "", "")
		s := h.confLogin(t, "acce@b.co", "password8", "")
		h.clock.Advance(16 * time.Minute) // access TTL 默认 15m
		r := h.do(t, http.MethodGet, "/v1/identity/session", "", confStr(t, s, "accessToken"), "")
		if r.status != http.StatusUnauthorized || r.code != "AUTH_TOKEN_EXPIRED" {
			t.Fatalf("status=%d code=%s", r.status, r.code)
		}
		rr := h.do(t, http.MethodPost, "/v1/identity/refresh", `{"refreshToken":"`+confStr(t, s, "refreshToken")+`"}`, "", "")
		if rr.status != http.StatusOK {
			t.Fatalf("refresh 应仍可用: %d code=%s", rr.status, rr.code)
		}
	})

	t.Run("logout 幂等吊销", func(t *testing.T) {
		h.do(t, http.MethodPost, "/v1/identity/register", confRegisterBody("bye@b.co", "password8", ""), "", "")
		s := h.confLogin(t, "bye@b.co", "password8", "")
		tok, ref := confStr(t, s, "accessToken"), confStr(t, s, "refreshToken")

		if r := h.do(t, http.MethodPost, "/v1/identity/logout", "", tok, ""); r.status != http.StatusOK {
			t.Fatalf("logout status = %d code=%s", r.status, r.code)
		}
		if r := h.do(t, http.MethodGet, "/v1/identity/session", "", tok, ""); r.status != http.StatusUnauthorized || r.code != "AUTH_TOKEN_REVOKED" {
			t.Fatalf("吊销后 access: %d %s", r.status, r.code)
		}
		if r := h.do(t, http.MethodPost, "/v1/identity/refresh", `{"refreshToken":"`+ref+`"}`, "", ""); r.status != http.StatusUnauthorized || r.code != "AUTH_TOKEN_REVOKED" {
			t.Fatalf("吊销后 refresh: %d %s", r.status, r.code)
		}
	})

	t.Run("设备上限:第 3 台 403,已绑可重登", func(t *testing.T) {
		h.do(t, http.MethodPost, "/v1/identity/register", confRegisterBody("dev@b.co", "password8", ""), "", "")
		for _, d := range []string{"d1", "d2"} {
			if s := h.confLogin(t, "dev@b.co", "password8", d); s["account"].(map[string]any)["id"] == "" {
				t.Fatalf("%s 登录失败", d)
			}
		}
		r := h.do(t, http.MethodPost, "/v1/identity/login", confRegisterBody("dev@b.co", "password8", "d3"), "", "")
		if r.status != http.StatusForbidden || r.code != "AUTH_DEVICE_LIMIT" {
			t.Fatalf("status=%d code=%s, 期望 403 AUTH_DEVICE_LIMIT", r.status, r.code)
		}
		if s := h.confLogin(t, "dev@b.co", "password8", "d1"); s["account"].(map[string]any)["id"] == "" {
			t.Fatal("d1 重登失败")
		}
	})

	t.Run("解绑设备:该设备会话全死,他台不受影响", func(t *testing.T) {
		h.do(t, http.MethodPost, "/v1/identity/register", confRegisterBody("ub@b.co", "password8", ""), "", "")
		s1 := h.confLogin(t, "ub@b.co", "password8", "d1")
		s2 := h.confLogin(t, "ub@b.co", "password8", "d2")
		tok1, tok2 := confStr(t, s1, "accessToken"), confStr(t, s2, "accessToken")

		if r := h.do(t, http.MethodDelete, "/v1/identity/devices/d1", "", tok1, ""); r.status != http.StatusOK {
			t.Fatalf("解绑 status = %d code=%s", r.status, r.code)
		}
		if r := h.do(t, http.MethodGet, "/v1/identity/session", "", tok1, ""); r.status != http.StatusUnauthorized || r.code != "AUTH_TOKEN_REVOKED" {
			t.Fatalf("d1 会话: status=%d code=%s", r.status, r.code)
		}
		if r := h.do(t, http.MethodGet, "/v1/identity/session", "", tok2, ""); r.status != http.StatusOK {
			t.Fatalf("d2 会话不受影响: status=%d", r.status)
		}
		if r := h.do(t, http.MethodDelete, "/v1/identity/devices/d1", "", tok2, ""); r.status != http.StatusNotFound || r.code != "COMMON_NOT_FOUND" {
			t.Fatalf("重复解绑: status=%d code=%s", r.status, r.code)
		}
		if s := h.confLogin(t, "ub@b.co", "password8", "d1"); s["account"].(map[string]any)["id"] == "" {
			t.Fatal("d1 重绑登录失败")
		}
	})

	t.Run("设备列表", func(t *testing.T) {
		h.do(t, http.MethodPost, "/v1/identity/register", confRegisterBody("dl@b.co", "password8", ""), "", "")
		s := h.confLogin(t, "dl@b.co", "password8", "d1")
		r := h.do(t, http.MethodGet, "/v1/identity/devices", "", confStr(t, s, "accessToken"), "")
		if r.status != http.StatusOK {
			t.Fatalf("devices status = %d code=%s", r.status, r.code)
		}
		var list struct {
			Items []map[string]any `json:"items"`
		}
		if err := json.Unmarshal(r.data, &list); err != nil {
			t.Fatalf("解析 devices: %v", err)
		}
		if len(list.Items) != 1 || list.Items[0]["deviceId"] != "d1" {
			t.Fatalf("devices = %v", list.Items)
		}
	})

	t.Run("scope 不符:400 SCOPE_MISMATCH", func(t *testing.T) {
		h.do(t, http.MethodPost, "/v1/identity/register", confRegisterBody("sc@b.co", "password8", ""), "", "")
		s := h.confLogin(t, "sc@b.co", "password8", "")
		r := h.do(t, http.MethodGet, "/v1/identity/session", "", confStr(t, s, "accessToken"), "game_b")
		if r.status != http.StatusBadRequest || r.code != "SCOPE_MISMATCH" {
			t.Fatalf("status=%d code=%s, 期望 400 SCOPE_MISMATCH", r.status, r.code)
		}
	})

	t.Run("路由兜底:方法不符 400/未知路径 404/未认证 401", func(t *testing.T) {
		if r := h.do(t, http.MethodGet, "/v1/identity/register", "", "", ""); r.status != http.StatusBadRequest || r.code != "COMMON_INVALID_ARGUMENT" {
			t.Fatalf("方法不符: status=%d code=%s", r.status, r.code)
		}
		if r := h.do(t, http.MethodGet, "/v1/identity/nope", "", "", ""); r.status != http.StatusNotFound || r.code != "COMMON_NOT_FOUND" {
			t.Fatalf("未知路径: status=%d code=%s", r.status, r.code)
		}
		if r := h.do(t, http.MethodGet, "/v1/identity/session", "", "", ""); r.status != http.StatusUnauthorized || r.code != "COMMON_UNAUTHENTICATED" {
			t.Fatalf("未认证: status=%d code=%s", r.status, r.code)
		}
	})

	t.Run("跨域认证:身份源 token 经 SwitchableVerifier 供 herald 用", func(t *testing.T) {
		h.do(t, http.MethodPost, "/v1/identity/register", confRegisterBody("xd@b.co", "password8", ""), "", "")
		s := h.confLogin(t, "xd@b.co", "password8", "")
		r := h.do(t, http.MethodGet, "/v1/announcements", "", confStr(t, s, "accessToken"), "")
		if r.status != http.StatusOK {
			t.Fatalf("herald 应认身份源 token: status=%d code=%s", r.status, r.code)
		}
		bad := h.do(t, http.MethodGet, "/v1/announcements", "", "forged-token", "")
		if bad.status != http.StatusUnauthorized {
			t.Fatalf("伪造 token 应 401, got %d", bad.status)
		}
	})
}

// TestIdentityContractConformance 一致性主测:两个实现同一场景全绿。
func TestIdentityContractConformance(t *testing.T) {
	t.Run("自建 account(不透明随机令牌)", func(t *testing.T) {
		h := newConformanceGateway(t, func(clock func() time.Time) (providers.Handler, auth.Verifier) {
			acc := account.New(account.Options{
				Iterations:    1000,
				MaxDevices:    2,
				RatePerMinute: 1000,
				RateBurst:     1000,
				Clock:         clock,
			})
			return acc, acc.Verifier()
		})
		runIdentityContract(t, h)
	})

	t.Run("accountalt(签名自验证令牌)", func(t *testing.T) {
		h := newConformanceGateway(t, func(clock func() time.Time) (providers.Handler, auth.Verifier) {
			alt := accountalt.New(accountalt.Options{
				Iterations:    1000,
				MaxDevices:    2,
				RatePerMinute: 1000,
				RateBurst:     1000,
				Clock:         clock,
			})
			return alt, alt.Verifier()
		})
		runIdentityContract(t, h)
	})
}

// TestIdentityRateLimitConformance 敏感端点限流口径一致(契约 auth.md「基础限流」)。
func TestIdentityRateLimitConformance(t *testing.T) {
	for _, tc := range []struct {
		name        string
		newIdentity func(clock func() time.Time) (providers.Handler, auth.Verifier)
	}{
		{"自建 account", func(clock func() time.Time) (providers.Handler, auth.Verifier) {
			acc := account.New(account.Options{Iterations: 1000, RatePerMinute: 3, RateBurst: 3, Clock: clock})
			return acc, acc.Verifier()
		}},
		{"accountalt", func(clock func() time.Time) (providers.Handler, auth.Verifier) {
			alt := accountalt.New(accountalt.Options{Iterations: 1000, RatePerMinute: 3, RateBurst: 3, Clock: clock})
			return alt, alt.Verifier()
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newConformanceGateway(t, tc.newIdentity)
			body := confRegisterBody("no@b.co", "whatever123", "")
			for i := 0; i < 3; i++ {
				if r := h.do(t, http.MethodPost, "/v1/identity/login", body, "", ""); r.status != http.StatusUnauthorized {
					t.Fatalf("第 %d 次: status=%d, 期望 401", i+1, r.status)
				}
			}
			r := h.do(t, http.MethodPost, "/v1/identity/login", body, "", "")
			if r.status != http.StatusTooManyRequests || r.code != "RATE_LIMITED" || !r.retryable {
				t.Fatalf("status=%d code=%s retryable=%v, 期望 429 RATE_LIMITED true", r.status, r.code, r.retryable)
			}
			if r.header.Get("Retry-After") == "" {
				t.Fatal("429 应带 Retry-After")
			}
		})
	}
}
