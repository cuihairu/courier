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
- **落地**:契约 `9df7ba7`(payment v1 + errors v4);网关 `ddcbd03`(teller+e2e);客户端 `fff7125`(service);真实渠道接入、订单持久化与定时对账随部署面

## 批次 11 — M5 后段:支付推送事件(v1.1)

- [x] payment.md v1.1:推送事件 `payment.paid` / `payment.delivered`(广播提示,载荷只含 orderId——金额/SKU 归拉取端点按归属过滤;幂等重复回调不重发,只报状态转移;发货感知升级「推送为提示、轮询为准」);events.md / messages.md 事件登记同步
- [x] teller Notify 钩子(接 chirp.Hub,同 herald/croupier 装配;CREATED→PAID 发 paid、转 DELIVERED 发 delivered 含对账恢复重发);e2e:SSE 断言 paid/delivered 5s 内送达、断单只发 paid、Reconcile 补发 delivered、幂等回调与伪造签名静默不推送
- **落地**:契约 `60b5d22`;网关 `79c5bce`;客户端注释同步随 docs 提交;订单持久化、定时对账与离线推送(APNs/FCM)随部署面

## 批次 12 — cocos(TS)core + service 层

- [x] core(`315717c`):FetchTransport(超时/网络错误归一 UNAVAILABLE)、ApiClient(scope 头恒发、匿名本地预检不发包、信封解析、空/非 JSON 体按状态码兜底、Retry-After 优先退避、AUTH_TOKEN_EXPIRED 刷新重放一次不计重试)、SessionService(单飞刷新、REUSED/REVOKED 清库、尽力而为登出)、IdentityService(guest/register/login 匿名 + bind Bearer 不采纳会话)
- [x] service(`09be3a9` + `db69d8e`,unity CourierServices 同构,501→null 隐藏入口):announcements/support(SSE 帧解析器:注释/多行 data/未知字段容忍)、app(三端点匿名;503 APP_MAINTENANCE / 426 typed 抛,SDK 不自动跳转)、config(缓存 + needsRefetch version 判据;501→v0 空结果用内建默认值)、branding(匿名透传零解释)、player(PATCH undefined 键不下发)、assistant(未命中非错误 matched=false+suggestTransfer)、payments(下单只收 skuId 服务端定价;payToken 仅下单响应;发货感知=轮询 DELIVERED)、realname(姓名/证件号只传输一次不缓存不落盘;curfew/charge-check 前置校验)
- [x] 测试:零依赖 `node --test` 41 例全绿(契约 6 / core 18 / service 17),对齐 unity CoreTests 形态(FakeTransport 脚本化)
- **落地**:core `315717c`、announcements/support `09be3a9`、八域 service `db69d8e`;平台适配器(微信小游戏 storage/原生存储)、生命周期状态机、UI 包随部署面

## 批次 13 — 生命周期状态机 + 微信小游戏适配器

- [x] 生命周期状态机(`ce974f7`,L2 core,unity Lifecycle.cs 同构):九态十触发严格迁移表(architecture.md 状态图全表;Resuming→ResumeCompleted 回挂起前稳定态)、fire 非法抛 / tryFire 幂等否决(平台信号处理器不许崩)、契约事件面:lifecycle.initialized/suspended/resumed/signed_out + 会话域发 token_expired(自动 refresh 开始)/account_switched(切号检测,首登与同号重登不发);CourierClient 接线:init()、guestLoginAsync/loginAsync/registerAsync 认证流(守卫 Ready/SignedOut,失败 AuthFailed 回退 Ready 可重试)、session adopt→切号、refresh→token_expired、REUSED/REVOKED 与登出→signed_out(tryFire,状态机未推进时静默)
- [x] 微信小游戏适配器(`e6cf491`,`src/platform/wechat/`,wx 最小结构类型零外部依赖):TokenStore(wx storage,JSON 落盘,损坏数据按未认证清场)、Transport(wx.request,请求头透传/响应头小写归一/对象响应重序列化/失败归一 TransportError 走 core 既有重试口径——重连策略归 core)、WechatLifecycleMonitor(onShow/onHide/onNetworkStatusChange → tryFire 幂等挂起恢复,unity ApplicationLifecycleMonitor 同构;Ready 态挂起被状态机否决不粘死)
- [x] 测试:零依赖 `node --test` 83 例全绿(契约 6 / core+lifecycle 53 / service 17 / wechat 7;fake wx 结构类型注入)+ 全链组合例(wechat 适配器 × CourierClient:登录落盘、切后台挂起、冷启动恢复)
- **落地**:core `ce974f7`、适配器 `e6cf491`;UE/Godot 适配器、真机联调随部署面

## 批次 14 — UE(C++)core + service 层

- [x] json 基础(`13aab75`):手写 RFC 8259 解析器(深度限制/代理对/前导零拒绝)+ dump,零依赖;`json_test.cpp` 覆盖转义/Unicode/边界
- [x] core(`225ad01` + `1377e60`,unity Core 同构,纯 ISO C++17 header-only 零外部依赖):Transport/TokenStore 注入接口、ApiClient(scope 头恒发、匿名本地预检不发包、信封解析、空/非 JSON 体按状态码兜底、Retry-After 优先退避、AUTH_TOKEN_EXPIRED 刷新重放一次不计重试)、SessionService(重入保护刷新、REUSED/REVOKED 清库、尽力而为登出)、IdentityService(guest/register/login 匿名 + bind Bearer 采纳会话)、LifecycleMachine(九态十触发严格迁移表 + 契约事件面)、CourierClient 门面(认证流守卫 Ready/SignedOut、失败 AuthFailed 回退可重试);`url_encode` 入共享 types
- [x] service M2(`f5b160e`,unity CourierServices 同构):SSE 帧解析器(注释/多行 data/未知字段容忍/reset 断线重连)、announcements(list/get wire 形状 + typed 404 不重试)、support(提单 category 缺省不下发、详情 ticket+messages、追加 CLOSED 409 不重试、FAQ 关键词 URL 编码)
- [x] service M3/M4/M5(`3cc859a`):app(三端点匿名;503 APP_MAINTENANCE typed 返,501→null 跳过检查)、config(缓存 + needs_refetch version 判据;501→v0 空结果用内建默认值)、branding(匿名透传零解释 + version 判据)、player(PATCH 空串键不下发;501→null)、assistant(未命中非错误 matched=false+suggestTransfer;501→null)、payments(下单只收 skuId 服务端定价;payToken 仅下单响应;发货感知=轮询 DELIVERED;归属 404 typed 不重试)、realname(姓名/证件号只传输一次不缓存不落盘;curfew/charge-check 前置校验;501→null)
- [x] 测试:`sdks/ue/run_tests.sh` 全绿(contract/json/core/lifecycle/service/service2 六套,g++ -std=c++17 -Wall -Wextra 独立编译,零外部依赖)
- **落地**:json `13aab75`、core `225ad01` + `1377e60`、service M2 `f5b160e`、service M3/M4/M5 `3cc859a`;UE 引擎集成面(HttpModule 适配、UObject 包装、.uplugin)无编译链未验证——如实报,不假装

## 批次 15 — miniprogram(TS)core + service + wx 适配

- [x] core + service(`33325e8`,cocos 批次 12+13 全量对齐):transport/apiClient/session/identity/lifecycle/courierClient 门面 + 九域 service(CourierServices 门面)+ SSE 解析器;平台差异收敛在注入接口
- [x] 微信小程序适配器(`src/platform/wechat/`):wx storage TokenStore(损坏数据清场)、wx.request Transport(响应头小写归一/对象响应重序列化)、LifecycleMonitor(**小程序信号面 `wx.onAppShow/onAppHide`** + onNetworkStatusChange,与小游戏端 onShow/onHide 的真实 API 差异在此分叉)
- [x] 测试:`node --test` 83 例全绿(契约 6 / core+lifecycle 53 / service 17 / wechat 7,fake wx 结构类型注入)
- **落地**:单增量 `33325e8`;真机联调(真 wx 环境)随部署面

## 批次 16 — laybox(TS)core + service + laya 适配

- [x] core + service(`59a8e02`,cocos/miniprogram 全量对齐):同构九域 + lifecycle + 门面
- [x] Layabox 适配器(`src/platform/laya/`):LocalStorage TokenStore、XMLHttpRequest Transport(响应头小写归一)、LifecycleMonitor(文档可见性 + navigator onLine → `LayaboxAppHooks` 结构面,引擎版本差异收敛在接入方绑定侧)
- [x] 测试:`node --test` 82 例全绿(契约 6 / core+lifecycle 53 / service 17 / laya 6,fake XHR/storage 注入)
- **落地**:单增量 `59a8e02`;真机联调随部署面

## 批次 17 — cocos UI 可选包

- [x] ui 可选包(`1fb4790`,unity com.courier.ui 同构):brandingCatalog(模块级当前品牌 + 热切换/同 version 忽略/reset;字段缺失回落内置默认 `Courier`/`#4C8DFF`/`联系客服`)+ 三面板:
- 公告栏 `AnnouncementPanel`:refreshAsync 列表渲染 / openAsync 详情渲染(详情=全文视图,不带标题头——unity 双标题瑕疵有意未复制)+ 品牌兜底链标题 + typed 错误出 `onError(wireOf(e))` 出口 + 未装配服务静默不抛
- 客服 `CustomerServicePanel`:openTicketAsync(提单即开详情,create+get 两跳)/ sendMessageAsync(追加后重拉)/ notifyTicketReplied(只认当前工单)/ searchFaqAsync(空结果兜底「无匹配问题」);CLOSED 409 出错误口
- 助手 `AssistantPanel`:askAsync(命中原样渲染 / 未命中 suggestTransfer / 501→COMMON_CAPABILITY_DISABLED 隐藏入口)+ transferToHumanAsync(`[助手]` 前缀标题截 120,经 support 域提单)
- [x] 测试:`node --test` 91 例全绿(+ui 8:品牌兜底链/公告三例/客服两例/助手两例,FakeTransport + fake services 注入,与 service.test.ts 同形态)
- **落地**:单增量 `1fb4790`;UI 框架绑定(Cocos Creator UIRichText/Label 挂载渲染回调)随部署面

