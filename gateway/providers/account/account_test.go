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
	"github.com/cuihairu/courier/gateway/providers/hooks"
	"github.com/cuihairu/courier/gateway/providers/oauth"
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

// --- 注销与撤销(v1.1,auth.md POST /account/delete + /account/delete/cancel)---

func deleteBody(t *testing.T, r resp) deleteDTO {
	t.Helper()
	var d deleteDTO
	if err := json.Unmarshal(r.data, &d); err != nil {
		t.Fatalf("解析 delete data 失败: %v (raw=%s)", err, r.data)
	}
	return d
}

const pathDelete = "/v1/identity/account/delete"
const pathDeleteCancel = "/v1/identity/account/delete/cancel"

func TestDeleteGracePeriodAndCancel(t *testing.T) {
	h := newHarness(t, func(o *Options) { o.DeleteGrace = time.Hour })
	s1 := h.mustRegister(t, "del@b.co", "password8", "dev-1")
	s2 := h.login(t, "del@b.co", "password8", "dev-2")
	var cancelToken string

	t.Run("受理:PENDING + cancelToken + 全部会话吊销", func(t *testing.T) {
		r := h.postAuth(t, pathDelete, "", s1.AccessToken)
		if r.status != http.StatusOK {
			t.Fatalf("status=%d code=%s", r.status, r.code)
		}
		d := deleteBody(t, r)
		if d.Status != "PENDING_DELETE" || !d.PendingDelete || d.DeletedAt != nil || !tokenRe.MatchString(d.CancelToken) {
			t.Fatalf("deleteDTO = %+v", d)
		}
		cancelToken = d.CancelToken
		for name, tok := range map[string]string{"dev-1": s1.AccessToken, "dev-2": s2.AccessToken} {
			if rr := h.getAuth(t, "/v1/identity/session", tok); rr.code != "AUTH_TOKEN_REVOKED" {
				t.Fatalf("%s 旧会话应已吊销: code=%s", name, rr.code)
			}
		}
	})

	t.Run("宽限期内登录被拒(AUTH_ACCOUNT_PENDING_DELETE)", func(t *testing.T) {
		r := h.post(t, "/v1/identity/login", registerBody("del@b.co", "password8", ""))
		if r.status != http.StatusForbidden || r.code != "AUTH_ACCOUNT_PENDING_DELETE" {
			t.Fatalf("status=%d code=%s", r.status, r.code)
		}
	})

	t.Run("撤销:错 token 401,对 token 200 且一次性,撤销后可重登", func(t *testing.T) {
		r := h.post(t, pathDeleteCancel, `{"cancelToken":"junk"}`)
		if r.status != http.StatusUnauthorized || r.code != "AUTH_DELETE_TOKEN_INVALID" {
			t.Fatalf("junk: status=%d code=%s", r.status, r.code)
		}
		ok := h.post(t, pathDeleteCancel, `{"cancelToken":"`+cancelToken+`"}`)
		if ok.status != http.StatusOK {
			t.Fatalf("撤销失败: status=%d code=%s", ok.status, ok.code)
		}
		if re := h.post(t, pathDeleteCancel, `{"cancelToken":"`+cancelToken+`"}`); re.code != "AUTH_DELETE_TOKEN_INVALID" {
			t.Fatalf("token 应一次性: code=%s", re.code)
		}
		s3 := h.login(t, "del@b.co", "password8", "dev-1") // 撤销后可登录
		if s3.Account.Status != "ACTIVE" {
			t.Fatalf("撤销后 status = %s", s3.Account.Status)
		}
		if rr := h.getAuth(t, "/v1/identity/session", s1.AccessToken); rr.code != "AUTH_TOKEN_REVOKED" {
			t.Fatalf("已吊销会话不应随撤销恢复: code=%s", rr.code)
		}
	})

	t.Run("重复受理重签 token,旧 token 作废", func(t *testing.T) {
		s4 := h.login(t, "del@b.co", "password8", "dev-2")
		tokA := deleteBody(t, h.postAuth(t, pathDelete, "", s4.AccessToken)).CancelToken
		if h.post(t, pathDeleteCancel, `{"cancelToken":"`+tokA+`"}`).status != http.StatusOK {
			t.Fatal("第一次撤销应成功")
		}
		s5 := h.login(t, "del@b.co", "password8", "dev-1")
		tokB := deleteBody(t, h.postAuth(t, pathDelete, "", s5.AccessToken)).CancelToken
		if tokB == tokA {
			t.Fatal("重签 token 应不同")
		}
		if r := h.post(t, pathDeleteCancel, `{"cancelToken":"`+tokA+`"}`); r.code != "AUTH_DELETE_TOKEN_INVALID" {
			t.Fatalf("旧 token 应作废: code=%s", r.code)
		}
		if r := h.post(t, pathDeleteCancel, `{"cancelToken":"`+tokB+`"}`); r.status != http.StatusOK {
			t.Fatalf("新 token 应有效: code=%s", r.code)
		}
	})
}

