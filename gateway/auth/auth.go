// Package auth 网关认证:凭证 → 身份注入(M1,契约 docs/contract/auth.md)。
//
// 职责:校验 access token,向请求上下文注入身份;边界:注册/登录业务经
// AccountProvider 接入(providers/account),会话验证逻辑见 session 包。
package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/cuihairu/courier/gateway/aggregation"
	"github.com/cuihairu/courier/gateway/middleware"
)

// Identity 已认证身份(会话维度,见 auth.md「数据模型:sessions」)。
type Identity struct {
	AccountID string
	SessionID string
	DeviceID  string // 可空:未绑定设备的会话
	GameID    string
	Env       string
}

// 认证失败哨兵(错误码冻结于 docs/contract/errors.md;RequireAuth 按此映射信封)。
var (
	ErrUnauthenticated    = errors.New("auth: missing or malformed credentials")
	ErrInvalidCredentials = errors.New("auth: invalid credentials")
	ErrTokenExpired       = errors.New("auth: token expired")
	ErrTokenRevoked       = errors.New("auth: token revoked")
	ErrScopeMismatch      = errors.New("auth: token scope mismatch")
	ErrAccountDisabled    = errors.New("auth: account disabled")
)

// Verifier access token 验证抽象;由会话实现方(session 包 + 存储方)提供。
// 实现读取 ctx 中的 scope(scope 中间件在链上先于 auth)做绑定比对。
type Verifier interface {
	Verify(ctx context.Context, accessToken string) (Identity, error)
}

// BearerToken 提取 Authorization: Bearer <token>;缺失/格式错返回 false。
func BearerToken(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(h) <= len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return "", false
	}
	token := strings.TrimSpace(h[len(prefix):])
	if token == "" {
		return "", false
	}
	return token, true
}

// RequireAuth 认证中间件:Bearer token → Verifier → 身份注入 ctx。
// 失败按冻结码写结构化错误:无/格式错 → COMMON_UNAUTHENTICATED;
// 过期 → AUTH_TOKEN_EXPIRED;吊销 → AUTH_TOKEN_REVOKED;无效 →
// AUTH_INVALID_CREDENTIALS;scope 不符 → SCOPE_MISMATCH;账号禁用 → AUTH_ACCOUNT_DISABLED。
func RequireAuth(v Verifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := BearerToken(r)
			if !ok {
				aggregation.WriteError(w, middleware.TraceID(r.Context()), aggregation.CodeUnauthenticated, ErrUnauthenticated.Error())
				return
			}
			id, err := v.Verify(r.Context(), token)
			if err != nil {
				aggregation.WriteError(w, middleware.TraceID(r.Context()), codeFor(err), err.Error())
				return
			}
			next.ServeHTTP(w, r.WithContext(WithIdentity(r.Context(), id)))
		})
	}
}

func codeFor(err error) string {
	switch {
	case errors.Is(err, ErrTokenExpired):
		return aggregation.CodeTokenExpired
	case errors.Is(err, ErrTokenRevoked):
		return aggregation.CodeTokenRevoked
	case errors.Is(err, ErrScopeMismatch):
		return aggregation.CodeScopeMismatch
	case errors.Is(err, ErrAccountDisabled):
		return aggregation.CodeAccountDisabled
	case errors.Is(err, ErrInvalidCredentials):
		return aggregation.CodeInvalidCredentials
	default:
		return aggregation.CodeUnauthenticated
	}
}

type ctxKey struct{}

// WithIdentity 把身份注入请求上下文。
func WithIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// FromContext 读取身份;未认证返回 false。
func FromContext(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(ctxKey{}).(Identity)
	return id, ok
}
