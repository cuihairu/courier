// Courier Gateway — 玩家 API 网关。
// M1 起点：仅提供健康检查与模块挂载点；accounts/sessions 见 docs/roadmap.md。
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

	// 模块挂载点（按 docs/architecture.md 边界实现）：
	//   /v1/auth/*           accounts + sessions（M1）
	//   /v1/announcements/*  herald/croupier 玩家侧投影（M2）
	//   /v1/support/*        croupier 工单/FAQ（M2）
	//   /v1/assistant/*      小助手编排（M3）
	//   /v1/payments/*       充值（M4，渠道抽象先行）

	log.Printf("courier gateway listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
