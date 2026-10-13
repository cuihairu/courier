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
	"github.com/cuihairu/courier/gateway/providers/accountalt"
	"github.com/cuihairu/courier/gateway/providers/archivist"
	"github.com/cuihairu/courier/gateway/providers/chirp"
	"github.com/cuihairu/courier/gateway/providers/croupier"
	"github.com/cuihairu/courier/gateway/providers/herald"
	"github.com/cuihairu/courier/gateway/providers/hooks"
	"github.com/cuihairu/courier/gateway/providers/sage"
	"github.com/cuihairu/courier/gateway/providers/scribe"
	"github.com/cuihairu/courier/gateway/providers/teller"
	"github.com/cuihairu/courier/gateway/providers/warden"
	"github.com/cuihairu/courier/gateway/providers/wardenalt"
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
	// oauth 域开关(v1.1 oauth.md「可换可关」):COURIER_OAUTH_DISABLED=1 关闭
	// (501 COMMON_CAPABILITY_DISABLED);第三方凭证校验器经 providers/oauth
	// 注册表由接入方装配(管理面注册制,未注册 = OAUTH_PROVIDER_UNKNOWN fail-closed)。
	oauthDisabled := os.Getenv("COURIER_OAUTH_DISABLED") == "1"
	acc := account.New(account.Options{DeleteGrace: account.DefaultDeleteGrace, OAuthDisabled: oauthDisabled})
	// identity 第二实现(批次 18):签名自验证令牌架构;路由表
	// {"identity":{"primary":"courier-account-alt"}} 即整体切换(一致性 e2e
	// 见 e2e/identity_conformance_test.go:两实现同一 wire 场景全绿)。
	accAlt := accountalt.New(accountalt.Options{DeleteGrace: accountalt.DefaultDeleteGrace, OAuthDisabled: oauthDisabled})
	// 会话校验器:经可重定向包装供 M2+ 域共享(通道/工单都为已认证玩家服务);
	// 身份源切换时随之重指(下方读配置后),否则换身份源后 M2 域验不了新令牌。
	sessVerifier := auth.NewSwitchable(acc.Verifier())
	reqAuth := auth.RequireAuth(sessVerifier)
	chirpP := chirp.New(chirp.Options{RequireAuth: reqAuth})
	heraldP := herald.New(herald.Options{RequireAuth: reqAuth, Notify: chirpP.Hub().Publish})
	// 客服(M2,批次 4):Notify 广播 support.ticket_replied 到 chirp.Hub。
	// 附件直传(F44,support.md v1.1)存储面由接入方注入 croupier.Options.AttachmentSigner
	// (换 S3 / OSS / 本地静态服务器 = 换配置,契约不动);未配置 = 501
	// COMMON_CAPABILITY_DISABLED(不内置假存储到生产)。
	croupierP := croupier.New(croupier.Options{RequireAuth: reqAuth, Notify: chirpP.Hub().Publish})
	// 实名(M2 后段,批次 7):注册 warden(自建核验),但默认路由表不含 realname
	// ——契约红线「默认关闭」;接入方在 COURIER_GATEWAY_CONFIG 显式配置
	// {"realname":{"primary":"warden"}} 开启,S2S 上报凭证经
	// COURIER_REALNAME_S2S_TOKEN(缺省 = S2S 端点 fail-closed)。
	// 审计留痕(F33 日志半边):COURIER_REALNAME_AUDIT=log 开启结构化审计行
	// (stdout,容器侧采集进日志系统);未开启 = 不产出。落库 sink(DB/SIEM 直写,
	// 保留策略)随部署面注入 warden.Options.AuditSink。
	var auditSink warden.AuditSink
	if os.Getenv("COURIER_REALNAME_AUDIT") == "log" {
		auditSink = warden.LogSink{}
	}
	wardenP := warden.New(warden.Options{
		RequireAuth: reqAuth,
		S2SToken:    os.Getenv("COURIER_REALNAME_S2S_TOKEN"),
		AuditSink:   auditSink,
	})
	// realname 第二实现(批次 64):warden-alt(托管核验)——判定经注入的
	// 核验服务(契约「自定义 Webhook」形态)而非本地证件号解析;网关侧只存
	// 判定态(不存指纹/出生日期,红线上更紧)。核验服务地址经
	// COURIER_REALNAME_VERIFY_WEBHOOK 注入;未注入 = Provider 注册但
	// HealthCheck 不就绪,路由 fail-closed 503(不静默半可用)。路由表
	// {"realname":{"primary":"warden-alt"}} 即切换(一致性 e2e 见
	// e2e/realname_conformance_test.go:两实现同一 wire 场景全绿)。
	var cloud wardenalt.CloudVerifier
	if ep := os.Getenv("COURIER_REALNAME_VERIFY_WEBHOOK"); ep != "" {
		cloud = wardenalt.WebhookVerifier{Endpoint: ep}
	}
	wardenAltP := wardenalt.New(wardenalt.Options{
		RequireAuth: reqAuth,
		Cloud:       cloud,
		S2SToken:    os.Getenv("COURIER_REALNAME_S2S_TOKEN"),
	})
	// 配置/应用状态(M3,批次 8):scribe(自建),config/branding/app 同管道;
	// 玩家侧只读投影,发布/版本/维护开关走管理面(Publish/SetVersion/SetMaintenance)。
	scribeP := scribe.New(scribe.Options{RequireAuth: reqAuth, Notify: chirpP.Hub().Publish})
	// 玩家档案(M3,批次 8):archivist(自建轻量)——账号档案 + 账号↔角色映射
	// (按 scope 隔离;角色数据归各游戏,本域不存)。
	archivistP := archivist.New(archivist.Options{RequireAuth: reqAuth})
	// 注销级联钩子(契约 auth.md v1.1 注册制):网关不认识下游存储,各 Provider
	// 自行订阅清理(archivist 清档案与角色绑定);新 Provider 接入 = 注册钩子。
	hooks.Register(hooks.AccountDeleted, archivistP.DeleteAccount)
	// 小助手(M4,批次 9):sage(自建 FAQ 检索)——命中即答,答不出引导转人工
	// (复用 support 域提单链路,经客户端组装;LLM 供应商按同一接口替换,默认不依赖)。
	sageP := sage.New(sage.Options{RequireAuth: reqAuth})
	// 支付(M5,批次 10):teller(自建沙箱渠道)——服务端定价 + 渠道回调 HMAC 校验
	// (签名即认证,不走 Bearer;密钥未配置 = 回调 fail-closed 403);
	// 真实渠道按同一回调签名面接入,发货经 Deliver 钩子由接入方落地。
	// v1.1:paid/delivered 状态转移经 Notify 广播(载荷只含 orderId,拉取兜底)。
	tellerP := teller.New(teller.Options{
		RequireAuth:   reqAuth,
		ChannelSecret: os.Getenv("COURIER_PAYMENTS_CHANNEL_SECRET"),
		Notify:        chirpP.Hub().Publish,
	})
	for _, h := range []providers.Handler{acc, accAlt, heraldP, croupierP, chirpP, wardenP, wardenAltP, scribeP, archivistP, sageP, tellerP} {
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
		providers.CapApp:           {Primary: scribe.DefaultName},
		providers.CapPlayer:        {Primary: archivist.DefaultName},
		providers.CapAssistant:     {Primary: sage.DefaultName},
		providers.CapPayments:      {Primary: teller.DefaultName},
	}
	if userCfg, err := loadConfig(); err != nil {
		log.Fatalf("courier gateway: 加载路由表配置失败: %v", err)
	} else if userCfg != nil {
		for cap, cc := range userCfg {
			cfg[cap] = cc // 接入方配置覆盖内置默认
		}
		// 身份源切换:验证器与主存同源,primary 换到第二实现即重指
		// (跟随 identity 域 primary;fallbacks 不参与)。
		if cc, ok := userCfg[providers.CapIdentity]; ok && cc.Primary == accountalt.DefaultName {
			sessVerifier.SetTarget(accAlt.Verifier())
		}
	}

	// 中间件链:trace → 结构化错误 → 限流 → scope(冻结顺序,批次 2);
	// 维护门包整个入口(app.md:维护开启 → /v1/* 除本域端点外一律 503,白名单内建)。
	chain := middleware.Chain(middleware.NewRateLimiter(120, 60))
	handler := middleware.Maintenance(scribeP.InMaintenance)(routing.New(reg, cfg, chain))

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
