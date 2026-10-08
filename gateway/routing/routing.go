// Package routing 路由装配:healthz + 按能力域挂载 /v1/{capability}/。
//
// 降级语义(批次 2 验收):
//
//   - 已知能力域未配置 / 显式关闭 / 无已注册 Handler → 路由已挂载,返回
//     501 COMMON_CAPABILITY_DISABLED(能力关闭,非故障;SDK 据此安全隐藏 UI);
//   - 配置了 Provider 但降级链全部健康检查失败 → 503 COMMON_UNAVAILABLE(依赖不可用);
//   - 未知前缀(业务名如 accounts)不挂载 → 结构化 404 COMMON_NOT_FOUND。
package routing

import (
	"fmt"
	"net/http"

	"github.com/cuihairu/courier/gateway/aggregation"
	"github.com/cuihairu/courier/gateway/middleware"
	"github.com/cuihairu/courier/gateway/providers"
)

// New 装配网关 handler:
//
//   - /healthz         运维端点,不进业务链(无 scope 要求),GET only;
//   - /v1/{capability} 已知能力域,经 chain(trace → 结构化错误 → 限流 → scope)后
//     按降级链分发到已注册 Provider;
//   - /v1/* 其余       结构化 404。
//
// chain 为 nil 时不做中间件(仅测试可用)。
func New(reg *providers.Registry, cfg providers.Config, chain middleware.Middleware) http.Handler {
	if reg == nil {
		reg = providers.NewRegistry()
	}
	if chain == nil {
		chain = func(next http.Handler) http.Handler { return next }
	}

	root := http.NewServeMux()
	root.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	for _, cap := range providers.AllCapabilities() {
		d := dispatcher(reg.Chain(cap, cfg[cap]), cap)
		root.Handle("/v1/"+string(cap)+"/", chain(http.HandlerFunc(d)))
	}
	// 未知 /v1 前缀:结构化 404,不进业务链(未知路由无 scope 可校验)。
	root.Handle("/v1/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		aggregation.WriteError(w, middleware.TraceID(r.Context()), aggregation.CodeNotFound,
			"no route for "+r.URL.Path)
	}))
	return root
}

// dispatcher 固定解析好的降级链;健康检查逐请求执行(批次 7 引入健康缓存与熔断)。
func dispatcher(chain []providers.Handler, cap providers.Capability) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if len(chain) == 0 {
			aggregation.WriteError(w, middleware.TraceID(r.Context()), aggregation.CodeCapabilityDisabled,
				fmt.Sprintf("capability %q not configured", cap))
			return
		}
		ctx := r.Context()
		for _, h := range chain {
			if h.HealthCheck(ctx) == nil {
				h.ServeHTTP(w, r)
				return
			}
		}
		aggregation.WriteError(w, middleware.TraceID(r.Context()), aggregation.CodeUnavailable,
			fmt.Sprintf("capability %q providers unavailable", cap))
	}
}
