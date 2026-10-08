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

	"github.com/cuihairu/courier/gateway/auth"
	"github.com/cuihairu/courier/gateway/middleware"
	"github.com/cuihairu/courier/gateway/providers"
	"github.com/cuihairu/courier/gateway/providers/account"
	"github.com/cuihairu/courier/gateway/providers/chirp"
	"github.com/cuihairu/courier/gateway/providers/croupier"
	"github.com/cuihairu/courier/gateway/providers/herald"
	"github.com/cuihairu/courier/gateway/providers/warden"
	"github.com/cuihairu/courier/gateway/routing"
)

func main() {
	addr := os.Getenv("COURIER_GATEWAY_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	// Provider 注册表:各 Provider 经 reg.Register 挂入(默认提供,可换可关)。
	// M1:自建 AccountProvider;M2:herald(公告)/ croupier(客服)/ chirp(推送)。
	// 接入方可经配置换掉或关闭任何一个。
	reg := providers.NewRegistry()
	acc := account.New(account.Options{})
	// 会话校验器:身份域自建,M2 三域共享(通道/工单都为已认证玩家服务)。
	reqAuth := auth.RequireAuth(acc.Verifier())
	chirpP := chirp.New(chirp.Options{RequireAuth: reqAuth})
	heraldP := herald.New(herald.Options{RequireAuth: reqAuth, Notify: chirpP.Hub().Publish})
	croupierP := croupier.New(croupier.Options{RequireAuth: reqAuth, Notify: chirpP.Hub().Publish})
	// 实名(M2 后段,批次 7):注册 warden(自建核验),但默认路由表不含 realname
	// ——契约红线「默认关闭」;接入方在 COURIER_GATEWAY_CONFIG 显式配置
	// {"realname":{"primary":"warden"}} 开启,S2S 上报凭证经
	// COURIER_REALNAME_S2S_TOKEN(缺省 = S2S 端点 fail-closed)。
	wardenP := warden.New(warden.Options{
		RequireAuth: reqAuth,
		S2SToken:    os.Getenv("COURIER_REALNAME_S2S_TOKEN"),
	})
	for _, h := range []providers.Handler{acc, heraldP, croupierP, chirpP, wardenP} {
		if err := reg.Register(h); err != nil {
			log.Fatalf("courier gateway: 注册 %s 失败: %v", h.Name(), err)
		}
	}

	// 配置驱动路由表(primary + fallbacks[]):内置默认(identity → 自建账号,
	// announcements/support/messages → 各默认供应商)+
	// COURIER_GATEWAY_CONFIG(可选,JSON,覆盖默认);未配置的域保持降级
	// (501 COMMON_CAPABILITY_DISABLED),见 routing 包文档。
	cfg := providers.Config{
		providers.CapIdentity:      {Primary: account.DefaultName},
		providers.CapAnnouncements: {Primary: herald.DefaultName},
		providers.CapSupport:       {Primary: croupier.DefaultName},
		providers.CapMessages:      {Primary: chirp.DefaultName},
	}
	if userCfg, err := loadConfig(); err != nil {
		log.Fatalf("courier gateway: 加载路由表配置失败: %v", err)
	} else if userCfg != nil {
		for cap, cc := range userCfg {
			cfg[cap] = cc // 接入方配置覆盖内置默认
		}
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
