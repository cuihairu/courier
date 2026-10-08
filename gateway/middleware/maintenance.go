package middleware

import (
	"net/http"
	"strings"

	"github.com/cuihairu/courier/gateway/aggregation"
)

// Maintenance 维护模式拦截(app.md 契约:维护开启 → 新会话的认证/业务请求
// 一律 503 APP_MAINTENANCE)。
//
// 白名单契约固定,不做成配置:
//   - /v1/app/*:维护时客户端仍需能查询 /v1/app/maintenance 得知恢复,否则死锁;
//   - 非 /v1 路径(/healthz):运维探活在维护期必须照常。
//
// 只拦新请求;已建立的连接(SSE 流)不经本门,自然不踢(契约:已建立的会话
// 不强制踢出)。enabled 为 nil = 永不拦截(未装配)。
func Maintenance(enabled func() bool) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if enabled != nil && enabled() &&
				strings.HasPrefix(r.URL.Path, "/v1/") && !strings.HasPrefix(r.URL.Path, "/v1/app/") {
				aggregation.WriteError(w, TraceID(r.Context()),
					aggregation.CodeMaintenance, "server under maintenance")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