## 批次 18 — 可换性证明(AccountProvider 第二实现 e2e)+ 契约 v1.1(取消/并发)

- [x] `auth.SwitchableVerifier`(`d0936eb`):可重定向会话验证器——原 main.go 把 reqAuth 钉死在自建账号 store 上,换 identity Provider 后 M2+ 域验不了新令牌(「可换」名存实亡的架构缺口);现装配期读 COURIER_GATEWAY_CONFIG,identity primary 换到第二实现即重指(跟随 primary,fallbacks 不参与;nil 忽略不残废)
- [x] `providers/accountalt`(`d0936eb`,identity 第二实现,courier-account-alt):与自建同一冻结契约(auth.md Frozen v1)但内部架构刻意不同——自建「不透明随机令牌 + 会话表全查」vs 本实现「HMAC-SHA256 签名自验证令牌,claim 内嵌会话事实 + 服务端只留吊销态/refresh 代际/access nonce」;轮换即换 access nonce(旧 access 立即失效,语义同自建 map 键删除);nonce 保证同秒轮换也产出新令牌; PBKDF2 同族凭证;防枚举/限流/设备上限/解绑吊销逐项对齐
- [x] 一致性 e2e(`e2e/identity_conformance_test.go`):**同一 wire 场景对两实现各跑一遍、同一断言**——注册/登录/会话信息、防枚举、校验 400、EMAIL_TAKEN 409、游客幂等、bind 链、refresh 轮换+旧 access 失效+重放吊销、refresh 未知/过期、access 过期、logout、设备上限、解绑、设备列表、scope 不符、路由兜底、跨域认证(身份源 token 经 SwitchableVerifier 供 herald)+ 限流口径(429+Retry-After)——全绿 = 「账号 Provider 可换」有行为证明而非仅接口冻结
- [x] 契约 primitives.md **v1.1**(兼容追加「取消与超时」「并发纪律」两节):取消是客户端行为服务端不感知、取消统一表现为传输失败(COMMON_UNAVAILABLE retryable)非域错误;重试对写类同样生效(双发由服务端幂等面承接,事实以拉取为准)、会话刷新单飞+重放至多一次不计预算、REUSED/REVOKED 清场、多会话合法、并发无顺序保证——全部为各端 SDK 既有实现口径(冻结不是发明)
- [x] 测试:gateway 全套 17 包绿(accountalt 单测:codec 编解码/篡改拒绝/签名密钥校验/代际重放;switchable 重定向;-race 干净)
- **落地**:`d0936eb`(第二实现+e2e);契约 v1.1 随 docs 提交;接入方启用第二实现 = `{"identity":{"primary":"courier-account-alt"}}` 一行配置

## 批次 19 — CI 测试门禁

- [x] `.github/workflows/tests.yml`(`d44e23a`):push(main)/PR 全仓门禁——Go(contractgen 根模块 + gateway 模块)/ Node 24 原生 TS(cocos/miniprogram/laybox 矩阵,零依赖 node --test)/ UE(ISO C++17 g++ headless);unity 诚实排除(需引擎本体,本地 Unity Test Runner 跑,注释写明);并发组防叠跑;首跑(run 37975643056)全绿
- **落地**:单增量 `d44e23a`;此前只有 deploy-docs.yml,测试只有本地跑——门禁补齐后每增量三栈自动回归

## 批次 20 — cocos 诊断可选包

- [x] `src/diagnostics/`(unity com.courier.diagnostics 同构,契约 diagnostics.md Frozen v1):diagnosticsTypes(四类 enabled+endpoint,默认全关;resource 宿主注入缺省不下发)/ httpReporters(自有端点直发单条一请求,失败静默不重试不缓存;crash breadcrumb 环形 20 随报清空;trace 去 query + resource 固定属性集 service.name=courier.sdk;performance 三注册指标维度仅 platform+appVersion;analytics 名称 1-64、props 原始类型运行时校验、单条 ≤1KB 违例抛使用方错不静默截断)/ diagnosticsHub(Disabled 共享零实例;reporter 懒建;setEnabled 关立即生效并清上下文,开需端点已配)
- [x] 测试:11 例与 unity DiagnosticsTests 一一对应(默认零上报/Disabled 共享/没端点即关/逐类生效+wire 形状/breadcrumb 环+清/trace 去 query+固定集/指标注册表+两维度/校验违例零发送/失败静默不反噬/运行时关+清/运行时开需端点);`node --test` 102 例全绿(+11)
- **落地**:单增量(本批);Sentry/OTLP 适配与直发端点联调随部署面

## 批次 21 — 维护态自定位(告警/漂移/脆弱点)

- [x] 依赖安全告警清零(`7bd102a`):Dependabot 五条告警(vite ×3 / esbuild / katex)——vitepress 1.x 钉 vite ^5.4.14 够不到 6.4.3 修复版致其自更新失败;npm overrides 钉 vite 6.4.4 / esbuild 0.25.12 / katex 0.18.10,告警全部自动关闭;docs 本地构建验证
- [x] 站点导航补缺(`3dca0c4`):config/app/player/assistant/payment 五个 Frozen 契约页未入侧边栏(站点浏览不可达)
- [x] 路线图状态补记(`92c64aa`):M1–M5 按 M0 惯例补「已完成」标记;M4 过时的「后移」清除(助手批次 9 已全量交付);M5 注明真实渠道沙箱随部署面
- [x] e2e 脆弱点(`9d24f7d`):m2 推送时序 50ms sleep 删除(chirp 在 WriteHeader 前同步注册订阅者,openStream 返回即订阅建立,sleep 防的是不可能竞态);realname rnStub 按值拷贝锁(go vet copylocks;go test 默认 vet 子集不含故 CI 漏网)改指针语义
- [x] CI vet 门禁(`160b047`):go job 增根模块 + gateway 两步 go vet(堵 copylocks 类盲区)
- [x] Provider 文档漂移(`621c5dc`):`oddsmaker`(代码零存在)全仓清除,architecture/layers/README 双语/contract 总纲统一对齐真实注册名——courier-account(+courier-account-alt 第二实现)、warden(实名,原「默认关闭」口径过时)、scribe/archivist/sage/teller 入表;风控行删除(无此能力域,支付沙箱期前置风控随部署面);诊断行改为「直发不经网关(契约红线)」
- **落地**:六增量全绿推送;维护态残留项 = 部署面(需真实凭据/环境)+ Godot(已拍板停靠),环境内告警/漂移/脆弱点已清零

## 批次 22 — 巡检续批(只读审计节奏:workflow 结论 × 文档声称)

- [x] README 双语文档缺口(`d979b08`):补「开发」本地门禁章节(四端此前仅三端有);文档索引的契约清单从 M0 口径补到全域(M1 auth / M2 三域 / M3 五域 / M4 / M5);诊断注明直发不经网关
- [x] 工作流一致性(`d979b08`):deploy-docs node 22 → 24,与 tests.yml 统一
- [x] Godot 停靠口径(`fe7328f`、`1dd70ad`):README 布局行与架构文档 binding 枚举标注「规划中;仅契约生成骨架」;删除「网关做风控前置」边界声称(风控非网关职责;PAYMENT_RISK_REJECTED 为冻结契约的可选钩子,「缺省不拦」口径保留)
- [x] 测试数声称刷新(`d9e61cf`):layers.md 现仓映射行 cocos 41 例 → 102(带分布)、Unity CoreTests 48 → 29(本地数 xunit 属性核实;ServiceTests 60 / DiagnosticsTests 11 / ContractTests 8)
- [x] Unity README 重写(`67edbce`):「计划结构」→ 已交付四包结构(补 com.courier.diagnostics)+ 测试章节(Unity Test Runner 口径)+ 当前约定
- [x] cocos README 补「运行测试」章节(`2ee4c8c`,对齐其余三端)
- [x] 审计证据:全量本地门禁在当前树重跑——cocos 102 / miniprogram 83 / laybox 82 / UE 六套 / gateway 17 包(+vet +race)/ 根模块 2 包;workflow 最新 run 全绿;Dependabot 告警 0
- **落地**:六增量全绿推送;环境内可推进项仍为零,残留 = 部署面 + Godot(停靠)

## 批次 23 — 巡检续批(上轮被打断后重派,同节奏)

- [x] 复核基线(`e0ac63c` 前置):本地=origin、批次 22 全部 run 绿、无并行改动
- [x] 文档漂移两处(`e0ac63c`):layers.md L1 行错误码 v3→v4 + 域清单补 payment v1.1、L3 现仓映射补 TS 三端 adapter(platform/wechat、platform/laya)与批次 12-17/20 历史、L4 补 accountalt 第二 identity 实现(批次 18);README 双语能力表补「推送(messages 通道)」行——M2 三域此前只列公告/客服,SSE 单连接/关闭=拉取兜底口径按 messages.md 对齐
- [x] 核对项全部当前:十域契约 Frozen 头、契约索引文件表 17 文件全覆盖、fixtures 引用(layers L1/todo)、供应商八家+account/accountalt 与 gateway/providers 实目录一致、三端 TS README 结构树、UE 约定(引擎集成面未验证口径诚实)、cocos 树 ui/diagnostics 行、tools/ = contractgen+packcheck、zh/en 表行对等、Oddsmaker 两处生态概念引用为批次 18 清除口径的刻意保留(非漂移)
- [x] 判定不扩范围:三 TS SDK 无 package.json(历史从未有、无文档声称、node --test 直跑不受影响)——非漂移,维护态不补
- [x] CI:e0ac63c Tests + Deploy Docs 全绿
- **落地**:单增量全绿推送;残留 = 部署面(真实渠道/持久化凭据/真机/引擎环境)+ Godot(停靠)

