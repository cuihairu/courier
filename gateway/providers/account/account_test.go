package account

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cuihairu/courier/gateway/middleware"
	"github.com/cuihairu/courier/gateway/scope"
)

// fakeClock 可推进的测试时钟(存储与验证器共享)。
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock { return &fakeClock{t: time.Now()} }
func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// newHarness 组装 provider + scope 中间件(模拟生产链:scope 先于 provider)。
type harness struct {
	p     *AccountProvider
	h     http.Handler
	clock *fakeClock
}

func newHarness(t *testing.T, mutate func(*Options)) *harness {
	t.Helper()
	clock := newFakeClock()
	opts := Options{
		Iterations:    1000, // 测试调低 PBKDF2 迭代
		RatePerMinute: 1000, // 除非测限流,放开敏感端点限流
		RateBurst:     1000,
		Clock:         clock.Now,
	}
	if mutate != nil {
		mutate(&opts)
	}
	p := New(opts)
	return &harness{p: p, h: middleware.Scope()(p), clock: clock}
}

var tokenRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

type resp struct {
	status    int
	code      string
	retryable bool
	traceID   string
	data      json.RawMessage
	header    http.Header
}

func (r resp) dataSession(t *testing.T) sessionDTO {
	t.Helper()
	var s sessionDTO
	if err := json.Unmarshal(r.data, &s); err != nil {
		t.Fatalf("解析会话 data 失败: %v (raw=%s)", err, r.data)
	}
	return s
}

