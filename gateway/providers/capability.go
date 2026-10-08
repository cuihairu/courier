package providers

// Capability 能力域 = 路由前缀 /v1/{capability}/ 与配置路由表的键。
//
// 全集与 cmd/gateway 挂载点一致;新能力先在 docs/contract/errors.md 注册域前缀,
// 再进入此处与路由表。业务名(accounts、announcements 的后端实体等)不得成为能力域。
type Capability string

const (
	CapIdentity      Capability = "identity"      // /v1/identity/*      账号/会话  M1
	CapAnnouncements Capability = "announcements" // /v1/announcements/* 公告       M2
	CapSupport       Capability = "support"       // /v1/support/*       客服       M2
	CapMessages      Capability = "messages"      // /v1/messages/*      推送通道   M2(无专属错误码前缀,复用通用码)
	CapRealname      Capability = "realname"      // /v1/realname/*      实名       M2 后段
	CapApp           Capability = "app"           // /v1/app/*           配置/品牌/版本 M3
	CapPlayer        Capability = "player"        // /v1/player/*        玩家档案   M3
	CapAssistant     Capability = "assistant"     // /v1/assistant/*     小助手     M4
	CapPayments      Capability = "payments"      // /v1/payments/*      支付       M5
)

// AllCapabilities 挂载全集:网关为每个已知能力域装配路由(未配置者返回降级信号)。
func AllCapabilities() []Capability {
	return []Capability{
		CapIdentity,
		CapAnnouncements,
		CapSupport,
		CapMessages,
		CapRealname,
		CapApp,
		CapPlayer,
		CapAssistant,
		CapPayments,
	}
}