func TestDeleteImmediateGuestAndEmailZeroGrace(t *testing.T) {
	t.Run("游客恒立即删,同设备再进 = 新游客", func(t *testing.T) {
		h := newHarness(t, nil) // GUEST 不吃宽限期(契约:恒立即)
		gs := h.post(t, "/v1/identity/guest", `{"deviceId":"g-dev-1"}`).dataSession(t)
		d := deleteBody(t, h.postAuth(t, pathDelete, "", gs.AccessToken))
		if d.Status != "DELETED" || d.DeletedAt == nil || d.CancelToken != "" || d.PendingDelete {
			t.Fatalf("deleteDTO = %+v", d)
		}
		gs2 := h.post(t, "/v1/identity/guest", `{"deviceId":"g-dev-1"}`).dataSession(t)
		if gs2.Account.ID == gs.Account.ID || gs2.Account.Status != "ACTIVE" {
			t.Fatalf("同设备应得新游客账号: old=%s new=%s/%s",
				gs.Account.ID, gs2.Account.ID, gs2.Account.Status)
		}
	})

	t.Run("EMAIL grace=0 立即删:匿名化,同邮箱可重注册,钩子触发", func(t *testing.T) {
		h := newHarness(t, nil) // 0 = EMAIL 也立即删(契约 0–30d)
		deleted := make(chan string, 1)
		cancel := hooks.Register(hooks.AccountDeleted, func(id string) { deleted <- id })
		defer cancel()
		s := h.mustRegister(t, "zap@b.co", "password8", "")
		d := deleteBody(t, h.postAuth(t, pathDelete, "", s.AccessToken))
		if d.Status != "DELETED" || d.DeletedAt == nil || d.CancelToken != "" {
			t.Fatalf("deleteDTO = %+v", d)
		}
		select {
		case id := <-deleted:
			if id != s.Account.ID {
				t.Fatalf("钩子账号不符: %s", id)
			}
		default:
			t.Fatal("立即删应触发 account.deleted 钩子")
		}
		if r := h.post(t, "/v1/identity/login", registerBody("zap@b.co", "password8", "")); r.code != "AUTH_INVALID_CREDENTIALS" {
			t.Fatalf("匿名化后登录应按未知账号: code=%s", r.code)
		}
		if s2 := h.mustRegister(t, "zap@b.co", "password8", ""); s2.Account.ID == s.Account.ID {
			t.Fatal("同邮箱重注册应得到新账号")
		}
	})
}

func TestDeleteFinalizeAfterGrace(t *testing.T) {
	h := newHarness(t, func(o *Options) { o.DeleteGrace = time.Hour })
	deleted := make(chan string, 1)
	cancel := hooks.Register(hooks.AccountDeleted, func(id string) { deleted <- id })
	defer cancel()
	s := h.mustRegister(t, "late@b.co", "password8", "")
	if d := deleteBody(t, h.postAuth(t, pathDelete, "", s.AccessToken)); d.Status != "PENDING_DELETE" {
		t.Fatalf("status = %s", d.Status)
	}
	select {
	case id := <-deleted:
		t.Fatalf("宽限期内不应触发钩子,却收到 %s", id)
	default:
	}
	h.clock.Advance(2 * time.Hour)
	// 终态化是惰性的(login 前顺手):匿名化后按未知账号(防枚举一致)。
	r := h.post(t, "/v1/identity/login", registerBody("late@b.co", "password8", ""))
	if r.status != http.StatusUnauthorized || r.code != "AUTH_INVALID_CREDENTIALS" {
		t.Fatalf("宽限期满后登录: status=%d code=%s", r.status, r.code)
	}
	select {
	case id := <-deleted:
		if id != s.Account.ID {
			t.Fatalf("钩子账号不符: %s", id)
		}
	default:
		t.Fatal("终态化应触发 account.deleted 钩子")
	}
	h.mustRegister(t, "late@b.co", "password8", "") // PII 已清,同邮箱可重注册
}

// --- v1.1 oauth.md(第三方身份;校验器经 oauth 注册表装配,测试用确定性实现) ---