## 批次 24 — 巡检续批(通读 architecture/roadmap 全文)

- [x] 契约间自相矛盾修复(`9823261`):architecture.md 总体形态图 + 关键决策 #3、layers.md 分层图 + L3 翻译行,四处声称推送走 WebSocket——与冻结契约 messages.md 的 **SSE** 选型矛盾(M0 冻结时暂定 WebSocket,M2 冻结 messages.md 改 SSE 未回改);全部对齐 SSE 口径,WebSocket 标「留后续扩展,需写时升版本」
- [x] events.md(冻结契约)加注更正(2026-10-10):传输选型以 messages.md 为准——**冻结行原文未动**(只加不改纪律),加注行承载更正
- [x] architecture.md 状态行 Draft → Current(M0 已立宪冻结、M1–M5 全落地,「Draft」声称过期)
- [x] 通读核对无漂移:roadmap.md 全文(M5 的 RiskProvider 前置为部署面计划项 + 契约预留钩子名,口径一致)、competitive.md 快照、tests.yml 三 job 结构与本地门禁一致、gateway 目录 vs L5 行、fixtures/errors.json 33 码 = errors.md 表 33 行(v4 含 5 PAYMENT + 5 REALNAME)、README 双语协议声称(SSE 行为批次 23 所加,无 WebSocket 残留)
- [x] CI:9823261 Deploy Docs 绿,Tests 排队(runner 积压,同形 docs-only 已连绿六次)
- **落地**:单增量全绿推送;残留 = 部署面 + Godot(停靠)

## 批次 25 — 巡检续批(通读 layers/research 全文,上批指定目标)

- [x] layers.md 九处同步(`0eb1503`):状态行 Draft→Current;L2/L3「下一步」从 M1 口径刷新到当前(UE 引擎集成面待引擎环境、Godot 停靠、真机随部署面);L3 现仓映射从「UPM 三包+其余五端规划」刷到已交付态(UPM 四包 + TS 三端 adapter + UE C++ header-only);L4 映射补 accountalt 批次 18;L5 映射补批次 9/10 M4/M5 e2e;L3 两处 Godot 枚举标规划中
- [x] **审计声称修正**(`0eb1503`,同一 commit):layers.md L5 职责与接口形状、architecture.md 目录树与安全边界,四处把「审计」列为已实现中间件——代码实况为 trace → 结构化错误 → 限流 → scope(冻结顺序,chain.go 注释),认证按能力路由内挂载,无 audit 中间件;全部改为「审计依赖 trace 贯穿,落库随部署面」;中间件链声称按代码实况改写
- [x] research/features.md(`055bd29`):第 12 节「这六条」→「这八条」(F69–F76 实为八项,与第 202 行「八条全是核心」矛盾);统计口径 47/13/9/7 = 76 逐行解析核验一致
- [x] research/competitive.md(`8785dc9`):「落到本仓」列两处现时声称刷新——UPM 三包→四包、「支付收据验真留 M5 契约落点」→已随 M5 冻结;调研快照正文(2026-10-08 口径)不动
- [x] CI:三增量 Tests + Deploy Docs 全绿(runner 积压已恢复,~35s)
- **落地**:三增量全绿推送;残留 = 部署面 + Godot(停靠)

## 批次 26 — 声称二次抽查(契约端点表 vs 路由、DTO 字段 vs 生成物)

- [x] 契约端点表 vs gateway 路由:33 个契约端点全实装(herald detail、croupier faq、archivist 四路径逐一核对);e2e 对差发现 `/v1/announcements/{id}` 与 `/v1/support/faq` 零覆盖(herald/croupier 无单测文件,e2e 也未打)→ **补 e2e 覆盖**(`ad5bac4`:详情命中 + 404 不泄露存在性 + FAQ 关键词命中,gateway 17 包 + -race 绿);`/v1/player/*` 已由 archivist 单测覆盖,非缺口
- [x] DTO 字段 vs 生成物:contractgen `TestGeneratedFilesInSync` 机器强制六端生成物与 fixtures 逐字节一致(本地复跑绿);fixtures ↔ 契约文档核对——errors.json 33 码 = errors.md、scope.json 头名/环境 = scope.md、dto.json 信封键/规则 = primitives.md;域 DTO 采样 payment(12 字段)与 realname 与 UE 生成物逐一吻合
- [x] Provider 单测盘点:11 个中 8 个有单测,chirp/croupier/herald 无(薄 HTTP 处理器,e2e 已覆盖 stream/tickets/faq/list/detail);剩余缺口属风格而非风险,维护态不扩
- [x] CI:ad5bac4 Tests 绿(Deploy Docs 因 docs 路径过滤未触发,符合预期)
- **落地**:单增量全绿推送;残留 = 部署面 + Godot(停靠)

## 批次 27 — 声称二次抽查(事件表 vs SDK 发射点、域端点表 vs SDK 方法)

- [x] 生命周期事件表 vs SDK 发射点:**零漂移**——events.md 六个 `lifecycle.*` 事件与 cocos/miniprogram/laybox/unity/ue 五端核心实现精确一致(逐事件名核对,非子串)
- [x] 域端点表 vs SDK 客户端方法:**零漂移**——33 端点中 31 个客户端面端点在三端样本(cocos/UE/unity)全有对应方法(announcements list/get、support 五方法、realname 四方法、player 四方法、payment 四方法、app 四方法、assistant、identity 八方法);2 个非客户端端点按契约有意不封装(payments/callback 服务端、realname/playtime-report S2S,SDK 源码有明文注释)
- [x] 附核:业务事件(announcement.published 等)SDK 按契约容忍规则泛型透传(sseParser「未知 event type 原样吐出」),非缺口;样本事件名在五端 SseParser 测试中均有覆盖
- [x] 本批零修复:环境内声称面已无漂移项
- **落地**:仅归档增量;残留 = 部署面 + Godot(停靠)

## 批次 28 — 巡检续批(README 双语对等性 + 站点导航完整性)

- [x] ① README 中英逐段对等:**零漂移**——11 节骨架一一对应、结构元素计数完全一致(表行 14 / 代码栅栏 16 / 清单项 17)、关键声称 token(供应商九家/SSE/测试数)分布一致;计数微差为英文子串噪音("Mes**sage**Provider"),非内容缺失
- [x] ② docs 站导航完整性:一处缺口修复(`6aff436`)——`contract/fixtures/index.md` 不在任何导航(其余 26 个 md 文件全被 nav+sidebar 覆盖);补 sidebar「Fixtures(机器可读快照)」项,构建通过、页面产出
- [x] nav 6 项 + sidebar 25 项目标全部存在,无死链(vitepress 构建含死链检查,绿)
- [x] CI:6aff436 Tests + Deploy Docs 绿
- **落地**:单增量全绿推送;残留 = 部署面 + Godot(停靠)

## 批次 29 — 代码级冷点复核(错误码 v4 ↔ 六端枚举、e2e 覆盖矩阵)

- [x] 错误码表 v4 ↔ 六端生成枚举:**零漂移**——errors.md 33 码逐端集合对差,cocos/miniprogram/laybox(errors.ts)、unity(ErrorCode.cs)、ue(errors.hpp)、godot(errors.gd)六端 **missing=0**(「多出」项均为域前缀常量/头保护/辅助 map,非漂移)
- [x] e2e 覆盖矩阵维护:**33/33 契约端点全部有测试覆盖**——31 端点在 e2e(批次 26 补齐公告详情 + FAQ 后;含 payments/callback 与 playtime-report 这两个服务端面端点),player profile/characters 四方法路径由 archivist 单测覆盖(21 处断言);对差工具字符串匹配需注意拼接/查询串形态(公告详情经 `+a.ID` 拼接、FAQ 带查询串),人工核实无缺
- [x] 附修(`aa436ec`):gateway `envelope_sync_test.go` 注释「错误码表 v1」→「v4」——与自身 `f.Version != 4` 断言矛盾;同步测试本身为双向对齐 + 版本闸,机器强制在位(fixtures/index.md「网关 codeTable 同步测试」声称属实)
- [x] CI:aa436ec Tests 绿(gateway 17 包 + vet 本地先行)
- **落地**:单增量全绿推送;残留 = 部署面 + Godot(停靠)

## 批次 30 — 巡检续批(DTO/错误枚举六端跨端一致性对照 + 冷点扫描)

- [x] 错误枚举前置复核仍零漂移:errors.md v4 33 码 × 六端(errors.ts ×3 / ErrorCode.cs / errors.hpp / errors.gd)逐码集合对差 + http/retryable 映射 + 13 域前缀,全对齐
- [x] DTO 跨端对照(十域 announcement/support/player/payment/realname/assistant/config/branding/app/auth × 五端 DTO vs 契约 vs 网关 wire 权威 `gateway/providers/account/account.go`),**六处不一致全修**(`469e809`,37 文件):
  - AnnouncementDto `startAt`/`endAt` TS 三端误为必填 → 可选(契约 `timestamp?`,缺省 = 发布即可见/不过期;UE/Unity 原本就对)
  - SessionInfoDto 缺 `account` → TS 三端 + UE 补(契约 auth.md GET /session;Unity 原有)
  - DeviceDto 缺 `id`/`platform` → TS 三端 + UE 补(契约 GET /devices item;Unity 原有)
  - SessionDto `accountId` 幻影字段移除(TS 三端 + UE):网关 sessionDTO 从不下发该键,TS/UE 切号检测原读幻影字段 → **检测失效 bug**;改为取 `account.id`(Unity 原本就对)
  - DeviceListDto 缺 `nextCursor` → Unity 补(与其余分页 DTO 及 TS/UE 对齐)
  - AppVersionDto `updateUrl` TS/UE/Unity 三端误为必填 → 可选(契约 `string?` + 网关 `omitempty`,共同漂移)
