// Courier Gateway — 玩家 API 网关。
//
// 入口治理(auth/session/scope/routing/aggregation/middleware),业务一律经
// providers/ 的 Provider 接口接入,不设业务目录;目录形状见 docs/architecture.md
// 「Gateway:入口治理,不含业务」。能力域与挂载点见 providers/capability.go。
package main

import (
	"log"
	"net/http"
	"os"

	"github.com/cuihairu/courier/gateway/middleware"
	"github.com/cuihairu/courier/gateway/providers"
	"github.com/cuihairu/courier/gateway/routing"
)

func main() {
	addr := os.Getenv("COURIER_GATEWAY_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	// Provider 注册表:M1 起各 Provider 经 reg.Register 挂入(默认提供,可换可关)。
	reg := providers.NewRegistry()

	// 配置驱动路由表(primary + fallbacks[]):COURIER_GATEWAY_CONFIG 指向 JSON;
	// 未配置的域保持降级(501 COMMON_CAPABILITY_DISABLED),见 routing 包文档。
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("courier gateway: 加载路由表配置失败: %v", err)
	}

	// 中间件链:trace → 结构化错误 → 限流 → scope(冻结顺序,批次 2)。
	chain := middleware.Chain(middleware.NewRateLimiter(120, 60))
	handler := routing.New(reg, cfg, chain)

	log.Printf("courier gateway listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, handler))
}

// loadConfig 读取 COURIER_GATEWAY_CONFIG(可选)指向的 JSON 路由表;未设置 = 全部能力降级。
func loadConfig() (providers.Config, error) {
	path := os.Getenv("COURIER_GATEWAY_CONFIG")
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return providers.LoadConfig(data)
}
