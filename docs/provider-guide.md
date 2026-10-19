
# 自定义 Provider 接入指南

> 面向要**替换或新增**网关能力域后端(Provider)的接入方。默认供应商可换可关;
> 客户端 SDK **零改动**(wire 由 `docs/contract/` 冻结,见 [sdk-selection.md](./sdk-selection.md)
> 九域默认接线与 501 降级后果)。治理原则见 [architecture.md](./architecture.md)「Provider 治理」:
> 接口统一 ≠ 数据迁移;接入方式两种(服务端 Provider 默认 / 客户端直连例外)。

## 1. 最小接入:四方法 + 注册 + 路由表

Provider = 一个 `providers.Handler`(`gateway/providers/registry.go`),以 `/v1/{capability}/` 为前缀服务:

```go
type Handler interface {
    Name() string                                  // 供应商标识(如 "herald"),注册表内唯一
    Capability() providers.Capability              // 能力域;同能力可注册多个(降级链候选)
    HealthCheck(ctx context.Context) error         // 单次可用性探测
    ServeHTTP(w http.ResponseWriter, r *http.Request)  // Path 保留能力域前缀
}
```

接入三步(装配先例:`gateway/cmd/gateway/main.go`):

```go
reg := providers.NewRegistry()
_ = reg.Register(myProvider) // 同名重复注册报错;同能力多注册即降级链候选
```

```json
// COURIER_GATEWAY_CONFIG(JSON 路由表,覆盖内置默认):
{"identity": {"primary": "courier-account-alt"},
 "announcements": {"primary": "herald", "fallbacks": ["my-herald"]},
 "payments": {"disabled": true}}
```

范围说明:这是**同一网关进程内**的替换(编译期 import + 配置期选路);独立进程 / HTTP
sidecar 形态契约不动,属部署面演进。

## 2. 路由与降级语义

- 路由表一行 = `CapabilityConfig{primary, fallbacks[], disabled}`
  (`gateway/providers/config.go`);`primary` 空 = 未配置,`disabled: true` = 显式关闭,
  二者同样返回 `501 COMMON_CAPABILITY_DISABLED`。
- 请求分发沿降级链逐个 `HealthCheck`(逐请求探活,无健康缓存),失败尝试下一个;熔断仅实名域已落地(`providers/router`,批次 7),通用降级链为逐请求探活。
- `fallbacks` 引用未注册的名字**静默跳过**(不残废);配置键拼错(未知能力域)在
  `LoadConfig` 直接报错,不静默失效。
- 管理语义:能力域 = 路由前缀,业务名不得成为能力域;新能力先在
  [errors.md](./contract/errors.md) 注册域前缀,再进 `providers/capability.go` 与路由表。

## 3. 能力域全集与默认供应商

| 能力域 | 前缀 | 默认 Provider | 里程碑 | 备注 |
| --- | --- | --- | --- | --- |
| identity | /v1/identity/ | `courier-account` | M1 | 第二实现 `courier-account-alt`(批次 18) |
| announcements | /v1/announcements/ | `herald` | M2 | 第二实现 `herald-alt`(写时索引,批次 69) |
| support | /v1/support/ | `croupier` | M2 | 注入点最多(见 §4);第二实现 `croupier-alt`(写时属主索引 + 键集游标,批次 73) |
| messages | /v1/messages/ | `chirp` | M2 | SSE 推送通道,其余域经其 Hub 广播;第二实现 `chirp-alt`(环形日志+游标,批次 68) |
| realname | /v1/realname/ | `warden` | M2 后段 | **默认关闭**:路由表显式配置才开启(契约红线);第二实现 `warden-alt`(托管核验,批次 64) |
| app | /v1/app/ | `scribe` | M3 | config / branding / app 同管道;第二实现 `scribe-alt`(写时维度索引,批次 71) |
| player | /v1/player/ | `archivist` | M3 | 玩家档案 + 账号↔角色映射;第二实现 `archivist-alt`(事件日志,批次 67) |
| assistant | /v1/assistant/ | `sage` | M4 | FAQ 检索;LLM 供应商按同一接口替换,默认不依赖;第二实现 `sage-alt`(写时预计算规范化,批次 70) |
| payments | /v1/payments/ | `teller` | M5 | 自建沙箱渠道;真实渠道按同一回调签名面接入;第二实现 `teller-alt`(事件账本投影,批次 72) |

## 4. Options 装配面:逐域注入点

各 Provider 的 `Options` 结构是唯一装配面;**nil / 未配置 = fail-closed,不是 panic 也不是裸奔**:

| 域 | 注入点 | 缺省行为 |
| --- | --- | --- |
| identity | `account.Options` / `accountalt.Options`;会话校验器经 `auth.NewSwitchable` 供 M2+ 域共享,身份源切换 = `SetTarget` 跟随 primary | 双实现同一 wire(见 §6) |
| identity·oauth | `OAuthDisabled: true` 关闭 oauth 域(501);第三方凭证校验器经 providers/oauth 装配,未注册 = `OAUTH_PROVIDER_UNKNOWN` | fail-closed |
| support | `croupier.Options.AttachmentSigner`(F44 附件直传)、`PenaltyStore`(F45 处罚投影源:List/Get/Appeal) | nil = 附件 / 处罚能力 501(不内置假存储假风控到生产);`croupieralt.Options` 同名字段同口径 |
| support | `Notify`(坐席回复 → chirp 广播)、`TicketRatePerMinute/Burst`(默认 5/分钟) | nil = 不推送 |
| payments | `teller.Options.ChannelSecret`(回调 HMAC-SHA256)、`Deliver` 发货钩子(按订单幂等,接入方落库)、`StateFile`(F77 一期订单快照:每次状态变更原子写全量 temp+rename,启动回放;空 = 纯内存与历史行为一致;`telleralt.Options.StateFile` 同形状,快的是事件账本) | 密钥空 = 回调一律 403 `PAYMENT_INVALID_SIGNATURE`;快照损坏 = HealthCheck fail-closed → 支付域 503(不静默从空态启动丢单),运维删除快照文件重启即恢复 |
| realname | `warden.Options.S2SToken`、`AuditSink`(F33 审计事件面:verify/curfew/charge_check/playtime_report + consent 组 v1.1 declared/parent_requested/granted/denied/revoked,事件只含账号维度元数据;接缝在共享包 `gateway/audit`,内置 `LogSink` 日志半边与 `HTTPSink` 直写半边,SIEM 摄取端 / 自建落库代理按部署面注入;两实现同形状,`wardenalt.Options.AuditSink` 同);`warden.Options.Consent` / `wardenalt.Options.Consent`(consent 组 v1.1,F31:法域阈值 minimumAge / digitalConsentAge / adultAge / requestTTL,经 main.go env 装配,阈值不进契约——换法域 = 换配置);两实现 `Notify`(consent 状态变化 → SSE `realname.consent.updated`,复用 messages 主通道);`wardenalt.Options.Cloud`(托管核验:核验服务按 `CloudVerifier` 接口注入,`WebhookVerifier` 为契约「自定义 Webhook」形态适配;地址经 `COURIER_REALNAME_VERIFY_WEBHOOK`) | S2S 缺省 = fail-closed 401;sink 缺省 = 不产出——`COURIER_REALNAME_AUDIT_WEBHOOK`(+ `COURIER_REALNAME_AUDIT_TOKEN`)开直写半边(尽力交付:超时/网络失败/非 2xx 只记日志,不阻断实名请求;保留策略归接入方存储),`COURIER_REALNAME_AUDIT=log` 开日志半边,两实现经 main.go 同源注入;warden-alt Cloud 缺省 = HealthCheck 不就绪 → 503(fail-closed,配置缺失 ≠ 能力关闭 501);Consent nil = consent 端点 501(契约「默认关」,阈值缺省数字同意 13 / 成年 18 / VPC 72h,minimumAge 未设 = 无 DENIED 档);consent S2S 判定面 `POST /v1/realname/consent/s2s/{grant,deny,revoke}`(运维通道,复用 `S2SToken`):grant 自 PENDING_PARENT/DENIED/REVOKED、deny 自 PENDING_PARENT、revoke 自 GRANTED/PENDING_PARENT,同态幂等、不适用 400、ADULT/UNDECLARED 不可判——防回拨纠错只走此面,玩家契约不可回拨 |
| 级联 | `hooks.Register(hooks.AccountDeleted, fn)`(契约 auth.md v1.1 注册制:注销数据清理由下游 Provider 自订) | 网关不认识任何下游存储 |

## 5. 凭据与配置注入

Provider 的密钥一律**环境变量注入、进程内 fail-closed**(`COURIER_PAYMENTS_CHANNEL_SECRET`、
`COURIER_REALNAME_S2S_TOKEN` 等,见 main.go);不落配置文件、不进契约。
网关侧另有装配 env:`COURIER_GATEWAY_ADDR`(监听地址,缺省 `:8080`)、
`COURIER_OAUTH_DISABLED=1`(关闭 oauth 域,501 降级)、`COURIER_GATEWAY_CONFIG`(JSON
路由表路径,覆盖内置默认;`SIGHUP` 重读生效,失败保留旧表,批次 99/F78 一期)、`COURIER_REALNAME_AUDIT_WEBHOOK`(+ `COURIER_REALNAME_AUDIT_TOKEN`,
审计直写半边端点与凭证)、`COURIER_REALNAME_AUDIT=log`(日志半边开关)、
`COURIER_REALNAME_CONSENT`(+ 可选 `_MINIMUM_AGE` / `_DIGITAL_CONSENT_AGE` /
`_ADULT_AGE` / `_REQUEST_TTL`,consent 组开关与法域阈值:任一非空即开启,全部
未设置 = consent 端点 501「默认关」;数值非法拒绝起服)、
`COURIER_PAYMENTS_STATE`(teller 订单快照文件路径,缺省空 = 纯内存)、
`COURIER_PAYMENTS_STATE_ALT`(teller-alt 事件账本快照路径,缺省空 = 纯内存)。
持久化(落库 / 对账 / 审计 / 对象存储)随部署面——trace 贯穿 gateway→Provider 已内建
([layers.md](./layers.md) L5),内存实现在单机与测试场景即为完整功能。

## 6. 替换验收:一致性测试先例

「Provider 可换」的行为证明 = **同一 wire 场景对两个实现各跑一遍、同一断言**
(`gateway/e2e/identity_conformance_test.go`,批次 18;实名域同构先例
`gateway/e2e/realname_conformance_test.go`,批次 64——warden 与 warden-alt
各跑成年/未成年同一链路):

- 装配与 main.go 同构(registry + 路由表 + 冻结中间件链:trace → 结构化错误 → 限流 → scope);
- 场景覆盖含跨域校验(换身份源后 herald 照常认证新令牌);
- 新 Provider 接入建议给出同构一致性测试;e2e 全绿 + 网关 `go vet` + `go test`
  全绿为本仓提交门禁(CI 同命令,见 tests.yml;本地防缓存可加 `-count=1`)。