- [x] 测试 mock 同步网关真实 wire:TS 六套 + UE 四套会话 JSON 删 accountId / 补 account 对象,断言改走 `account.id`;全矩阵绿:cocos 102 / miniprogram 83 / laybox 82 / UE 六套 / Unity dotnet 48+69+11+8 / gateway 全包 / contractgen + packcheck
- [x] 附修:gateway `e2e/realname_test.go` 注释「响应即含 accountId」→「account.id 即账号 ID」(同批语义修正)
- [x] 冷点扫描:工作树无非忽略未跟踪文件;忽略面完整(vitepress dist / node_modules / unity 四工程 bin+obj);最大被跟踪文件为批次 2 误入库的过时 gateway 二进制 9.9MB → `git rm --cached` + `.gitignore` 补 `gateway/gateway`(`b250622`)
- [x] 文档计数漂移附修:layers.md / unity README 「CoreTests 29 例」→ 48、「ServiceTests 60 例」→ 69(实测 dotnet 口径);「本地不可验」说法废除(四 `Tests~/` 纯逻辑 dotnet 工程本地可验,README 补运行方式);tests.yml 跳过理由注释同步
- [x] 观察项(记录不扩范围,冻结契约面留契约通道):auth.md GET /session 示例含 `accessExpiresAt` 但网关不下发且 SDK 未建模;support.md ticket 响应未列 `category` 但三端 SDK 均有(请求字段回显,跨端一致);assistant.md transferTicket 预留字段各端均未建模(一致);Unity README 中文版无对应英文对等节(批次 28 双语结论维持)
- **落地**:三增量全绿推送(`469e809` DTO 对齐、`b250622` 二进制卫生、本 docs 收尾);残留 = 部署面 + Godot(停靠)

## 批次 31 — 巡检续批(三端 TS 守同工具化 + CI 工具面补全)

- [x] 缺口定位:三端 TS(cocos/laybox/miniprogram)共享面(src/{contract,core,service} 与同名 tests)是同构复制,此前**无任何守同机制**——批次 30 的 sed 漏同步 laybox/miniprogram 正是此失效模式;且 CI 只跑 `tools/contractgen/...`,**packcheck 从未进 CI**(本地绿但无门禁)
- [x] 新增 `tools/tsync`(packcheck 同构:Go 测试即结构检查,repoRoot 相对定位):共享目录文件集合三端一致 + 内容逐字节一致;同名 tests 出现于 ≥2 端时逐字节一致;平台/可选包专属 tests(laya/wechat/ui/diagnostics)豁免——生命周期信号随端实现不同(小游戏 `wx.onShow` vs 小程序 `wx.onAppShow`),测试随端走
- [x] 首跑即抓到一个真实差异:`tests/wechat.test.ts` cocos≠miniprogram——核实为**合理差异**(两端 monitor 调用的 wx API 本就不同),按豁免规则收编,未误改代码
- [x] CI:`go test ./tools/contractgen/...` → `go test ./tools/...`(contractgen+packcheck+tsync 同门禁),步骤改名 root module tools,头部覆盖说明同步
- [x] 三端 README 各补一条约定:共享面由 tools/tsync 守同,改一处须三端同步(cp)
- [x] 验证:tools 三件套 + `go vet ./...` 绿;三端套件 102/83/82 全绿(wechat.test.ts 还原后 miniprogram 83/83)
- **落地**:双增量推送(工具+CI、docs 收尾);残留 = 部署面 + Godot(停靠)

## 批次 32 — Godot 引擎落位(M1 core:生命周期 + Identity/Session)

- [x] 卡点解除:Godot 4.3-stable 可 headless 运行;装机首跑即命中真 bug——contractgen 生成的 `.gd` 头部用 `//`(Go/C 风格),GDScript 判为语法错误(`Unexpected "/" in class body`),`tests/test_contract.gd` 根本跑不起来。修:新增 `#` 风格 `gdBanner` 供 genGdErrors/genGdEnvelope 使用,重新生成(与临时 sed 结果一致),补回归断言(`.gd` 输出不得含行首 `//`)
- [x] 补 `project.godot` + `--import` 生成 `.godot/` 类缓存:`class_name` 全局解析依赖该缓存,缺失时 `--script` 报 `Identifier not declared`(contract 测试当时靠 `preload` 绕过,src 交叉引用无法绕);`.godot/` 已 gitignore
- [x] M1 core 落位(`sdks/godot/src/core`,零平台引用,与 cocos/ue/unity core 同构):`lifecycle`(9 态/10 触发 + 契约事件面 + 挂起回跳 + 切号检测)、`transport`/`tokenStore`(抽象 + Memory 实现)、`courierError`(wire 优先、未知码容忍)、`apiClient`(scope 头、信封解析、retryable 退避、`AUTH_TOKEN_EXPIRED` refresh 后重放)、`identity`/`session`(单飞 refresh、安全事件清场、登出尽力而为)、`courierClient`(两段装配 + auth_flow 状态守卫)
- [x] 测试(与 cocos `core.test.ts`/`lifecycle.test.ts` 同构,SceneTree 脚本 headless 跑):`test_lifecycle.gd`(迁移全表 13 行 + 非法迁移拒绝 + 事件面 + tryFire)、`test_core.gd`(20 例:wire 形状、Bearer、未认证预检、typed 信封、兜底 wire、退避重试与耗尽、网络失败映射、TOKEN_EXPIRED 重放、refresh 失败返原 401、安全事件清场、单飞、登出、bind 不落会话、501 降级、配置校验、未知码容忍、会话 DTO 全字段、门面状态流与失败回退)
- [x] GDScript 与 TS 的语义差实测入档(均写成代码注释,非口头约定):无异常→结果字典;`var x := await` 是解析错误(须显式类型);协程调用必须 `await`(Callable.call 也不行,`call_deferred` 可点火);`while true`+`continue` 的返回路径不判穷尽(须兜底 return);`--script` 下 `Engine.get_main_loop()` 为 null;`Dictionary ==` 逐元素按类型严格比较(JSON 数字是 float,与 int 字面量不等);`%` 右侧传 Array 会被当参数列表;`match` 兜底分支缩进不得差一级
- [x] 验证:三套 Godot 测试全绿(退出码 0)+ `go test ./tools/...` + `go vet ./...` 绿
- **落地**:双增量推送(core+tests、docs 收尾);残留 = 部署面(凭据)+ UE 引擎集成面 + Godot 平台 Transport/TokenStore(M2 起随引擎集成面)

## 批次 33 — Godot 服务域落地(M2–M5 契约面:SSE 解析器 + 九域客户端)

- [x] 服务层落位(`sdks/godot/src/service`,与 cocos `src/service` / ue `service` 同构):`sseParser`(增量帧解析:心跳注释忽略、多行 data `\n` 连接、未知字段容忍、reset 复用)+ 九域客户端(announcement/support/app/appConfig/branding/player/assistant/payment/realname)+ `CourierServices` 门面(从 client.api 装配)
- [x] 降级语义跨端对齐(TS `null` / UE `nullopt` 同构):501 `COMMON_CAPABILITY_DISABLED` → `{ok:true, data:null}`(config 域按契约返 v0 空 items 不进报错路径);其余错误照常 `{ok:false, error}` typed 透传(APP_MAINTENANCE 503 / PAYMENT_ORDER_NOT_FOUND 404 等不进降级);GDScript `String.uri_encode()` 与 `encodeURIComponent` 同形(空格 `%20`)
- [x] 测试(与 cocos `service.test.ts`/`service2.test.ts` 同构,SceneTree headless):`test_service.gd`(8 例:SSE 三态 + 公告 wire/typed 404 + 客服提单/详情/关单 409/FAQ 编码)、`test_service2.gd`(9 例:app 匿名+501+维护 typed、config 缓存与 needs_refetch 与失败沿用缓存、branding version 判据、player PATCH 缺省键、assistant 未命中非错误、payment 只发 skuId 与 payToken 只在下单、realname 四端点与 501)
- [x] GDScript 语义差再入档(写成代码注释):**`Callable` 弱引用目标**——client 被回收则 `token_provider` 失效(`null::access_token`),用例须经成员持活 client(接入方持 CourierClient 同构);getter-only 属性(`var x: Variant: get:`)可用;JSON 浮点收窄用 `int()` 逐字段断言;**构造器缺失 = new() 只收 0 参**(漏写 `_init` 时报 "Too many arguments",不在定义处报错)
- [x] 验证:五套 Godot 测试全绿(contract/lifecycle/core/service/service2,退出码 0)+ `go test ./tools/...` + `go vet` 绿
- **落地**:双增量推送(service+tests、docs 收尾);残留 = 部署面(凭据)+ UE 引擎集成面 + Godot 平台 Transport/TokenStore(messages 流式读随平台适配面)

## 批次 34 — Godot 平台适配面(真传输 + 持久令牌存储)

- [x] `CourierHTTPTransport`(`src/platform/httpTransport.gd`,extends core `CourierTransport`):HTTPClient poll 增量模型(无 Node 依赖,headless 可跑);url 拆解(scheme/缺省端口)、TLS 走 `TLSOptions.client()`、帧让出经注入 `frame_f`(`--script` 下 `Engine.get_main_loop()` 为 null,用例注入;运行时缺省取主循环)、15s 超时两段(connect/响应各一 deadline)、方法名映射、响应头键小写归一
- [x] **HTTPClient poll 语义两坑实测入档(代码注释)**:①响应完整性须以 Content-Length 计数,不可看终态——对端发完即断时 EOF 可与末字节同一 poll 到达,状态直接跳 `DISCONNECTED`,`CONNECTED` 未必可观察;②响应头仅在收体期间可读(收完后被引擎清空),须在循环内捕获快照
- [x] `CourierFileTokenStore`(`src/platform/fileTokenStore.gd`,extends core `CourierTokenStore`):`user://` JSON 整会话落盘(FileAccess/DirAccess,引擎按平台映射应用专属目录),`clear` 即删文件
- [x] 测试(`tests/test_platform.gd`,本仓首个真传输面,SceneTree headless):`MockServer` 内部类(TCPServer 真监听 + 裸 HTTP/1.1 收发,记录解析后请求,入队响应自动包 200);4 例 = token store user:// 回路跨实例、GET 真往返(自定义头透传/体逐字节/响应头小写)、POST 体+Content-Length+501 原样上抛、全链 e2e(client→api→transport→store:guest scope 头/body 真 socket 落库,第二跳自动带 `Bearer access-1`)
- [x] 协程点火语义补档:GDScript 协程不能无 `await` 起跑,`call_deferred` 点火 + 用例侧 pump 循环(`pump_until` 900 帧耗尽即 FAIL 不悬挂)收尾结果到成员断言
- [x] 验证:六套 Godot 测试全绿(contract/lifecycle/core/service/service2/platform,退出码 0)+ `go test ./tools/...` + `go vet` 绿
- **落地**:双增量推送(platform+tests、docs 收尾);残留 = 部署面(凭据)+ UE 引擎集成面 + Godot TLS 路径与真机验证(随部署面)+ messages SSE 流式传输适配