func (h *harness) do(t *testing.T, method, path, body, bearer, gameID string) resp {
	t.Helper()
	var rd *strings.Reader
	if body == "" {
		rd = strings.NewReader("")
	} else {
		rd = strings.NewReader(body)
	}
	r := httptest.NewRequest(method, path, rd)
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		r.Header.Set("Authorization", "Bearer "+bearer)
	}
	if gameID == "" {
		gameID = "game_a"
	}
	r.Header.Set(scope.HeaderGameID, gameID)
	r.Header.Set(scope.HeaderEnv, "prod")
	rec := httptest.NewRecorder()
	h.h.ServeHTTP(rec, r)

	out := resp{status: rec.Code, header: rec.Header().Clone()}
	var env struct {
		Error *struct {
			Code      string `json:"code"`
			Retryable bool   `json:"retryable"`
		} `json:"error"`
		TraceID string          `json:"traceId"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("响应非 JSON: %v (body=%s)", err, rec.Body.String())
	}
	if env.Error != nil {
		out.code = env.Error.Code
		out.retryable = env.Error.Retryable
	}
	out.traceID = env.TraceID
	out.data = env.Data
	return out
}

func (h *harness) post(t *testing.T, path, body string) resp {
	return h.do(t, http.MethodPost, path, body, "", "")
}

func (h *harness) postAuth(t *testing.T, path, body, bearer string) resp {
	return h.do(t, http.MethodPost, path, body, bearer, "")
}

func (h *harness) getAuth(t *testing.T, path, bearer string) resp {
	return h.do(t, http.MethodGet, path, "", bearer, "")
}

func registerBody(email, password, device string) string {
	b := `{"email":"` + email + `","password":"` + password + `"`
	if device != "" {
		b += `,"deviceId":"` + device + `"`
	}
	return b + `}`
}

// mustRegister 注册并断言成功。
func (h *harness) mustRegister(t *testing.T, email, password, device string) sessionDTO {
	t.Helper()
	r := h.post(t, "/v1/identity/register", registerBody(email, password, device))
	if r.status != http.StatusOK {
		t.Fatalf("register status = %d code=%s", r.status, r.code)
	}
	return r.dataSession(t)
}

// login 便捷:邮箱登录并返回会话。
func (h *harness) login(t *testing.T, email, password, device string) sessionDTO {
	t.Helper()
	r := h.post(t, "/v1/identity/login", registerBody(email, password, device))
	if r.status != http.StatusOK {
		t.Fatalf("login status = %d code=%s", r.status, r.code)
	}
	return r.dataSession(t)
}

func TestRegisterAndLogin(t *testing.T) {
	h := newHarness(t, nil)

	t.Run("注册成功", func(t *testing.T) {
		r := h.post(t, "/v1/identity/register", registerBody("a@b.co", "password8", "dev-1"))
		if r.status != http.StatusOK {
			t.Fatalf("status = %d code=%s", r.status, r.code)
		}
		s := r.dataSession(t)
		if !tokenRe.MatchString(s.AccessToken) || !tokenRe.MatchString(s.RefreshToken) {
			t.Fatalf("token 应为 64-hex: %q", s.AccessToken)
		}
		if s.Account.Type != "EMAIL" || s.Account.Email == nil || *s.Account.Email != "a@b.co" {
			t.Fatalf("account = %+v", s.Account)
		}
		if s.DeviceID != "dev-1" {
			t.Fatalf("deviceId = %q, 期望 dev-1", s.DeviceID)
		}
	})

	t.Run("登录成功", func(t *testing.T) {
		s := h.login(t, "a@b.co", "password8", "")
		if s.Account.ID == "" {
			t.Fatal("account.id 为空")
		}
	})

	t.Run("防枚举:密码错与账号不存在返回一致", func(t *testing.T) {
		wrong := h.post(t, "/v1/identity/login", registerBody("a@b.co", "wrong-pass-1", ""))
		unknown := h.post(t, "/v1/identity/login", registerBody("ghost@b.co", "whatever123", ""))
		if wrong.status != http.StatusUnauthorized || wrong.code != "AUTH_INVALID_CREDENTIALS" {
			t.Fatalf("wrong: status=%d code=%s", wrong.status, wrong.code)
		}
		if unknown.status != wrong.status || unknown.code != wrong.code {
			t.Fatalf("unknown 与 wrong 不一致: %d/%s vs %d/%s",
				unknown.status, unknown.code, wrong.status, wrong.code)
		}
	})
}

func TestRegisterValidation(t *testing.T) {
	h := newHarness(t, nil)
	cases := map[string]string{
		"email 非法":    registerBody("not-an-email", "password8", ""),
		"密码过短":        registerBody("x@b.co", "short", ""),
		"deviceId 过长": registerBody("x@b.co", "password8", strings.Repeat("d", 129)),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			r := h.post(t, "/v1/identity/register", body)
			if r.status != http.StatusBadRequest || r.code != "COMMON_INVALID_ARGUMENT" || r.retryable {
				t.Fatalf("status=%d code=%s retryable=%v, 期望 400 COMMON_INVALID_ARGUMENT false",
					r.status, r.code, r.retryable)
			}
		})
	}
}

func TestEmailTaken(t *testing.T) {
	h := newHarness(t, nil)
	if r := h.post(t, "/v1/identity/register", registerBody("dup@b.co", "password8", "")); r.status != http.StatusOK {
		t.Fatalf("首次注册失败: %d", r.status)
	}
	r := h.post(t, "/v1/identity/register", registerBody("dup@b.co", "password9", ""))
	if r.status != http.StatusConflict || r.code != "AUTH_EMAIL_TAKEN" || r.retryable {
		t.Fatalf("status=%d code=%s, 期望 409 AUTH_EMAIL_TAKEN", r.status, r.code)
	}
}

func TestGuestIdempotent(t *testing.T) {
	h := newHarness(t, nil)
	s1 := h.post(t, "/v1/identity/guest", `{"deviceId":"g-dev","platform":"ios"}`).dataSession(t)
	s2 := h.post(t, "/v1/identity/guest", `{"deviceId":"g-dev"}`).dataSession(t)
	if s1.Account.ID != s2.Account.ID {
		t.Fatalf("同设备应回同一游客账号: %s vs %s", s1.Account.ID, s2.Account.ID)
	}
	if s1.Account.Type != "GUEST" || s1.Account.Email != nil {
		t.Fatalf("游客账号字段异常: %+v", s1.Account)
	}
	if r := h.post(t, "/v1/identity/guest", `{}`); r.status != http.StatusBadRequest {
		t.Fatalf("缺 deviceId 应 400, got %d", r.status)
	}
}

func TestBind(t *testing.T) {
	h := newHarness(t, nil)
	guest := h.post(t, "/v1/identity/guest", `{"deviceId":"b-dev"}`).dataSession(t)

	t.Run("游客转正", func(t *testing.T) {
		r := h.postAuth(t, "/v1/identity/bind", registerBody("bind@b.co", "password8", ""), guest.AccessToken)
		if r.status != http.StatusOK {
			t.Fatalf("status = %d code=%s", r.status, r.code)
		}
		var acc accountDTO
		if err := json.Unmarshal(r.data, &acc); err != nil {
			t.Fatalf("解析失败: %v", err)
		}
		if acc.Type != "EMAIL" || acc.Email == nil || *acc.Email != "bind@b.co" {
			t.Fatalf("account = %+v", acc)
		}
	})

	t.Run("转正后可用邮箱登录", func(t *testing.T) {
		s := h.login(t, "bind@b.co", "password8", "b-dev")
		if s.Account.ID != guest.Account.ID {
			t.Fatalf("bind 后账号应同一: %s vs %s", s.Account.ID, guest.Account.ID)
		}
	})

	t.Run("EMAIL 账号再 bind → 400", func(t *testing.T) {
		r := h.postAuth(t, "/v1/identity/bind", registerBody("again@b.co", "password8", ""), guest.AccessToken)
		if r.status != http.StatusBadRequest || r.code != "COMMON_INVALID_ARGUMENT" {
			t.Fatalf("status=%d code=%s", r.status, r.code)
		}
	})

	t.Run("邮箱被占 → 409", func(t *testing.T) {
		h.post(t, "/v1/identity/register", registerBody("taken@b.co", "password8", ""))
		g2 := h.post(t, "/v1/identity/guest", `{"deviceId":"b-dev-2"}`).dataSession(t)
		r := h.postAuth(t, "/v1/identity/bind", registerBody("taken@b.co", "password8", ""), g2.AccessToken)
		if r.status != http.StatusConflict || r.code != "AUTH_EMAIL_TAKEN" {
			t.Fatalf("status=%d code=%s", r.status, r.code)
		}
	})
}

func TestRefreshRotationAndReplay(t *testing.T) {
	h := newHarness(t, nil)
	h.mustRegister(t, "rot@b.co", "password8", "")
	s1 := h.login(t, "rot@b.co", "password8", "")

	t.Run("轮换:新 token 生成,旧 access 立即失效", func(t *testing.T) {
		r := h.post(t, "/v1/identity/refresh", `{"refreshToken":"`+s1.RefreshToken+`"}`)
		if r.status != http.StatusOK {
			t.Fatalf("status = %d code=%s", r.status, r.code)
		}
		s2 := r.dataSession(t)
		if s2.AccessToken == s1.AccessToken || s2.RefreshToken == s1.RefreshToken {
			t.Fatal("轮换应生成全新 token")
		}
		// 旧 access 死
		old := h.getAuth(t, "/v1/identity/session", s1.AccessToken)
		if old.status != http.StatusUnauthorized || old.code != "AUTH_INVALID_CREDENTIALS" {
			t.Fatalf("旧 access: status=%d code=%s", old.status, old.code)
		}
		// 新 access 活
		if got := h.getAuth(t, "/v1/identity/session", s2.AccessToken); got.status != http.StatusOK {
			t.Fatalf("新 access: status=%d code=%s", got.status, got.code)
		}
		// 重放旧 refresh → REUSED + 吊销整个会话
		replay := h.post(t, "/v1/identity/refresh", `{"refreshToken":"`+s1.RefreshToken+`"}`)
		if replay.status != http.StatusUnauthorized || replay.code != "AUTH_REFRESH_REUSED" {
			t.Fatalf("重放: status=%d code=%s", replay.status, replay.code)
		}
		// 会话已吊销:新 refresh 也死
		after := h.post(t, "/v1/identity/refresh", `{"refreshToken":"`+s2.RefreshToken+`"}`)
		if after.status != http.StatusUnauthorized || after.code != "AUTH_TOKEN_REVOKED" {
			t.Fatalf("吊销后: status=%d code=%s", after.status, after.code)
		}
	})
}

func TestRefreshUnknown(t *testing.T) {
	h := newHarness(t, nil)
	r := h.post(t, "/v1/identity/refresh", `{"refreshToken":"deadbeef"}`)
	if r.status != http.StatusUnauthorized || r.code != "AUTH_INVALID_CREDENTIALS" {
		t.Fatalf("status=%d code=%s", r.status, r.code)
	}
}

func TestRefreshExpired(t *testing.T) {
	h := newHarness(t, nil)
	h.mustRegister(t, "exp@b.co", "password8", "")
	s := h.login(t, "exp@b.co", "password8", "")
	h.clock.Advance(31 * 24 * time.Hour) // 超过 refresh TTL 默认 30d
	r := h.post(t, "/v1/identity/refresh", `{"refreshToken":"`+s.RefreshToken+`"}`)
	if r.status != http.StatusUnauthorized || r.code != "AUTH_TOKEN_EXPIRED" {
		t.Fatalf("status=%d code=%s", r.status, r.code)
	}
}

func TestAccessExpired(t *testing.T) {
	h := newHarness(t, nil)
	h.mustRegister(t, "acc-exp@b.co", "password8", "")
	s := h.login(t, "acc-exp@b.co", "password8", "")
	h.clock.Advance(16 * time.Minute) // 超过 access TTL 默认 15m
	r := h.getAuth(t, "/v1/identity/session", s.AccessToken)
	if r.status != http.StatusUnauthorized || r.code != "AUTH_TOKEN_EXPIRED" {
		t.Fatalf("status=%d code=%s", r.status, r.code)
	}
	// refresh 仍在有效期,可轮换
	rr := h.post(t, "/v1/identity/refresh", `{"refreshToken":"`+s.RefreshToken+`"}`)
	if rr.status != http.StatusOK {
		t.Fatalf("refresh 应仍可用: %d code=%s", rr.status, rr.code)
	}
}

func TestLogout(t *testing.T) {
	h := newHarness(t, nil)
	h.mustRegister(t, "bye@b.co", "password8", "")
	s := h.login(t, "bye@b.co", "password8", "")

	if r := h.postAuth(t, "/v1/identity/logout", "", s.AccessToken); r.status != http.StatusOK {
		t.Fatalf("logout status = %d", r.status)
	}
	access := h.getAuth(t, "/v1/identity/session", s.AccessToken)
	if access.status != http.StatusUnauthorized || access.code != "AUTH_TOKEN_REVOKED" {
		t.Fatalf("吊销后 access: %d %s", access.status, access.code)
	}
	refresh := h.post(t, "/v1/identity/refresh", `{"refreshToken":"`+s.RefreshToken+`"}`)
	if refresh.status != http.StatusUnauthorized || refresh.code != "AUTH_TOKEN_REVOKED" {
		t.Fatalf("吊销后 refresh: %d %s", refresh.status, refresh.code)
	}
}

func TestDeviceLimit(t *testing.T) {
	h := newHarness(t, func(o *Options) { o.MaxDevices = 2 })
	h.mustRegister(t, "dev@b.co", "password8", "")

	if s := h.login(t, "dev@b.co", "password8", "d1"); s.Account.ID == "" {
		t.Fatal("d1 登录失败")
	}
	if s := h.login(t, "dev@b.co", "password8", "d2"); s.Account.ID == "" {
		t.Fatal("d2 登录失败")
	}
	r := h.post(t, "/v1/identity/login", registerBody("dev@b.co", "password8", "d3"))
	if r.status != http.StatusForbidden || r.code != "AUTH_DEVICE_LIMIT" {
		t.Fatalf("status=%d code=%s, 期望 403 AUTH_DEVICE_LIMIT", r.status, r.code)
	}
	// 已绑定设备可重复登录
	if s := h.login(t, "dev@b.co", "password8", "d1"); s.Account.ID == "" {
		t.Fatal("d1 重登失败")
	}
}

func TestUnbindDevice(t *testing.T) {
	h := newHarness(t, func(o *Options) { o.MaxDevices = 2 })
	h.mustRegister(t, "ub@b.co", "password8", "")
	s1 := h.login(t, "ub@b.co", "password8", "d1")
	s2 := h.login(t, "ub@b.co", "password8", "d2")

	t.Run("解绑 d1", func(t *testing.T) {
		r := h.do(t, http.MethodDelete, "/v1/identity/devices/d1", "", s1.AccessToken, "")
		if r.status != http.StatusOK {
			t.Fatalf("status = %d code=%s", r.status, r.code)
		}
	})
	t.Run("d1 会话全部吊销", func(t *testing.T) {
		r := h.getAuth(t, "/v1/identity/session", s1.AccessToken)
		if r.status != http.StatusUnauthorized || r.code != "AUTH_TOKEN_REVOKED" {
			t.Fatalf("status=%d code=%s", r.status, r.code)
		}
	})
	t.Run("d2 会话不受影响", func(t *testing.T) {
		if r := h.getAuth(t, "/v1/identity/session", s2.AccessToken); r.status != http.StatusOK {
			t.Fatalf("status = %d", r.status)
		}
	})
	t.Run("重复解绑 → 404", func(t *testing.T) {
		r := h.do(t, http.MethodDelete, "/v1/identity/devices/d1", "", s2.AccessToken, "")
		if r.status != http.StatusNotFound || r.code != "COMMON_NOT_FOUND" {
			t.Fatalf("status=%d code=%s", r.status, r.code)
		}
	})
	t.Run("解绑后可重新绑定", func(t *testing.T) {
		if s := h.login(t, "ub@b.co", "password8", "d1"); s.Account.ID == "" {
			t.Fatal("d1 重绑登录失败")
		}
	})
}

func TestScopeMismatch(t *testing.T) {
	h := newHarness(t, nil)
	h.mustRegister(t, "sc@b.co", "password8", "")
	s := h.login(t, "sc@b.co", "password8", "")
	r := h.do(t, http.MethodGet, "/v1/identity/session", "", s.AccessToken, "game_b")
	if r.status != http.StatusBadRequest || r.code != "SCOPE_MISMATCH" {
		t.Fatalf("status=%d code=%s, 期望 400 SCOPE_MISMATCH", r.status, r.code)
	}
}

func TestSensitiveRateLimit(t *testing.T) {
	h := newHarness(t, func(o *Options) { o.RatePerMinute = 3; o.RateBurst = 3 })
	body := registerBody("no@b.co", "whatever123", "")
	for i := 0; i < 3; i++ {
		if r := h.post(t, "/v1/identity/login", body); r.status != http.StatusUnauthorized {
			t.Fatalf("第 %d 次: status=%d, 期望 401", i+1, r.status)
		}
	}
	r := h.post(t, "/v1/identity/login", body)
	if r.status != http.StatusTooManyRequests || r.code != "RATE_LIMITED" || !r.retryable {
		t.Fatalf("status=%d code=%s retryable=%v, 期望 429 RATE_LIMITED true", r.status, r.code, r.retryable)
	}
	if r.header.Get("Retry-After") == "" {
		t.Fatal("429 应带 Retry-After")
	}
}

func TestRouteFallbacks(t *testing.T) {
	h := newHarness(t, nil)
	t.Run("方法不符 → 400", func(t *testing.T) {
		r := h.do(t, http.MethodGet, "/v1/identity/register", "", "", "")
		if r.status != http.StatusBadRequest || r.code != "COMMON_INVALID_ARGUMENT" {
			t.Fatalf("status=%d code=%s", r.status, r.code)
		}
	})
	t.Run("未知路径 → 404", func(t *testing.T) {
		r := h.do(t, http.MethodGet, "/v1/identity/nope", "", "", "")
		if r.status != http.StatusNotFound || r.code != "COMMON_NOT_FOUND" {
			t.Fatalf("status=%d code=%s", r.status, r.code)
		}
	})
	t.Run("未认证访问受保护端点 → 401", func(t *testing.T) {
		r := h.do(t, http.MethodGet, "/v1/identity/session", "", "", "")
		if r.status != http.StatusUnauthorized || r.code != "COMMON_UNAUTHENTICATED" {
			t.Fatalf("status=%d code=%s", r.status, r.code)
		}
	})
}
