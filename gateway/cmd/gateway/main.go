// Courier Gateway — 玩家 API 网关。
// 职责是入口治理（auth/session/scope/routing/aggregation/middleware），不含业务目录：
// 业务一律经 providers/ 下的 Provider 接口接入，目录规划见 docs/architecture.md「Gateway:入口治理,不含业务」。
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
)

func main() {
	addr := os.Getenv("COURIER_GATEWAY_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	// 路由挂载点（按 docs/architecture.md；未配置的 Provider 对应路由不注册，客户端得到 COMMON_CAPABILITY_DISABLED）：
	//   /v1/identity/*       AccountProvider（默认：自建 accounts/sessions，M1）
	//   /v1/announcements/*  AnnouncementProvider（默认：herald，M2）
	//   /v1/support/*        SupportProvider（默认：croupier，M2）
	//   /v1/realname/*       RealNameProvider（默认：关闭，M2 后段）
	//   /v1/app/*            ConfigProvider / BrandingProvider（M3）
	//   /v1/assistant/*      AssistantProvider（M4）
	//   /v1/payments/*       PaymentProvider（M5，仅契约先行）

	log.Printf("courier gateway listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