## 批次 35 — Godot SSE 流式传输(messages 通道真流读)

- [x] `CourierSseStream`(`src/platform/sseStreamTransport.gd`):HTTPClient poll 流读,行缓冲跨 chunk 喂 `CourierSseParser`,完整帧经 on_event 上抛;契约 messages.md 客户端要求全内建——501 → `capability_disabled`(转纯拉取,不重试)、401 → `unauthenticated`(不重试)、断线指数退避重连(1s 起、上限 60s,`Retry-After` 秒头优先)、收过事件的连接断开即重置退避(健康连≠失败连)、stop 回调每圈探查净停;Authorization 每次尝试按 token_provider 现取(契约:新连接按当前 token 校验);退避等待 sleep_f 可注入(缺省主循环 Timer)
- [x] **HTTPClient 分框硬约束实测入档(类文档代码注释)**:无 Content-Length 的 close 分隔响应体不可读——收完头即判响应完成,永不进 `STATUS_BODY`;SSE 流响应必须 `Transfer-Encoding: chunked`。自建网关(Go net/http 在 Content-Length 缺省时自动补 chunked)天然满足;直连其他无分框 SSE 端点不可用
- [x] 测试件重构:`MockServer` 抽出 `tests/mock_server.gd` 共享(preload,不占全局类名),新增 `enqueue_stream`/`pushes`/`hangup`(SSE 长连持有、chunked 分框、终止块随挂断发出);`test_platform.gd` 改用共享件(六套回归仍绿)
- [x] 测试(`tests/test_stream.gd`,4 例):帧上抛+心跳注释不出事件、流内推送第二帧、服务端断流退避重连(重连现取 token,`Bearer tok-2` 断言轮换)且停止后不再连、501 降级收尾不重试、401 收尾不重试、跨 chunk 行缓冲(字段名被切段拼回)
- [x] 验证:七套 Godot 测试全绿(contract/lifecycle/core/service/service2/platform/stream,退出码 0)+ `go test ./tools/...` + `go vet` 绿
- **落地**:单增量推送(stream+tests+mock 重构、docs 收尾);残留 = 部署面(凭据)+ UE 引擎集成面 + Godot TLS 路径与真机验证、前后台生命周期信号(随部署面/引擎集成面)

## 批次 36 — Godot 前后台生命周期适配(状态机事件入站)

- [x] `CourierLifecycleMonitor`(`src/platform/lifecycleMonitor.gd`,与 Unity ApplicationLifecycleMonitor 同构):引擎通知(`NOTIFICATION_APPLICATION_PAUSED/RESUMED` 移动端前后台、`FOCUS_OUT/IN` 桌面焦点)+ 每帧可见性探测轮询(`visibility_probe` 注入即启用;无注入视为可见,以通知为准)统一经 `_set_visible` 幂等去重——移动端与桌面端信号常成对触发;`bind` 状态机后 `Suspended` ⇄ `ResumeStarted+ResumeCompleted`,恢复到挂起前稳定态;`on_suspended`/`on_resumed` 回调交通道管理方断流省电/重连(契约 messages.md 语义规则 4,本类不持通道)
- [x] 语义差实测入档(代码注释):Godot 4.3 无 `DisplayServer.screen_is_visible`、`SceneTree` 无 `visibility_changed` 信号——前后台只走 Node 通知;挂起仅 Authenticated/PlayerReady 合法(READY 不可挂起,与 Unity 同表);**转移被拒不更新基线可见性,下一帧重试**——隐藏启动、状态机稍后才就绪的场景不丢挂起;SceneTree 脚本测试无 `add_child`(用 `root.add_child`);探测与通知打架会互相翻转(测试中两者须同变,真实环境天然一致)
- [x] 测试(`tests/test_adapter.gd`,7 例):PlayerReady 挂起/恢复原态、幂等去重、未绑定 no-op、隐藏启动留帧重试补挂起、四通知路径(PAUSED/RESUMED/FOCUS_OUT/FOCUS_IN)、缺省探测;用例 monitor 经 `detach`(set_process(false)+queue_free)防跨用例计数污染
- [x] 验证:八套 Godot 测试全绿(contract/lifecycle/core/service/service2/platform/stream/adapter,退出码 0)+ `go test ./tools/...` + `go vet` 绿
- **落地**:单增量推送(monitor+tests、docs 收尾);残留 = 部署面(凭据)+ UE 引擎集成面 + Godot TLS 路径与真机验证、UI 可选包

## 批次 37 — Unity SSE 流式传输(messages 通道真流读)

- [x] L2 接缝 + 退避纯函数(`core/Runtime/Core/SseStream.cs`):`ISseSource`(平台接线面)/`SseStreamConnection`(状态码 + 头小写归一 + 逐行读,流尾回 null)/`SseStreamRequest` + `SseBackoff`(base·2^(n-1) 封顶 max,Retry-After 优先)+ 收尾原因四值(capability_disabled/unauthenticated/stopped/disconnected);接缝放 Core 不放 Service——Adapter 实现不引 Service,编排(依赖 SseParser)在 Service
- [x] 编排(`service/Runtime/SseStreamClient.cs`,纯逻辑):501→`capability_disabled`(降级转纯拉取)、401→`unauthenticated` 终态不重试;断线指数退避重连(1s 起、60s 封顶,`Retry-After` 秒头优先);收过事件的连接断开即重置退避(健康连≠失败连);Authorization 每连现取(轮换);净停经 CancellationToken;逐行喂 SseParser,未知 event 容忍
- [x] 测试(`ServiceTests~/SseStreamTests.cs`,+8 例 = 77):帧上抛/心跳静默/重连换 token、501/401 终态、Retry-After 优先可观察、建连故障重连+净停、取消先行不建连、退避纯函数(封顶/钳制/全零配置);竞态修正——自旋 fake 会持续计数,断言取事件回调点的建连数
- [x] L3 接线(`core/Runtime/Adapter/UnityWebRequestSseSource.cs`,packcheck 结构验收;Adapter 引 UnityEngine 不进 dotnet 单测):DownloadHandlerScript 增量收体(锁保护按 \n 字节切行后再 UTF-8 解码,多字节不撕裂);`SendWebRequest` 主线程起跑(Unity 限制);长连不设超时,`CancellationToken.Register(Abort)` 净停(Abort 归一 `TransportException`);头到即回(responseCode 置位),体走 50ms 轮询增量读;服务端必须 chunked 分框(与 Godot/UE 同硬约束,Go net/http 网关缺省满足)
- [x] CI:`tests.yml` 增 unity-dotnet job,四套纯逻辑 dotnet 工程进门禁(引擎内 Test Runner 用例不进 CI)
- [x] 验证:四套 dotnet 全绿(8+48+77+11)+ `go test ./tools/packcheck/` + `go vet` 绿
- **落地**:三增量推送(ci gate、stream client、adapter;docs 收尾);残留 = Unity 引擎内 Test Runner 实跑与真机联调(随部署面)

## 批次 38 — TS 三端 + UE messages SSE 流客户端(六端流编排同构收口)

- [x] cocos 参照实现:`core/sseStream.ts`(接缝 SseSource/SseStreamConnection/SseStreamRequest + 退避纯函数 sseBackoffDelayMs + FetchSseSource fetch 流读默认实现;行切分按 \n 剥尾 \r,多字节经 TextDecoder stream:true 跨 chunk 安全;取消经 AbortSignal,网络失败归一 TransportError)+ `service/sseStreamClient.ts`(编排:501→capability_disabled、401→unauthenticated 终态不重试;断线指数退避重连 1s/60s 封顶、Retry-After 秒头优先;健康连重置退避;Authorization 每连现取;逐行喂 SseParser)——与 unity SseStream/SseStreamClient 全同构
- [x] 测试(`tests/stream.test.ts`,+9 例 = 111):退避纯函数、帧上抛/心跳静默/重连换 token/净停、501/401 终态、Retry-After 优先且仅对本次响应生效、建连故障重连、取消先行不建连 + **FetchSseSource 真服务器 e2e**(node:http chunked 分框、断流重连现取 token、真 501)
- [x] **三坑实测入档**:①注入 sleep 立即返回时 `lastDelayMs` 只留最新值,可观察断言须在注入 sleep 捕获实参(unity 用真实 7s 延迟掩盖了此竞态);②client 已按次现取 token 拼头,source 不得再调 tokenProvider(否则首连即取走第二个 token);③测试侧 run 循环未净停即返回会让子进程永不退出(宏任务自旋),每用例 finally abort+settle
- [x] laybox/miniprogram 同构复制(共享面 core/service 逐字节一致,批次 31 工具已守同):node:test 91/92 例全绿
- [x] UE C++:`core/sse_stream.hpp`(接缝 SseSource/SseStream/SseReadResult 值语义 + 退避纯函数,与 UE Transport 同口径——引擎接线留 L3 集成面)+ `service/sse_stream_client.hpp`(同构编排,净停靠 should_stop 谓词,sleep 注入;头键小写归一)+ `service/sse_stream_test.cpp`(7 例:退避、帧/心跳/重连换 token/退避序列、501、401、Retry-After、建连故障重连+事件后净停、取消先行),`run_tests.sh` 七套全绿
- [x] **UE 语义差入档**:should_stop 以"收到事件"为据时,事件后的下一读循环顶即净停(停了不重连)——客户端行为正确,测试预期须按此设;C++ 类作用域名先声明后使用(成员别名置于类首)
- [x] 验证:cocos 111/111、laybox 91/91、miniprogram 92/92(node:test)、UE 七套 g++ 全绿(exit 0)+ `go vet` 绿
- **落地**:三增量推送(cocos、laybox+miniprogram、UE;docs 收尾);残留 = 平台特化流源(wx.request enableChunked、laya XHR 流读)与 UE 引擎接线(随平台/引擎环境)