func exchangeBody(provider, token, tokenType, device string) string {
	b := `{"provider":"` + provider + `","token":"` + token + `"`
	if tokenType != "" {
		b += `,"tokenType":"` + tokenType + `"`
	}
	if device != "" {
		b += `,"deviceId":"` + device + `","platform":"ios"`
	}
	return b + `}`
}

func withDefaultVerifier(t *testing.T) {
	t.Helper()
	oauth.SetDefault(oauth.DeterministicVerifier{})
	t.Cleanup(func() { oauth.SetDefault(nil) })
}

func TestOAuthExchangeNewAccountAndRelogin(t *testing.T) {
	withDefaultVerifier(t)
	h := newHarness(t, nil)

	r := h.post(t, "/v1/identity/oauth/exchange", exchangeBody("google", "ok.sub-1", "", "dev-1"))
	if r.status != http.StatusOK {
		t.Fatalf("exchange status = %d code=%s", r.status, r.code)
	}
	s := r.dataSession(t)
	if s.Account.Type != "OAUTH" || s.Account.Status != "ACTIVE" {
		t.Fatalf("账号类型/状态不符: %+v", s.Account)
	}
	if s.Account.Email != nil {
		t.Fatalf("OAUTH 账号不应有 email: %v", *s.Account.Email)
	}
	if !tokenRe.MatchString(s.AccessToken) || s.DeviceID != "dev-1" {
		t.Fatalf("会话形状不符: access=%q deviceId=%q", s.AccessToken, s.DeviceID)
	}
	accID := s.Account.ID

	// 重复 exchange 同 (provider, subject):登录同账号,签发新会话。
	h.clock.Advance(time.Minute)
	r2 := h.post(t, "/v1/identity/oauth/exchange", exchangeBody("google", "ok.sub-1", "", "dev-1"))
	if r2.status != http.StatusOK {
		t.Fatalf("re-exchange status = %d", r2.status)
	}
	s2 := r2.dataSession(t)
	if s2.Account.ID != accID {
		t.Fatalf("同 subject 应登录同账号: %s ≠ %s", s2.Account.ID, accID)
	}
	if s2.AccessToken == s.AccessToken {
		t.Fatal("每次 exchange 应签发新会话")
	}

	// tokenType 缺省 = ID_TOKEN;显式枚举合法。
	for _, tt := range []string{"", "ID_TOKEN", "ACCESS_TOKEN", "CODE"} {
		r3 := h.post(t, "/v1/identity/oauth/exchange", exchangeBody("google", "ok.sub-1", tt, ""))
		if r3.status != http.StatusOK {
			t.Fatalf("tokenType %q: status = %d code=%s", tt, r3.status, r3.code)
		}
	}
	// tokenType 非法枚举 → 400。
	r4 := h.post(t, "/v1/identity/oauth/exchange", exchangeBody("google", "ok.sub-1", "JWT", ""))
	if r4.status != http.StatusBadRequest || r4.code != "COMMON_INVALID_ARGUMENT" {
		t.Fatalf("tokenType 非法: status=%d code=%s", r4.status, r4.code)
	}
}

func TestOAuthIdentityInvalidAndProviderUnknown(t *testing.T) {
	withDefaultVerifier(t)
	h := newHarness(t, nil)

	// 伪造凭证 → OAUTH_IDENTITY_INVALID(401,防枚举不区分无效/过期)。
	r := h.post(t, "/v1/identity/oauth/exchange", exchangeBody("google", "forged-token", "", ""))
	if r.status != http.StatusUnauthorized || r.code != "OAUTH_IDENTITY_INVALID" || r.retryable {
		t.Fatalf("伪造凭证: status=%d code=%s retryable=%v", r.status, r.code, r.retryable)
	}
	// 缺 token → 400。
	r = h.post(t, "/v1/identity/oauth/exchange", exchangeBody("google", "", "", ""))
	if r.status != http.StatusBadRequest || r.code != "COMMON_INVALID_ARGUMENT" {
		t.Fatalf("缺 token: status=%d code=%s", r.status, r.code)
	}
	// provider 格式不符(大写/下划线/超长)→ OAUTH_PROVIDER_UNKNOWN(未注册同码)。
	for _, bad := range []string{"Google", "bad_provider", ""} {
		r = h.post(t, "/v1/identity/oauth/exchange", exchangeBody(bad, "ok.x", "", ""))
		if r.status != http.StatusBadRequest || r.code != "OAUTH_PROVIDER_UNKNOWN" {
			t.Fatalf("provider %q: status=%d code=%s", bad, r.status, r.code)
		}
	}
}

