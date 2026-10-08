// Package middleware 网关中间件链。冻结顺序(批次 2):
//
//	trace → 结构化错误(panic 恢复) → 限流 → scope
//
// 中间件只处理横切面,不认识业务字段(docs/layers.md L5 边界)。
package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"

	"github.com/cuihairu/courier/gateway/aggregation"
	"github.com/cuihairu/courier/gateway/scope"
)

// Middleware 标准中间件签名。
type Middleware func(http.Handler) http.Handler

// compose 依次包裹:mws[0] 最外层。
func compose(mws ...Middleware) Middleware {
	return func(next http.Handler) http.Handler {
		for i := len(mws) - 1; i >= 0; i-- {
			next = mws[i](next)
		}
		return next
	}
}

// Chain 按冻结顺序组链:trace → 结构化错误 → 限流 → scope。
// limiter 为 nil 时限流一步放行(未配置 = 不限流,后续批次引入基础限流配置)。
func Chain(limiter *RateLimiter) Middleware {
	return compose(Trace(), Recovery(), RateLimit(limiter), Scope())
}

// Trace 注入 32-hex traceId(primitives.md「TraceID」:32 位 hex,兼容 W3C);
// 请求携带 X-Request-Id 时网关原样回传(不混作 traceId:它是客户端幂等/关联 ID)。
func Trace() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if id := r.Header.Get("X-Request-Id"); id != "" {
				w.Header().Set("X-Request-Id", id)
			}
			ctx := withTraceID(r.Context(), newTraceID())
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// Recovery 结构化错误出口:panic → 500 COMMON_INTERNAL 信封(errors.md 冻结码)。
// 注意:handler 已写响应头后再 panic 无法改写状态,仅尽力补写错误体。
func Recovery() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					fmt.Printf("gateway: panic recovered: %v\n", rec)
					aggregation.WriteError(w, TraceID(r.Context()), aggregation.CodeInternal, "internal error")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// Scope 契约校验(scope.md「网关校验」1):两 header 缺失或不合法 →
// COMMON_INVALID_ARGUMENT;通过则把 scope 注入上下文供下游读取。
func Scope() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s, err := scope.FromRequest(r)
			if err != nil {
				aggregation.WriteError(w, TraceID(r.Context()), aggregation.CodeInvalidArgument, err.Error())
				return
			}
			next.ServeHTTP(w, r.WithContext(scope.WithContext(r.Context(), s)))
		})
	}
}

type traceKey struct{}

// TraceID 读取上下文 traceId;未经过 Trace 中间件时现场生成(保证失败信封必带 traceId)。
func TraceID(ctx context.Context) string {
	if id, ok := ctx.Value(traceKey{}).(string); ok && id != "" {
		return id
	}
	return newTraceID()
}

func withTraceID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, traceKey{}, id)
}

// newTraceID 32 位小写 hex(crypto/rand,16 字节)。
func newTraceID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand 不可用属致命环境问题;退化为固定串保证信封形状。
		return "00000000000000000000000000000000"
	}
	return hex.EncodeToString(b[:])
}