## 批次 39 — 手写 UTF-8 解码器 + 微信 enableChunked 流源(平台特化第一批)

- [x] `core/utf8.ts` 增量 UTF-8 解码器(三端同构):跨块多字节不撕裂、非法输入(超长/代理区/越界/孤立续字节)U+FFFD 降级、流尾截断冲刷——wx 等无 TextDecoder 环境安全;+7 例(全切分点扫描、逐字节、emoji 三块、截断冲刷)
- [x] 微信流源 `platform/wechat/sseSource.ts`(cocos + miniprogram):`createWechatSseSource`——wx.request enableChunked,success 头到回填连接(statusCode/headers),onChunkReceived 增量解码→行缓冲→阻塞读;fail 按是否已建连分流(建连失败重试 / 已建连归一断流重连);AbortSignal → task.abort() → wx 以 fail 收场
- [x] 接缝调整:`SseStreamConnection.statusCode/headers` 改非只读(wx 响应头在 success 回调才到,接线须建连后回填;与 unity settable 同口径),三端同步
- [x] 测试(+5 例,cocos/miniprogram `tests/wechatStream.test.ts`,fake wx 手动 pushTo/hangup 驱动):分块行缓冲、跨块 emoji(🎮 F0 9F 8E AE 拆两块)、心跳静默、头键小写归一(GET/enableChunked/Accept/token 断言)、断流重连现取 token(tok-1→tok-2)、建连失败重试、abort 净停(task.abort 被调)、Retry-After 大写头键生效
- [x] **三坑实测入档**:①wx chunked 无"服务端净关流"回调——断流只以 fail(或静默)呈现,SDK 按 fail 归一重连,静默靠心跳注释与生命周期兜底,真机待验随部署面;②测试 fake 的 abort 必须触发 fail(真机语义 abort 以 fail 收场),否则连接永不苏醒、run 挂死;③无数据时 run 阻塞在读上——推块前 `waitUntil(事件到)` 永不满足,测试须先等请求落地再驱动块
- [x] 验证:cocos 123/123、miniprogram 104/104、laybox 98/98(node:test)全绿
- **落地**:残留 = laya XHR 流读(平台特化)、UE SseSource 引擎接线(引擎环境)、微信真机验证(部署面)

## 批次 40 — laya XHR 流源(平台特化流源全线收口)

- [x] `platform/laya/sseSource.ts`:`createLayaSseSource`——XHR 流读,onprogress 体增量按消费偏移切行(responseText 已含全部已收字节,浏览器内建流式解码跨块多字节不撕裂);**onload = 净关流回调**(与微信相反,XHR 有干净流尾 → 归一流尾走编排层重连);头/状态码 onprogress/onload 首拍回填连接(XHR 无独立"头到"回调);timeout 置 0 长连不禁超时;onerror/ontimeout/onabort 统一按是否已建连分流(建连失败重试 / 已建连断流重连)
- [x] `layaApi.ts` XhrLike 补面:readyState/onprogress/onabort(流式源所需;真 XHR 天然满足)
- [x] 同步修 wx 两份 sseSource 的 `settled` 可见性(private → public:建连回调在工厂闭包按它分流,tsc 消费方会拒 private 越界访问)
- [x] 测试(+6 例,`tests/layaStream.test.ts`,FakeLayaXhr 手动 head/progress/finish/fail 驱动):帧/心跳静默/多字节跨 progress、净关流重连现取 token(tok-1→tok-2)、建连失败重试、abort → xhr.abort 被调 + 净停、429 Retry-After 大写头键生效、无体直收尾(onload 首拍建连 + 流尾 → 重连)
- [x] 验证:laybox 104/104、cocos 123/123、miniprogram 104/104(node:test)全绿;共享面(sseStream.ts 三端、wx sseSource 双端)逐字节一致
- **落地**:六端流源全景 = unity WebRequest / godot HTTPClient / TS fetch / wx enableChunked / laya XHR / UE 接缝(引擎接线随引擎环境);真机验证随部署面

## 批次 41 — ui/diagnostics 可选包三端对齐(声称漂移修复)

- [x] 巡检发现 layers.md L3 行声称"TS 三端…ui 包批次 17、diagnostics 包批次 20",实际两包只在 cocos(732 行 + 19 例)——文档声称与现仓漂移
- [x] 移植:`src/ui`(brandingCatalog + 三面板骨架)+ `src/diagnostics`(diagnosticsHub/Types/httpReporters)逐字节复制到 laybox/miniprogram——两包 import 全为共享面路径(core/service/同包),零改动移植即绿
- [x] 测试:三端各 123/123(cocos 123、miniprogram +19=123、laybox +19=123,node:test);tsync 同构守同仍绿(共享面不含 ui/diagnostics,批次 31 设计如此)
- [x] 文档:laybox/miniprogram README 补包结构与计数(104→123);layers.md L3 行改为"ui 包批次 17、diagnostics 包批次 20 落位(cocos 首落,批次 41 三端对齐)"
- **落地**:TS 三端面全对齐(共享面 + 可选包);剩余差异仅平台绑定(wechat/laya)与端专属测试

## 批次 42 — 声称漂移修复(Godot 绑定"规划中"四处清账)

