# Courier TODO — 分批落地

> 原则:**不堆业务,先立骨架;契约先行**。每批交付「接口形状 + 最小实现 + 验收」;批次映射五层([layers.md](./layers.md)),里程碑映射 [roadmap](./roadmap.md)。

## 批次 0 — 更名与定位(已完成,2026-10)

- [x] Concierge → Courier 全量更名(标识符、包名、环境变量、文档、站点、二进制)
- [x] 定位重写:Universal Game SDK + 信使隐喻 + 站点同步
- [x] 五层架构设计文档([docs/layers.md](./layers.md))与本分批 todo(本文档)

## 批次 1 — M0 Contract 立宪(已完成,2026-10)

- [x] 基元契约初稿:[primitives](./contract/primitives.md) / [errors](./contract/errors.md) / [scope](./contract/scope.md) / [versioning](./contract/versioning.md) / [events](./contract/events.md)
- [x] 域契约初稿:[realname](./contract/realname.md) / [branding](./contract/branding.md) / [diagnostics](./contract/diagnostics.md)(红线与 Provider 口径内建)
- [x] 跨端评审(≥ Unity + 一个非 C# 端)→ 冻结升 v1(四端视角过读,6 项修订;记录见 [contract/index.md](./contract/index.md))
- [x] 契约测试骨架(各端错误枚举/DTO 对齐断言)([fixtures](./contract/fixtures/) + `tools/contractgen` 生成六端契约源,漂移即测试红)
- **验收**:依契约手写 DTO 无歧义(roadmap M0)——unity(dotnet 8 用例)/ cocos / miniprogram / laybox(node:test 6×3)/ ue(g++)全绿;godot 为引擎侧卡点;gateway 服务端 codeTable↔fixture 同步测试并入
- **落地**:commit `d5dbebf`(评审+冻结 v1)/ `a60d8ed`(测试骨架);realname / branding / diagnostics 仍 Draft 初稿,实现前冻结

## 批次 2 — Gateway 瘦身骨架(M1 前置工程,已完成,2026-10)

- [x] 目录重构:auth / session / scope / routing / aggregation / middleware / providers(业务目录不存在,业务一律 Provider)
- [x] Provider 注册表接口形状冻结:`Register(registry)` + 配置驱动路由表(`primary`+`fallbacks[]`)
- [x] 能力降级语义:未配置 Provider → 路由不注册 / `COMMON_CAPABILITY_DISABLED`
- [x] 保留 `/healthz`;中间件链:trace → 结构化错误 → 限流 → scope
- **验收**:go test(含 -race)覆盖注册/降级/中间件行为;`accounts` 等业务名不出现在 gateway 一级目录
- **落地**:commit `cdd391d`;auth/session 为 M1 占位,热重载与熔断留批次 7

## 批次 3 — M1:AccountProvider(自建)+ auth 契约(已完成,2026-10)

- [x] `docs/contract/auth.md` 冻结(Frozen v1)
- [x] accounts / sessions:邮箱+密码、游客设备、access/refresh 轮换、吊销、设备绑定
- [x] 数据表:accounts、account_credentials、sessions、devices;基础限流
- **验收**:roadmap M1 服务端项——go test(含 -race)覆盖结构化错误全码路径与登录限流(429 + Retry-After);e2e 全链路(游客→绑定→登出→轮换→重放吊销→拒绝)
- **落地**:commit `3c24cfc`(契约)/ `39b5007`(实装);持久化与多实例限流协同随 M1 后续

## 批次 4 — M1:Unity Core(L2)(已完成,2026-10)

- [x] `CourierClient` 门面 + Identity/Session 域
- [x] 生命周期状态机(Uninitialized→…→PlayerReady、Suspended/Resuming)+ 生命周期事件
- [x] ITransport / ITokenStore / IClock 注入接口;重试与超时
- [x] 纯逻辑单测(不依赖 Unity 运行时)
- **验收**:状态机全事件单测;与 auth 契约字段 100% 对齐——xunit 45 用例:转移表逐行断言+8 非法转移、契约事件仅 6 个、guest 请求体逐字段/会话 DTO 全字段 wire 对齐、bind Bearer、失败信封 typed 枚举回 Ready、TOKEN_EXPIRED 自动 refresh 轮换重放、REUSED 吊销清场+signed_out、Init 恢复三态(有效被动/expired refresh 清场/expired access 轮换)
- **落地**:commit `8b9ed8a`;Newtonsoft(DateParseHandling.None 保 RFC3339 原文)入 Runtime,平台实现留批次 5

## 批次 5 — M1:Unity Adapter + 三包骨架(L3)(已完成,2026-10)

- [x] UPM 包骨架:Core / Service / UI 三包目录(UI 包可空,结构就位)
- [x] 安全存储实现 ITokenStore(非明文 PlayerPrefs);UnityWebRequest 实现 ITransport
- [x] 前后台/进程信号 → 状态机事件
- **验收**:M1 全链路(游客→绑定→登出→refresh 轮换→吊销拒绝)Core 层 45+ 用例与 gateway e2e 双覆盖;切后台恢复状态正确由状态机转移表用例保证(Resuming 回挂起前态)。UPM 骨架、asmdef 隔离、L2 零平台引用、Adapter 符号、测试工程 include 防漂移由 `tools/packcheck` 结构验收守
- **落地**:commit `516c1a0`;L2 迁入 com.courier.core 包,asmdef 隔离纯内核与 Adapter;TryFire 平台幂等;硬件级安全存储(Keychain/Keystore)与真机链路留引擎侧卡点

## 批次 6 — M2:公告/客服/推送 Provider + UI 包(已完成,2026-10)

- [x] AnnouncementProvider / SupportProvider / MessageProvider(herald、croupier、chirp 为默认供应商,均可配置替换或关闭)
- [x] 推送通道:chirp 会话复用;关闭 = 仅拉取兜底
- [x] 可选 UI 包首发(公告栏/客服页),消费 Branding(默认标)
- **验收**:roadmap M2 前两项——网关 e2e 四例:运营发布→SSE 5s 内推送+拉取历史一致;messages 关闭→501 COMMON_CAPABILITY_DISABLED 且拉取兜底可用;提单→坐席回复→SSE 实时可见+详情 REPLIED;匿名 stream 401。契约层:错误码 v2(26 码)三端同步(dotnet 8 / TS 6×3 / UE 26 枚举)。Unity 侧:service 包 wire 断言 17 例(路由/请求体/typed 404·409·429 不重试)+ SSE 解析 9 例;packcheck 守 service 纯净/ui 引擎件/asmdef 依赖链
- **落地**:契约 commit `8ec5702`;网关 commit `38fe759`;客户端 commit `a4ce1eb`;SSE 流式传输与 TokenStore 真机验证留引擎侧卡点;herald/croupier 与 chirp 经 Notify 钩子装配解耦(不互相 import)

## 批次 7 — M2 后段:实名(合规)(已完成,2026-10)

- [x] RealNameProvider 热插拔:路由表热重载、降级链、熔断(首个实现:自建核验;云厂商按需)
- [x] 默认关闭;开启即数据范围明示;脱敏口径落 UI
- **验收**:roadmap M2 实名验收项——契约 realname.md Frozen v1(F27–F30 端点表 + 错误码 v3,调研「可参考分析」落点兑现);治理引擎 `providers/router`(降级链保序、熔断开路/半开探测、路由表原子热重载在途按旧表完成,业务结果不计失败,9 例 -race);warden 自建核验(SHA-256 指纹不存明文、幂等、F28 窗口、F29 分龄限额、S2S 上报幂等月累计,14 例);e2e 6 例含「主熔断→fallback 接管→全挂 PENDING_REVIEW」经 HTTP 与热重载不中断;默认关闭经 main 注册但不配路由表(接入方显式开启);Unity 侧 RealNameService(501 转 null 不报错)+ RealNameMask 统一脱敏 + 五态面板(ServiceTests 36 例)
- **落地**:契约 `e835ff2`;网关 `e926626`;客户端 `a3dc311`;云厂商适配(阿里云/慧眼/易盾/Webhook)按接入方需要接同一 `router.RealNameProvider` 形状;进程级配置热重载触发(SIGHUP 等)留部署面

## 批次 8 — M3:Config / Player / App / Branding / Diagnostics(已完成,2026-10)

- [x] ConfigProvider(条件匹配 + 热生效);Player Profile;Courier.App
- [x] BrandingProvider(config 同管道);UI 包消费品牌;构建期物料指引
- [x] Diagnostics 可选包(四类 Provider 适配;默认全关,no-op 零开销)
- **验收**:roadmap M3 验收项——契约 config/app/branding/player/diagnostics 五件 Frozen v1(player 零新增错误码,复用通用码;diagnostics 红线内建:默认全关/开启明示/关闭即零上报/永不反噬)。网关 scribe(config/app/branding 同管道,条件投影缺省维度跳过 + fnv64 灰度分桶 + 版本全局单调、branding 键值透传 + version 保留键、维护门白名单内建)+ archivist(档案懒建/修剪校验、角色绑定幂等 + scope 隔离 + ≤50/账号/游戏、fail-closed);e2e 五例:配置热生效(命中端 items 变化/未命中端不变)、维护拦新会话不拦既有流与 app 域、版本/环境端点、品牌热切换。Unity:AppService/AppConfigService/BrandingService/PlayerService(501 → 空结果或 null 不进报错路径)+ UI BrandingCatalog 兜底链(远端 productName > 宿主 BrandTitle > 内置默认)双面板标题热切换;diagnostics 可选包 `com.courier.diagnostics`(四类接口 + 自有端点直发,DiagnosticsTests 11 例:默认零上报/逐类开启逐类生效/关闭立即归零/≤1KB 与原始类型校验/失败静默不反噬),核心三包不依赖它(packcheck 双向守卫)
- **落地**:契约 `569fc8b`(config+app)/ `60adb4c`(branding+事件登记)/ `a91c179`(player)/ `164e9e4`(diagnostics);网关 `33c5c18`(scribe+维护门+config e2e)/ `a3be6f7`(branding 端点+e2e)/ `72bf15c`(archivist);客户端 `c7fdf63`(App/Config)/ `cbaa42d`(Branding+UI)/ `43118be`(Player)/ `43ed745`(diagnostics 包);Sentry/OTLP 生态适配与构建期物料模板随各端模板工程落档

## 批次 9 — M4:Assistant;批次 10 — M5:Payment

- [x] FAQ 检索问答 + 转人工(M4)(已完成,2026-10)
- [x] Payment Contract 冻结(订单/回调 + 四形态)+ 沙箱渠道全链路(M5)(已完成,2026-10)

### 批次 9 落地(M4,2026-10)

- [x] assistant.md Frozen v1:检索式问答(命中返回知识条目**原样快照**,答不生成——可审计不幻觉);未命中不是错误(200 + suggestTransfer);零新增错误码;LLM 接口预留但默认不依赖(接入方换供应商,契约语义不变)
- [x] sage 默认供应商:知识库管理面登记(AddEntry,自带库不反向依赖 support 域)、检索评分、QueryStats 命中率统计(进程内);e2e 两例:命中/未命中/400 校验 + 转人工链路端到端(未命中 → support 提单 → 坐席回复 REPLIED → 玩家详情可见)
- [x] Unity AssistantService(未命中即结果对象、501→null 隐藏入口)+ AssistantPanel(AskAsync 渲染 + TransferToHumanAsync 一键提单,标题 `[助手]` 前缀);packcheck 守三面板
- **验收**:roadmap M4——常见问题命中率可统计(QueryStats + 单测/e2e 断言);转人工链路端到端(e2e)
- **落地**:契约 `1104081`;网关 `2481383`(sage+e2e)/ `786b14b`(stats);客户端 `9b45f9e`(service)/ `1047bb1`(UI 面板);持久化知识库与跨实例统计聚合、LLM 供应商接入随部署面

### 批次 10 落地(M5,2026-10)

- [x] payment.md Frozen v1:订单-回调模型(下单只收 skuId 服务端定价;渠道回调签名即认证——X-Payment-Signature = hex HMAC-SHA256(secret, rawBody),验证先于 body 解析,伪造/错钥/未配密钥一律 403 fail-closed);四形态(DIRECT_PURCHASE/ACCOUNT_WALLET/GAME_WALLET/EXTERNAL_PAYMENT)只是订单标注不产生不同状态机;状态机 CREATED→PAID→DELIVERED / CREATED|PAID→CLOSED;发货失败订单停 PAID 不丢单,管理面对账重发(断单可对账恢复);回调幂等不二次发货;归属 404 不泄露存在性;PAYMENT 域 5 码入 errors.md v4(全端 contractgen 同步)
- [x] teller 默认供应商:价格表管理面(AddSku 改价不影响已建订单)、下单(风控前置 RiskCheck→403)、S2S 回调(恒时比较、幂等受理、paidAt 透传)、Reconcile 对账(停 PAID 枚举重发)、CloseOrder(DELIVERED 不可关→409 PAYMENT_ORDER_STATE);e2e 四例:全链(价格表投影/服务端定价防伪造金额字段/发货指令含定价事实/详情列表无 payToken)、签名面(缺头/错钥/篡改全部 403 订单停 CREATED)、断单对账恢复、风控拒绝+未知订单回调 404
- [x] Unity PaymentService(SKU 投影/下单/列表/详情轮询,501→null 隐藏商城 UI;payToken 只在下单响应出现,消费归渠道 SDK,契约不解释;金额原样渲染不做客户端计算)+ ServiceTests 6 例;packcheck 守 service 包新件
- **验收**:roadmap M5——服务端定价(客户端无法改价)、回调签名面 fail-closed(单测+e2e 三伪造形态)、断单对账恢复(e2e)、回调幂等不二次发货(单测+e2e)
- **落地**:契约 `9df7ba7`(payment v1 + errors v4);网关 `ddcbd03`(teller+e2e);客户端 `fff7125`(service);真实渠道接入、订单持久化与定时对账、支付推送事件(v1.1 预留)随部署面

## 明确不做

- 实时多人/匹配/大厅(Nakama / Agones 地盘);渠道包聚合联运(MSDK / QuickSDK 地盘);运营端界面(默认供应商 Croupier,可自建);自建长连接(默认 Provider chirp,可换);统一钱包假设。
