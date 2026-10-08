package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type stubVerifier struct {
	id  Identity
	err error
}

func (s stubVerifier) Verify(context.Context, string) (Identity, error) { return s.id, s.err }

func run(t *testing.T, v Verifier, header string) (int, string, bool) {
	t.Helper()
	h := RequireAuth(v)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := FromContext(r.Context())
		if !ok || id.AccountID != "acc_1" {
			t.Errorf("handler 未收到注入身份: %+v %v", id, ok)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	r := httptest.NewRequest(http.MethodGet, "/v1/x/", nil)
	if header != "" {
		r.Header.Set("Authorization", header)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	var env struct {
		Error *struct {
			Code      string `json:"code"`
			Retryable bool   `json:"retryable"`
		} `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	code := ""
	retryable := false
	if env.Error != nil {
		code = env.Error.Code
		retryable = env.Error.Retryable
	}
	return rec.Code, code, retryable
}

func TestRequireAuth(t *testing.T) {
	ok := stubVerifier{id: Identity{AccountID: "acc_1", SessionID: "ses_1"}}

	t.Run("有效凭证通过", func(t *testing.T) {
		status, code, _ := run(t, ok, "Bearer tok-1")
		if status != http.StatusNoContent || code != "" {
			t.Fatalf("status=%d code=%q", status, code)
		}
	})

	cases := map[string]struct {
		header   string
		verifier Verifier
		wantCode string
		wantStat int
	}{
		"无 header":  {"", ok, "COMMON_UNAUTHENTICATED", http.StatusUnauthorized},
		"非 Bearer":  {"Basic abc", ok, "COMMON_UNAUTHENTICATED", http.StatusUnauthorized},
		"空 Bearer":  {"Bearer ", ok, "COMMON_UNAUTHENTICATED", http.StatusUnauthorized},
		"无效凭证":      {"Bearer x", stubVerifier{err: ErrInvalidCredentials}, "AUTH_INVALID_CREDENTIALS", http.StatusUnauthorized},
		"过期":        {"Bearer x", stubVerifier{err: ErrTokenExpired}, "AUTH_TOKEN_EXPIRED", http.StatusUnauthorized},
		"吊销":        {"Bearer x", stubVerifier{err: ErrTokenRevoked}, "AUTH_TOKEN_REVOKED", http.StatusUnauthorized},
		"scope 不符":  {"Bearer x", stubVerifier{err: ErrScopeMismatch}, "SCOPE_MISMATCH", http.StatusBadRequest},
		"账号禁用":      {"Bearer x", stubVerifier{err: ErrAccountDisabled}, "AUTH_ACCOUNT_DISABLED", http.StatusForbidden},
		"未知错误保守未认证": {"Bearer x", stubVerifier{err: errors.New("mystery")}, "COMMON_UNAUTHENTICATED", http.StatusUnauthorized},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			status, code, retryable := run(t, c.verifier, c.header)
			if status != c.wantStat || code != c.wantCode || retryable {
				t.Fatalf("status=%d code=%s retryable=%v, 期望 %d %s false", status, code, retryable, c.wantStat, c.wantCode)
			}
		})
	}
}

func TestBearerToken(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if _, ok := BearerToken(r); ok {
		t.Fatal("无 header 应返回 false")
	}
	r.Header.Set("Authorization", "bearer tok") // 大小写不敏感
	if tok, ok := BearerToken(r); !ok || tok != "tok" {
		t.Fatalf("tok=%q ok=%v", tok, ok)
	}
	r.Header.Set("Authorization", "Bearer  ") // 纯空白
	if _, ok := BearerToken(r); ok {
		t.Fatal("空白 token 应返回 false")
	}
}
