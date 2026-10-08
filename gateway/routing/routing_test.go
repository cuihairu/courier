package routing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/cuihairu/courier/gateway/middleware"
	"github.com/cuihairu/courier/gateway/providers"
	"github.com/cuihairu/courier/gateway/scope"
)

var traceIDRe = regexp.MustCompile(`^[0-9a-f]{32}$`)

// stubHandler 每能力域一个的测试 Handler:健康可翻转,响应回显自己的名字。
type stubHandler struct {
	name    string
	cap     providers.Capability
	healthy bool
}

func (s *stubHandler) Name() string                     { return s.name }
func (s *stubHandler) Capability() providers.Capability { return s.cap }
func (s *stubHandler) HealthCheck(context.Context) error {
	if s.healthy {
		return nil
	}
	return context.DeadlineExceeded
}
func (s *stubHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Served-By", s.name)
	w.WriteHeader(http.StatusNoContent)
}

type envelope struct {
	Error *struct {
		Code      string `json:"code"`
		Retryable bool   `json:"retryable"`
	} `json:"error"`
	TraceID string `json:"traceId"`
}

func do(t *testing.T, h http.Handler, path string) (*httptest.ResponseRecorder, envelope) {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.Header.Set(scope.HeaderGameID, "game_demo")
	r.Header.Set(scope.HeaderEnv, "prod")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	var env envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil && rec.Body.Len() > 0 {
		t.Fatalf("响应非 JSON: %v (body=%q)", err, rec.Body.Bytes())
	}
	return rec, env
}

func testServer(reg *providers.Registry, cfg providers.Config) http.Handler {
	return New(reg, cfg, middleware.Chain(middleware.NewRateLimiter(600, 100)))
}

