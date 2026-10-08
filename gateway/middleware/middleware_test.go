package middleware

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/cuihairu/courier/gateway/scope"
)

var traceIDRe = regexp.MustCompile(`^[0-9a-f]{32}$`)

// getJSON 发请求并解析响应信封字段。
type resp struct {
	status    int
	body      []byte
	header    http.Header
	traceID   string
	errCode   string
	retryable bool
}

func do(t *testing.T, h http.Handler, r *http.Request) resp {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	out := resp{status: rec.Code, header: rec.Header().Clone()}
	out.body = rec.Body.Bytes()
	var env struct {
		Error *struct {
			Code      string `json:"code"`
			Retryable bool   `json:"retryable"`
		} `json:"error"`
		TraceID string `json:"traceId"`
	}
	if err := json.Unmarshal(out.body, &env); err == nil && env.Error != nil {
		out.errCode = env.Error.Code
		out.retryable = env.Error.Retryable
		out.traceID = env.TraceID
	}
	return out
}

func scopeReq(path string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.Header.Set(scope.HeaderGameID, "game_demo")
	r.Header.Set(scope.HeaderEnv, "prod")
	return r
}

func TestChainOrder(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := scope.FromContext(r.Context()); !ok {
			t.Error("scope 应已注入上下文")
		}
		w.WriteHeader(http.StatusNoContent)
	})

	t.Run("trace 最外层:panic 响应仍回传 X-Request-Id", func(t *testing.T) {
		panicning := Chain(nil)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			panic("boom")
		}))
		r := httptest.NewRequest(http.MethodGet, "/v1/app/", nil)
		r.Header.Set("X-Request-Id", "client-req-1")
		r.Header.Set(scope.HeaderGameID, "g")
		r.Header.Set(scope.HeaderEnv, "prod")
		got := do(t, panicning, r)

		if got.header.Get("X-Request-Id") != "client-req-1" {
			t.Fatalf("X-Request-Id = %q, 期望原样回传 client-req-1", got.header.Get("X-Request-Id"))
		}
		if got.status != http.StatusInternalServerError || got.errCode != "COMMON_INTERNAL" || !got.retryable {
			t.Fatalf("panic → status=%d code=%s retryable=%v, 期望 500 COMMON_INTERNAL retryable=true",
				got.status, got.errCode, got.retryable)
		}
		if !traceIDRe.MatchString(got.traceID) {
			t.Fatalf("traceId = %q, 期望 32-hex", got.traceID)
		}
	})

	t.Run("限流先于 scope:桶空 + 缺 scope → 429", func(t *testing.T) {
		// 独立 limiter(burst=2),避免与其它子测试共享令牌桶。
		limited := Chain(NewRateLimiter(60, 2))(ok)
		for i := 0; i < 2; i++ { // 消耗完 burst=2
			got := do(t, limited, scopeReq("/v1/app/"))
			if got.status != http.StatusNoContent {
				t.Fatalf("第 %d 次请求 status = %d, 期望 204", i+1, got.status)
			}
		}
		bare := httptest.NewRequest(http.MethodGet, "/v1/app/", nil) // 无 scope
		got := do(t, limited, bare)
		if got.status != http.StatusTooManyRequests || got.errCode != "RATE_LIMITED" || !got.retryable {
			t.Fatalf("status=%d code=%s retryable=%v, 期望 429 RATE_LIMITED retryable=true",
				got.status, got.errCode, got.retryable)
		}
		if got.header.Get("Retry-After") == "" {
			t.Fatal("429 应带 Retry-After")
		}
	})

	t.Run("scope 在限流内层:有桶但缺 scope → 400", func(t *testing.T) {
		fresh := Chain(NewRateLimiter(60, 10))(
			http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				t.Error("scope 校验失败不应到达 handler")
			}))
		got := do(t, fresh, httptest.NewRequest(http.MethodGet, "/v1/app/", nil))
		if got.status != http.StatusBadRequest || got.errCode != "COMMON_INVALID_ARGUMENT" || got.retryable {
			t.Fatalf("status=%d code=%s retryable=%v, 期望 400 COMMON_INVALID_ARGUMENT retryable=false",
				got.status, got.errCode, got.retryable)
		}
	})

	t.Run("全链通过", func(t *testing.T) {
		fresh := Chain(NewRateLimiter(60, 10))(ok)
		got := do(t, fresh, scopeReq("/v1/app/"))
		if got.status != http.StatusNoContent {
			t.Fatalf("status = %d, 期望 204(body=%s)", got.status, got.body)
		}
	})
}

func TestScopeValidation(t *testing.T) {
	h := Chain(nil)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("非法 scope 不应到达 handler")
	}))

	cases := map[string]func(*http.Request){
		"缺 env":     func(r *http.Request) { r.Header.Set(scope.HeaderGameID, "g") },
		"缺 game_id": func(r *http.Request) { r.Header.Set(scope.HeaderEnv, "prod") },
		"env 不在注册表": func(r *http.Request) {
			r.Header.Set(scope.HeaderGameID, "g")
			r.Header.Set(scope.HeaderEnv, "qa")
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/v1/app/", nil)
			setup(r)
			got := do(t, h, r)
			if got.status != http.StatusBadRequest || got.errCode != "COMMON_INVALID_ARGUMENT" {
				t.Fatalf("status=%d code=%s, 期望 400 COMMON_INVALID_ARGUMENT", got.status, got.errCode)
			}
		})
	}
}

func TestTraceIDFromContext(t *testing.T) {
	h := Trace()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := TraceID(r.Context())
		if !traceIDRe.MatchString(id) {
			t.Errorf("TraceID = %q, 期望 32-hex", id)
		}
		if got := TraceID(r.Context()); got != id {
			t.Errorf("同一请求 TraceID 应稳定: %q != %q", got, id)
		}
		_, _ = io.WriteString(w, "ok")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
}

func TestTraceEchoesRequestID(t *testing.T) {
	h := Trace()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Request-Id", "abc-123")
	h.ServeHTTP(rec, r)
	if got := rec.Header().Get("X-Request-Id"); got != "abc-123" {
		t.Fatalf("X-Request-Id = %q, 期望原样回传", got)
	}

	// 未携带时不伪造回传
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/", nil))
	if got := rec2.Header().Get("X-Request-Id"); got != "" {
		t.Fatalf("未携带 X-Request-Id 时不应设置, got %q", got)
	}
}