func TestOAuthProviderRegistry(t *testing.T) {
	// 未按名注册且无缺省校验器 → fail-closed OAUTH_PROVIDER_UNKNOWN。
	h := newHarness(t, nil)
	r := h.post(t, "/v1/identity/oauth/exchange", exchangeBody("google", "ok.sub-1", "", ""))
	if r.status != http.StatusBadRequest || r.code != "OAUTH_PROVIDER_UNKNOWN" {
		t.Fatalf("未注册 fail-closed: status=%d code=%s", r.status, r.code)
	}
	// 按名注册优先于缺省:按名注册的恒拒实现生效。
	withDefaultVerifier(t)
	oauth.Register("apple", rejectingVerifier{})
	t.Cleanup(func() { oauth.Register("apple", nil) })
	r = h.post(t, "/v1/identity/oauth/exchange", exchangeBody("apple", "ok.sub-1", "", ""))
	if r.status != http.StatusUnauthorized || r.code != "OAUTH_IDENTITY_INVALID" {
		t.Fatalf("按名注册优先: status=%d code=%s", r.status, r.code)
	}
	// 管理面显式注销(注册 nil)= 回落缺省校验器。
	oauth.Register("apple", nil)
	r = h.post(t, "/v1/identity/oauth/exchange", exchangeBody("apple", "ok.sub-2", "", ""))
	if r.status != http.StatusOK {
		t.Fatalf("注销后应回落缺省: status=%d code=%s", r.status, r.code)
	}
}

// rejectingVerifier 恒拒校验器(测试按名注册优先级)。
type rejectingVerifier struct{}

func (rejectingVerifier) Verify(provider, token, tokenType string) (string, error) {
	return "", oauth.ErrInvalidIdentity
}

func TestOAuthBindListUnbind(t *testing.T) {
	withDefaultVerifier(t)
	h := newHarness(t, nil)
	acc := h.mustRegister(t, "a@b.co", "password8", "")
	bearer := acc.AccessToken

	// bind:返回 {provider, createdAt} 形状。
	r := h.postAuth(t, "/v1/identity/oauth/bind", `{"provider":"google","token":"ok.sub-9"}`, bearer)
	if r.status != http.StatusOK {
		t.Fatalf("bind status = %d code=%s", r.status, r.code)
	}
	var bind struct {
		Provider  string `json:"provider"`
		CreatedAt string `json:"createdAt"`
	}
	if err := json.Unmarshal(r.data, &bind); err != nil || bind.Provider != "google" || bind.CreatedAt == "" {
		t.Fatalf("bind 响应形状: raw=%s err=%v", r.data, err)
	}

	// 重复绑定同 provider → COMMON_INVALID_ARGUMENT。
	r = h.postAuth(t, "/v1/identity/oauth/bind", `{"provider":"google","token":"ok.sub-9"}`, bearer)
	if r.status != http.StatusBadRequest || r.code != "COMMON_INVALID_ARGUMENT" {
		t.Fatalf("重复绑定: status=%d code=%s", r.status, r.code)
	}
	// 凭证无效 → OAUTH_IDENTITY_INVALID。
	r = h.postAuth(t, "/v1/identity/oauth/bind", `{"provider":"google","token":"junk"}`, bearer)
	if r.status != http.StatusUnauthorized || r.code != "OAUTH_IDENTITY_INVALID" {
		t.Fatalf("bind 伪造凭证: status=%d code=%s", r.status, r.code)
	}

	// 再绑第二个 provider;列表按 createdAt 升序且不返回 subject。
	h.clock.Advance(time.Minute)
	if r := h.postAuth(t, "/v1/identity/oauth/bind", `{"provider":"apple","token":"ok.sub-10"}`, bearer); r.status != http.StatusOK {
		t.Fatalf("bind apple status = %d", r.status)
	}
	r = h.getAuth(t, "/v1/identity/oauth/bindings", bearer)
	if r.status != http.StatusOK {
		t.Fatalf("bindings status = %d", r.status)
	}
	var list struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(r.data, &list); err != nil {
		t.Fatalf("解析 bindings: %v", err)
	}
	if len(list.Items) != 2 {
		t.Fatalf("应有 2 条绑定: %s", r.data)
	}
	if list.Items[0]["provider"] != "google" || list.Items[1]["provider"] != "apple" {
		t.Fatalf("列表应按 createdAt 升序: %s", r.data)
	}
	for _, item := range list.Items {
		if _, leak := item["subject"]; leak {
			t.Fatal("列表不得返回 subject")
		}
	}

	// 解绑幂等;解绑后同 subject 再 exchange = 新 OAUTH 账号。
	for i := 0; i < 2; i++ {
		r = h.do(t, http.MethodDelete, "/v1/identity/oauth/bindings/google", "", bearer, "")
		if r.status != http.StatusOK {
			t.Fatalf("unbind[%d] status = %d", i, r.status)
		}
	}
	r = h.getAuth(t, "/v1/identity/oauth/bindings", bearer)
	if err := json.Unmarshal(r.data, &list); err != nil || len(list.Items) != 1 {
		t.Fatalf("解绑后应剩 1 条: %s", r.data)
	}
	r = h.post(t, "/v1/identity/oauth/exchange", exchangeBody("google", "ok.sub-9", "", ""))
	if r.status != http.StatusOK {
		t.Fatalf("解绑后 exchange status = %d", r.status)
	}
	s := r.dataSession(t)
	if s.Account.ID == acc.Account.ID || s.Account.Type != "OAUTH" {
		t.Fatalf("解绑后同 subject 应为新 OAUTH 账号: %+v", s.Account)
	}
}