- [x] 巡检发现:architecture.md 两处 + layers.md 两处仍称 "Godot 规划中"(binding 枚举 / token 存储枚举 / 打包形态 / ITokenStore 接口形状),而批次 34/35/36 已交付真实绑定(HTTPClient poll 真传输、user:// 持久令牌、SSE 流读、前后台信号,八套 headless 全绿)——本机复跑 godot 八套确认仍绿后修文档
- [x] 修正口径:binding 枚举补 Godot(项目形态已落位,插件化打包随引擎集成面);token 存储枚举补 user:// 持久文件;ITokenStore 接口形状行同步;全仓 grep "Godot 规划中" 清零
- [x] tsync 守同扩面:ui/diagnostics 目录与同名 tests 从"端专属豁免"收进共享校验(批次 41 三端对齐后的自然收口;laya/wechat 仍豁免——生命周期信号真随端不同)
- **落地**:文档账面修正 + 守同面扩大;无产品代码变更

## 批次 43 — 巡检续批(只读复核):契约分框缺口补登

- [x] 全仓健康检查(只读):Go tools 3 包 ✓、gateway 17 包 ✓、TS 三端各 123/123 ✓、UE 七套 exit 0 ✓、Unity dotnet 144 例(48/77/8/11)✓、Godot headless 八套 ✓;工作树干净,origin/main 与本地 0/0 同步
- [x] docs 对账:三端 README 计数(123)与实际一致;unity README 计数(8+48+77+11=144)与实跑一致;ue 七套/godot 八套声称与实际一致;todo 0 未勾、批次 42 条目齐;契约域(11)↔ 网关能力前缀(9)映射一致(config/branding 归 app 域、认证经 scope 中间件、diagnostics SDK 直发不经网关);最近 12 commit 走查——英文短句、无 AI 痕迹、署名唯一
- [x] **发现并修复契约缺口**:messages.md 流协议节从未写明服务端分框要求,而六端流读实现(fetch / wx.enableChunked / XHR onprogress / UnityWebRequest / HTTPClient poll / UE 接缝)均按增量可读语义实现——定长缓冲响应会破坏流读,契约与实现存在未写明的硬依赖。按 index.md「只加不改」原则补「传输分框(服务端硬要求)」一条;并实测网关 chirp handler 已满足(`Content-Type` 设定后逐帧 `Flusher.Flush()`,Go net/http 在无 Content-Length 时自动 chunked)
- [x] 版本登记:messages.md Frozen v1 → **Frozen v1.1**(2026-10-11 追加传输分框硬要求,不改变端点/头/DTO/wire 形态),index.md 状态行同步
- **落地**:契约 v1.1 只增不改登记完成;复核未发现其他漂移或缺陷

## 批次 44 — 竞品调研功能点落地(competitive.md 对齐,2026-10-11)

> 来源:[docs/research/competitive.md](./research/competitive.md) 的 6 条可参考项 + 3 条边界。**已有落点的不重复立项**(见本批末「覆盖对照」),以下只补调研点到任务行的缺口:

- [ ] **各端示例齐平**(Nakama 实践第一半:契约跨端一致已由 `tools/contractgen` 六端测试守住,但「示例与文档齐平」缺):现全仓无 `examples/` 目录 —— 为六端(unity / cocos / ue / godot / miniprogram / laybox)各建一个最小可跑示例(接入 → 登录 → 收公告 → 发工单),放 `examples/<端>/`,并给运行方式
- [ ] **按需选装指南**(MSDK 启示:「不为用不到的功能付体积」是被验证的付费需求):docs 出一页**选装清单** —— UPM 四包(core/service/ui/diagnostics)× Provider 逐项列出「不装省什么、装了得到什么」,附各包**构建产物实测字节**(非估算);现 docs 对「选装/体积」0 命中
- [ ] **每端 quickstart 实测**(Nakama 实践第二半:示例与文档齐平):逐端跑 README 的 quickstart,给出命令与实际预期输出,跑不通的修文档不改契约
- [ ] **对标数据时效**(competitive.md 数据口径 2026-10-08):星数/活跃度随巡检批次复核更新;链接失效提 issue

**覆盖对照(调研 9 项的落点,避免重复立项)**:

| 调研项 | 落点 | 状态 |
| --- | --- | --- |
| Nakama 跨端契约一致 | 批次 1 `contractgen` 六端生成 + CI 守 | 已有 |
| Nakama 示例/文档齐平 | **本批 1、3** | 缺口 → 立项 |
| MSDK 插件化按需选装(设计) | 批次 2/18 Provider 可换、UPM 四包 | 已有 |
| MSDK 选装的产品化呈现 | **本批 2** | 缺口 → 立项 |
| QuickSDK 服务端验签 | 批次 3 auth Frozen + 批次 10 回调只认渠道签名 | 已有 |
| XDSDK 实名防沉迷 | `realname.md` Frozen v1 + 批次 7 熔断/降级 | 已有 |
| XDSDK/MSDK 登录 UI 剥离 | `com.courier.ui` 独立可选包,批次 17/41 三端对齐 | 已有 |
| Playgama 平台差异压进 Adapter | `layers.md` L3 + packcheck 结构验收 | 已有 |
| 边界(实时/渠道不做) | `roadmap.md` 非目标 + 本文件「明确不做」 | 已有 |

- **验收**:六端示例各一条命令真实跑通(报文附运行输出);选装清单含实测字节;quickstart 逐端实测记录;对标口径更新到当期日期

## 批次 45 — 功能清单全量对照(features.md 76 项入账,2026-10-11)

> 来源:[features.md](./research/features.md) §1–§12 全量 76 项。**本节是功能清单不是新任务**:已有落点的条目注明批次、不重复立项;`[ ]` 是仓库内真实缺口(汇总见 [implementation-status.md](./research/implementation-status.md));`[Provider·不进核心]` 与 `[待议]` 不打勾;⬜不做 7 项不入本节,只在 implementation-status 留档。统计:核心 47 = 已落地 37 + 缺口 10;Provider 13;待议 9;不做 7。
> **接口边界口径**:落点写到契约域的 = 进核心契约(边界内);`[Provider·不进核心]` = 能力存在但接口归 Provider,契约只留红线或钩子;`[待议]` = 边界未定,先不立项;⬜不做 = 边界外。每条目的「落点:契约 xx.md / 端 yy」即该功能的接口边界。

### §1 账号与身份

- [ ] **F01 登录方式聚合**(游客/账号密码统一登录入口)｜落点:契约 auth.md / 端 六端 identity——批次 3 已覆盖游客+邮箱密码;第三方 OAuth(Apple/Google/Steam/微信/QQ)与手机号 auth.md:10 明示「不覆盖,后续域契约扩展」
- [x] **F02 账号绑定/解绑**(一个账号挂多平台身份,游客升级为正式账号)｜落点:契约 auth.md bind/解绑 / 端 六端(批次 3/18 已覆盖;多正式账号合并不在 v1)
- [ ] **F03 账号注销与数据删除**(完整注销+下游数据级联删除,商店政策要求游戏内可达)｜落点:契约 auth.md——现仅设备解绑(`DELETE /devices/{deviceId}`,auth.md:24),账号注销端点未建
- [x] **F04 跨平台身份贯通**(同一账号跨设备共享身份,多设备会话+设备管理)｜落点:契约 auth.md devices / 端 六端(批次 3/18 已覆盖;进度共享归游戏后端,player 档案映射角色批次 8)
- [x] **F05 SDK 不带 UI,游戏自绘**(数据与回调为主,UI 永远可选包)｜落点:UPM 四包 / TS 三端 ui+diagnostics 包(批次 5/17/41 已覆盖)
- [x] **F06 匿名登录与静默自动登录**(先进游戏后补账号)｜落点:契约 auth.md 游客设备 / 端 六端(批次 3/4 已覆盖,e2e 全链路)
- [Provider·不进核心] **F07 账号风控与防刷**(设备指纹/异常登录/封号分级)｜落点:无契约域——登录限流已内建(批次 3);支付风控为预留钩子 `PAYMENT_RISK_REJECTED`(缺省不拦)

### §2 会话、令牌与调用安全

- [x] **F08 access/refresh 与自动续期**(过期静默刷新不打断游戏)｜落点:契约 auth.md+primitives v1.1 并发纪律 / 端 六端 session(批次 3/4/12/14/32 已覆盖,单飞刷新+重放一次)
- [x] **F09 服务端验签**(客户端凭证不可信)｜落点:auth Bearer 校验(批次 3)+payment 回调 HMAC 签名 fail-closed(批次 10)
- [x] **F10 scope/多环境隔离**(game_id+env 隔离,越权显式报错)｜落点:契约 scope.md / 网关 scope 中间件(批次 2 已覆盖)
- [x] **F11 统一错误码与可重试语义**(客户端可判定重试/降级/终止)｜落点:契约 errors.md v4 33 码 / 端 contractgen 六端生成(批次 1/29 已覆盖,对差零漂移)
- [x] **F12 客户端凭据与密钥管理**(SDK 内嵌 key 只做准入,真凭证在服务端)｜落点:architecture.md 安全边界——客户端零 Provider 凭证(批次 2 起架构红线)

### §3 玩家数据与存档

- [待议] **F13 云存档/进度同步**——存档面留 Provider,不做统一钱包式假设(features.md §3 取舍)
- [待议] **F14 分级读写权限**(本人/公开/仅服务端分层)
- [待议] **F15 写锁与冲突合并**(乐观锁/版本号)
- [x] **F16 玩家档案**(昵称/头像/元数据,账号↔角色映射)｜落点:契约 player.md / 网关 archivist(批次 8 已覆盖)
- [Provider·不进核心] **F17 二进制文件存储**(存档文件/截图/UGC 对象存储)｜落点:Provider 化;support 附件同口径(support.md:11)
- [x] **F18 服务端权威写入**(数值类数据只由服务端写)｜落点:Gateway 唯一入口(批次 2)+payment 服务端定价(批次 10)

### §4 支付与商业化

- [x] **F19 支付商品列表与渠道拉起**(服务端下发商品,拉起归渠道 SDK)｜落点:契约 payment.md SKUs+payToken / 网关 teller(批次 10 已覆盖;真实渠道沙箱随部署面)
- [x] **F20 服务端收据验真**(回调签名即认证,先于 body 解析 fail-closed)｜落点:契约 payment.md 回调签名面 / 网关 teller(批次 10 已覆盖;商店 API 真核验随部署面)
- [x] **F21 幂等与防重放**(同一收据只入账一次)｜落点:网关 teller 回调幂等不二次发货(批次 10 已覆盖,单测+e2e)
- [x] **F22 商品一致性与订单履约**(校验商品与实付后才发货)｜落点:网关 teller 服务端定价防伪造金额(批次 10 已覆盖,e2e)
- [Provider·不进核心] **F23 订阅状态管理**(到期/续订/退款跟随商店通知)｜落点:随真实渠道接入(部署面)
- [x] **F24 跨设备恢复购买**(换设备拉历史订单恢复权益)｜落点:契约 payment.md `GET /orders`(payment.md:64,批次 10 已覆盖)
- [x] **F25 交易流水与对账**(全量交易可查,补单/对账/审计)｜落点:网关 teller Reconcile(payment.md:91,批次 10/11 已覆盖;持久化与定时对账随部署面)
- [x] **F26 支付回调与补发/掉单处理**(发货中断查询/补发/恢复)｜落点:断单停 PAID+对账重发(批次 10/11 已覆盖,e2e 断单恢复)

### §5 合规与未成年人保护

- [x] **F27 实名认证**(对接官方实名系统,开发者主体上报)｜落点:契约 realname.md / 网关 warden(批次 7 已覆盖)
- [x] **F28 游戏时长与时段限制**(未成年人可玩时段云端查询)｜落点:realname curfew(F28 窗口,批次 7 已覆盖)
- [x] **F29 充值额度限制**(分龄单次/月度限额)｜落点:realname charge-check(F29 分龄限额,批次 7 已覆盖)
- [x] **F30 防沉迷数据上报**(时长/消费 S2S 上报)｜落点:realname playtime-report S2S(批次 7 已覆盖,幂等月累计)
- [ ] **F31 年龄门与同意管理(海外)**(首启年龄筛查,VPC 家长同意)｜落点:realname.md:100 明示 v1.1 预留 consent 组(只加不改,出海需求触发再冻结)——未立项
- [Provider·不进核心] **F32 数据最小化与第三方共享清单**(采集项/目的/权限清单)｜落点:红线内建契约(默认关闭/字段最小化,contract/index.md 宪法 5/6);清单输出归 Provider
- [ ] **F33 审计与合规留痕**(实名/同意/限额判定可追溯日志)｜落点:trace 贯穿 gateway→Provider 已实现(layers.md L5);审计落库随部署面

### §6 消息与触达

- [x] **F34 公告系统**(三形态/起止时间/排序,后台配置下发)｜落点:契约 announcement.md / 网关 herald(批次 6 已覆盖)
- [ ] **F35 公告定向过滤**(按 OS/渠道/版本/大区过滤,客户端只拿命中项)｜落点:announcement.md v1.1 `filters` 对象(features.md §14 落点建议)——v1 未入
- [ ] **F36 跑马灯/滚服播报**(全服滚动消息,与公告同源不同呈现)｜落点:数据面复用公告契约零新契约(severity 字段 announcement.md:32);跑马灯呈现归可选 UI 包——未做
- [Provider·不进核心] **F37 远程推送**(APNs/FCM 厂商通道)｜落点:应用内 SSE 已落(chirp,批次 6/35-40 六端流读);厂商通道随部署面,不自建长连接
- [Provider·不进核心] **F38 按账号/标签定向推送**
- [Provider·不进核心] **F39 本地通知/定时提醒**
- [Provider·不进核心] **F40 触达效果统计**

### §7 客服与玩家反馈

- [x] **F41 游戏内工单**(不离开客户端提问/查进度)｜落点:契约 support.md / 网关 croupier(批次 6 已覆盖)
- [x] **F42 FAQ/知识库自助**(客户端内检索,命中免工单)｜落点:support FAQ+sage 检索问答(批次 6/9 已覆盖)
- [ ] **F43 上下文自动附带**(自动带 player ID/build/关卡)｜落点:support.md v1.1 可选 `context` 对象(features.md §14 落点建议)——现 croupier 以服务端 extras 承载,契约未入
- [ ] **F44 反馈与日志上传**(一键反馈+截图/日志附件)｜落点:support.md:11 明示「附件后续扩展」;与 diagnostics 联动未定义
- [ ] **F45 举报与处罚申诉**(举报/封禁通知/申诉闭环)｜落点:无契约面(support.md 无举报域)
- [x] **F46 客服侧补单/补偿**(按账号查交易/补发)｜落点:支付 Reconcile 补发通道(批次 10/11 已覆盖;工具面归 croupier 管理侧)

### §8 运营配置与实验

- [x] **F47 远程配置/Feature Flag**(不发版改数值,灰度+热生效)｜落点:契约 config.md / 网关 scribe(批次 8 已覆盖)
- [x] **F48 分群与条件定向**(按版本/设备/自定义条件下发)｜落点:config 条件投影五维(批次 8 已覆盖)
- [Provider·不进核心] **F49 A/B 实验**(对照组/显著性判定)｜落点:垂直领域不自建,契约不建(features.md §14)
- [x] **F50 配置版本历史与回滚**(版本单调,回滚=历史内容再发布)｜落点:契约 config.md 版本语义(config.md:11,批次 8 已覆盖)

### §9 数据、崩溃与归因

- [Provider·不进核心] **F52 埋点与行为分析**｜落点:不自建分析产品,归游戏自有分析栈或 Provider
- [Provider·不进核心] **F53 付费与广告收入统计**
- [Provider·不进核心] **F55 崩溃与性能监控**｜落点:diagnostics 可选包直发自托管端点(Sentry/GlitchTip/OTLP),不经网关(批次 8/20/41)
- [x] **F56 SDK 自身遥测**(调用量/延迟/SDK 健康度)｜落点:契约 diagnostics.md / 端 diagnostics 可选包(批次 8/20/41 已覆盖,默认全关)
- [Provider·不进核心] **F57 数据导出**

### §10 进度、成就与留存玩法

- [待议] **F58 排行榜**——玩法非服务,防范围失控(features.md §13 结论⑤)
- [待议] **F59 成就/任务**
- [待议] **F60 数值统计(Stats)**
- [待议] **F61 赛季/通行证/签到**
- [待议] **F62 邮件/礼包码**(与消息面相邻,批次 9 后按需优先评估)
- [待议] **F63 活动与限时运营**

### §12 SDK 工程与交付

- [x] **F69 多引擎一等公民**(契约行为跨端一致)｜落点:contractgen 六端生成+CI 门禁(批次 1/19;六端 unity/cocos/ue/godot/miniprogram/laybox 批次 12-16/32-33)
- [x] **F70 模块化按需选装**(只引需要的模块)｜落点:UPM 四包+可选包三端对齐(批次 5/17/41);选装产品化呈现=批次 44 已立项
- [x] **F71 UI 可选/游戏自绘**(无强制 UI)｜落点:UI 永远可选(批次 5/17/41 已覆盖,同 F05)
- [x] **F72 初始化与生命周期**(单次 Init+确定回调)｜落点:生命周期状态机九态十触发六端同构(批次 4/13/14/32)
- [x] **F73 弱网重试与离线降级**(退避重试+只读缓存兜底)｜落点:RetryPolicy 退避+config 缓存+公告拉取兜底(批次 4/6/12)
- [x] **F74 日志与诊断开关**(可调级别+trace ID+可上报)｜落点:diagnostics 开关+traceId(批次 8/20)
- [x] **F75 沙箱/测试环境**(dev/staging/prod+mock fixtures)｜落点:scope 三环境+contractgen fixtures(批次 1/2)
- [ ] **F76 多语言与本地化**(内容按语种下发,SDK 文案可本地化)｜落点:全契约无语种/locale 字段(grep 零命中);announcement v1.1 语种字段评估+UI 包文案本地化口径

## 批次 46 — 外部架构评审要点归档(2026-10-11)

- [x] Provider 原则补两条:接口统一 ≠ 数据迁移;接入方式两种(服务端 Provider 默认/客户端直连例外)→ [architecture.md](./architecture.md)
- [x] roadmap 补「验证与交付四阶段」(A 证据审计→B 跨端一致→C 可替换性→D 可交付)与验收七问 → [roadmap.md](./roadmap.md)
- [x] 实现状态汇总表落档 → [implementation-status.md](./research/implementation-status.md)(批次 45 同产出)

## 批次 47 — 契约扩展性加固:三份 v1.1 提案 + 七份扩展预留补注(Draft,2026-10-11)

> 来源:批次 45 缺口清单 + 外部评审「不要设计太死,每个接口要写明万一扩展怎么处理」。**全部为加法**:提案标 Draft 未冻结,预留节为加注不动冻结行;不动实现、不动 fixtures、不升 Frozen 版本号——冻结升版 + 错误码入 errors.md + contractgen 六端同步随评审通过一批落地。

### v1.1 提案(待评审:≥ Unity + 一非 C# 端过读)

- [ ] **F03 账号注销与数据删除** → [auth.md](./contract/auth.md) v1.1 提案:delete/cancel 两端点、宽限期(GUEST 立即删)、受理即吊销会话、匿名化保留外键、`OnAccountDeleted` Provider 钩子承载级联;拟新增 2 错误码
- [ ] **F35 公告定向过滤** → [announcement.md](./contract/announcement.md) v1.1 提案:复用 config 投影语义(缺省维度跳过 + DSL 不进玩家契约)、`filters` 开集 map 只读回显、零新错误码
- [ ] **F43 工单上下文** → [support.md](./contract/support.md) v1.1 提案:`context` 开集 map 服务端零解释透传、SDK 自动附带基础事实面且不可覆写、零新错误码

### 扩展预留补注(每域接口的「万一要扩展怎么处理」)

- [x] [primitives.md](./contract/primitives.md):加字段只可选 / 未知字段容忍 / ID 前缀登记制 / 语义变更升版
- [x] [errors.md](./contract/errors.md):加码兼容走兜底 / 废弃码保留不重用 / 域前缀登记制
- [x] [scope.md](./contract/scope.md):env 不得封闭枚举 / orgId 维度预留 / token 跨游戏永不原地放开
- [x] [branding.md](./contract/branding.md):新素材位加可选字段走管理面 / 缺省走兜底链 / 素材托管归接入方
- [x] [payment.md](./contract/payment.md):新渠道签名面不变 / 形态加枚举不改状态机 / 订阅·余额·退款扩展位预留
- [x] [app.md](./contract/app.md):新只读端点兼容 / 合规文案归 Branding / 更新分发预留后续域
- [x] [diagnostics.md](./contract/diagnostics.md):新类别默认关红线不变 / props 开集 / sampling·consent 预留 / 直发红线不松动
- [x] [index.md](./contract/index.md) 变更流程第 1 步固化:每条提案必须含「扩展与演进」节,禁止钉死封闭枚举

### 冻结与实现(评审通过后)

- [ ] 评审通过 → 三域升 Frozen v1.1 + errors.md v5(仅 AUTH 两码)+ contractgen 六端同步 + 各端 SDK 实现 + e2e(宽限期 / 撤销 / 过滤命中 / 上下文回显)
- **下一步缺口**:F45 举报申诉(需新端点组设计)、F76 多语言(与 F35 的 `language` 维度耦合,F35 冻结后并入)、F01 第三方 OAuth(新域契约)、F44 附件、F36 跑马灯 UI 呈现

## 批次 48 — 契约 v1.1 提案续:多语内容 + 举报处罚申诉(Draft,2026-10-11)

> 来源:批次 47「下一步缺口」前两项。同口径:加法提案、不动冻结行 / fixtures / 版本号;每条含「扩展与演进」节;随批次 47 三提案同批评审,冻结前不实现。

- [ ] **F76 多语言与本地化** → [announcement.md](./contract/announcement.md) v1.1 提案续:单语分条模型契约化(零破坏,`title`/`body` 类型不动)+ DTO 可选 `language`/`groupKey` + SDK 自动附带 `language` 查询参数(游戏可覆写);多语对象 / 多语并存明示 v2 破坏性,永不原地改;语种回退归管理面兜底
- [ ] **F45 举报与处罚申诉** → [support.md](./contract/support.md) v1.1 提案续:reports / penalties / appeal 四端点;**处罚判定与执行全在管理面**,契约只做玩家侧投影 + 申诉入口(网关不执行风控);拟新增 2 错误码(`SUPPORT_PENALTY_NOT_FOUND` / `SUPPORT_APPEAL_EXISTS`);v1 不动 auth 登录语义,拦截联动留 v1.2 提案
- [ ] 评审通过与批次 47 同批冻结 → errors.md v5(AUTH 两码 + SUPPORT 两码)+ contractgen 六端同步 + 各端 SDK 实现 + e2e(宽限期 / 撤销 / 过滤命中 / 上下文回显 / 申诉闭环)
- **剩余缺口**:F44 附件(需对象存储面设计,Provider 口径)、F36 跑马灯 UI 呈现(实现非契约)、F01 第三方 OAuth(新域契约)、F31/F33 维持

## 明确不做

- 实时多人/匹配/大厅(Nakama / Agones 地盘);渠道包聚合联运(MSDK / QuickSDK 地盘);运营端界面(默认供应商 Croupier,可自建);自建长连接(默认 Provider chirp,可换);统一钱包假设。