func TestDegradation(t *testing.T) {
	t.Run("未配置 Provider → 501 COMMON_CAPABILITY_DISABLED", func(t *testing.T) {
		rec, env := do(t, testServer(providers.NewRegistry(), nil), "/v1/announcements/list")
		if rec.Code != http.StatusNotImplemented {
			t.Fatalf("status = %d, 期望 501", rec.Code)
		}
		if env.Error == nil || env.Error.Code != "COMMON_CAPABILITY_DISABLED" || env.Error.Retryable {
			t.Fatalf("error = %+v, 期望 COMMON_CAPABILITY_DISABLED retryable=false", env.Error)
		}
		if !traceIDRe.MatchString(env.TraceID) {
			t.Fatalf("traceId = %q, 期望 32-hex", env.TraceID)
		}
	})

	t.Run("配置了但全部未注册 → 501(静默跳过,不残废)", func(t *testing.T) {
		cfg := providers.Config{providers.CapSupport: {Primary: "nobody"}}
		rec, env := do(t, testServer(providers.NewRegistry(), cfg), "/v1/support/tickets")
		if rec.Code != http.StatusNotImplemented || env.Error.Code != "COMMON_CAPABILITY_DISABLED" {
			t.Fatalf("status=%d code=%v, 期望 501 COMMON_CAPABILITY_DISABLED", rec.Code, env.Error)
		}
	})

	t.Run("显式关闭 → 501", func(t *testing.T) {
		reg := providers.NewRegistry()
		h := &stubHandler{name: "herald", cap: providers.CapAnnouncements, healthy: true}
		_ = reg.Register(h)
		cfg := providers.Config{providers.CapAnnouncements: {Primary: "herald", Disabled: true}}
		rec, env := do(t, testServer(reg, cfg), "/v1/announcements/list")
		if rec.Code != http.StatusNotImplemented || env.Error.Code != "COMMON_CAPABILITY_DISABLED" {
			t.Fatalf("status=%d code=%v, 期望 501 COMMON_CAPABILITY_DISABLED", rec.Code, env.Error)
		}
	})

	t.Run("配置 + 注册 + 健康 → 命中 Provider", func(t *testing.T) {
		reg := providers.NewRegistry()
		_ = reg.Register(&stubHandler{name: "herald", cap: providers.CapAnnouncements, healthy: true})
		cfg := providers.Config{providers.CapAnnouncements: {Primary: "herald"}}
		rec, _ := do(t, testServer(reg, cfg), "/v1/announcements/list")
		if rec.Code != http.StatusNoContent || rec.Header().Get("X-Served-By") != "herald" {
			t.Fatalf("status=%d served-by=%q, 期望 204 herald", rec.Code, rec.Header().Get("X-Served-By"))
		}
	})

	t.Run("primary 不健康 → 降级链 fallback", func(t *testing.T) {
		reg := providers.NewRegistry()
		_ = reg.Register(&stubHandler{name: "herald", cap: providers.CapAnnouncements, healthy: false})
		_ = reg.Register(&stubHandler{name: "backup", cap: providers.CapAnnouncements, healthy: true})
		cfg := providers.Config{providers.CapAnnouncements: {Primary: "herald", Fallbacks: []string{"backup"}}}
		rec, _ := do(t, testServer(reg, cfg), "/v1/announcements/list")
		if rec.Code != http.StatusNoContent || rec.Header().Get("X-Served-By") != "backup" {
			t.Fatalf("status=%d served-by=%q, 期望降级到 backup", rec.Code, rec.Header().Get("X-Served-By"))
		}
	})

	t.Run("降级链全挂 → 503 COMMON_UNAVAILABLE", func(t *testing.T) {
		reg := providers.NewRegistry()
		_ = reg.Register(&stubHandler{name: "herald", cap: providers.CapAnnouncements, healthy: false})
		_ = reg.Register(&stubHandler{name: "backup", cap: providers.CapAnnouncements, healthy: false})
		cfg := providers.Config{providers.CapAnnouncements: {Primary: "herald", Fallbacks: []string{"backup"}}}
		rec, env := do(t, testServer(reg, cfg), "/v1/announcements/list")
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, 期望 503", rec.Code)
		}
		if env.Error == nil || env.Error.Code != "COMMON_UNAVAILABLE" || !env.Error.Retryable {
			t.Fatalf("error = %+v, 期望 COMMON_UNAVAILABLE retryable=true", env.Error)
		}
	})
}

func TestUnknownRoute(t *testing.T) {
	h := testServer(providers.NewRegistry(), nil)

	t.Run("业务名前缀不挂载 → 结构化 404", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/accounts/list", nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, 期望 404", rec.Code)
		}
		var env envelope
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
			t.Fatalf("应为 JSON 信封: %v", err)
		}
		if env.Error == nil || env.Error.Code != "COMMON_NOT_FOUND" {
			t.Fatalf("error = %+v, 期望 COMMON_NOT_FOUND", env.Error)
		}
	})

	t.Run("能力域缺 scope → 400", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/app/", nil))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, 期望 400", rec.Code)
		}
	})
}

func TestHealthz(t *testing.T) {
	h := testServer(providers.NewRegistry(), nil)

	t.Run("GET /healthz 200 ok", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
		if rec.Code != http.StatusOK || rec.Body.String() != `{"status":"ok"}` {
			t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
		}
	})

	t.Run("POST /healthz → 405", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/healthz", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, 期望 405", rec.Code)
		}
	})
}

func TestRequestIdEcho(t *testing.T) {
	h := testServer(providers.NewRegistry(), nil)
	r := httptest.NewRequest(http.MethodGet, "/v1/app/", nil)
	r.Header.Set(scope.HeaderGameID, "g")
	r.Header.Set(scope.HeaderEnv, "dev")
	r.Header.Set("X-Request-Id", "echo-me-42")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if got := rec.Header().Get("X-Request-Id"); got != "echo-me-42" {
		t.Fatalf("X-Request-Id = %q, 期望原样回传", got)
	}
}