func TestOAuthExchangeSubjectTakenAndDeviceLimit(t *testing.T) {
	withDefaultVerifier(t)
	h := newHarness(t, nil)
	acc := h.mustRegister(t, "a@b.co", "password8", "")

	// subject 已属其他账号 → OAUTH_IDENTITY_TAKEN(409)。
	h.post(t, "/v1/identity/oauth/exchange", exchangeBody("google", "ok.sub-t", "", ""))
	r := h.postAuth(t, "/v1/identity/oauth/bind", `{"provider":"google","token":"ok.sub-t"}`, acc.AccessToken)
	if r.status != http.StatusConflict || r.code != "OAUTH_IDENTITY_TAKEN" || r.retryable {
		t.Fatalf("subject 被占: status=%d code=%s retryable=%v", r.status, r.code, r.retryable)
	}

	// 设备超限(按账号计):登录路径换新设备 → AUTH_DEVICE_LIMIT。
	h2 := newHarness(t, func(o *Options) { o.MaxDevices = 1 })
	if r := h2.post(t, "/v1/identity/oauth/exchange", exchangeBody("google", "ok.s1", "", "dev-1")); r.status != http.StatusOK {
		t.Fatalf("首次 exchange status = %d", r.status)
	}
	r = h2.post(t, "/v1/identity/oauth/exchange", exchangeBody("google", "ok.s1", "", "dev-2"))
	if r.status != http.StatusForbidden || r.code != "AUTH_DEVICE_LIMIT" {
		t.Fatalf("设备超限: status=%d code=%s", r.status, r.code)
	}
	// 原设备仍可登录。
	r = h2.post(t, "/v1/identity/oauth/exchange", exchangeBody("google", "ok.s1", "", "dev-1"))
	if r.status != http.StatusOK {
		t.Fatalf("原设备 re-exchange status = %d", r.status)
	}
}

func TestOAuthCapabilityDisabled(t *testing.T) {
	withDefaultVerifier(t)
	h := newHarness(t, func(o *Options) { o.OAuthDisabled = true })
	r := h.post(t, "/v1/identity/oauth/exchange", exchangeBody("google", "ok.s1", "", ""))
	if r.status != http.StatusNotImplemented || r.code != "COMMON_CAPABILITY_DISABLED" || r.retryable {
		t.Fatalf("域关闭: status=%d code=%s retryable=%v", r.status, r.code, r.retryable)
	}
	acc := h.mustRegister(t, "a@b.co", "password8", "")
	r = h.postAuth(t, "/v1/identity/oauth/bind", `{"provider":"google","token":"ok.s1"}`, acc.AccessToken)
	if r.status != http.StatusNotImplemented || r.code != "COMMON_CAPABILITY_DISABLED" {
		t.Fatalf("bind 域关闭: status=%d code=%s", r.status, r.code)
	}
}

// TestOAuthPendingStoreDefense PENDING 账号绑定拒绝(防御竞态:受理即吊销会话,
// HTTP 层正常不可达,存储层仍拒)。
func TestOAuthPendingStoreDefense(t *testing.T) {
	withDefaultVerifier(t)
	// grace 须显式给值:0 = 立即删(EMAIL 也立即),不会进入 PENDING。
	h := newHarness(t, func(o *Options) { o.DeleteGrace = 24 * time.Hour })
	acc := h.mustRegister(t, "a@b.co", "password8", "")
	if _, _, _, err := h.p.store.RequestDelete(acc.Account.ID, h.p.store.Options().DeleteGrace); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := h.p.store.BindOAuth(acc.Account.ID, "google", "sub-x"); err != oauth.ErrPendingDelete {
		t.Fatalf("PENDING bind 应拒: %v", err)
	}
}
