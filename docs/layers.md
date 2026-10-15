# 五层架构:L1–L5

> **状态**:Current — 随实现维护(2026-10-08 M0 立宪冻结,M1–M5 已全部落地)。原则:**不堆业务,先立骨架**;总纲见 [architecture.md](./architecture.md)(设计原则 / Provider 原则 / 能力树 / 生命周期),契约宪法见 [contract/index.md](./contract/index.md),分批节奏见 [todo.md](./todo.md)。

## 分层总览

```text
 游戏客户端:Unity / Unreal / Cocos / Godot / 小程序 / Layabox
 ───────────────────────── 客户端侧 ─────────────────────────
 L1 SDK Contract      语言无关契约:DTO、错误模型、scope、事件、生命周期(唯一事实源)
 L2 Core              平台无关内核:状态机、token 存储接口、重试、序列化
 L3 Platform Adapter  平台绑定:原生网络栈、安全存储、生命周期、打包形态(+可选 UI 包)
 ───────────────────────── 线 路 ───────────────────────────
                      HTTPS JSON(SSE 推送通道 M2+,通道为 Provider 能力)
 ───────────────────────── 服务端 ───────────────────────────
 L5 Gateway           唯一入口:auth / session / scope / routing / aggregation / middleware
 L4 Service Provider  Provider 接口 + 默认实现(默认供应商可换可关)
 ───────────────────────── 生 态 ────────────────────────────
   默认供应商(仅以 Provider 角色存在):courier-account(账号,第二实现 courier-account-alt 可换) herald(公告,第二实现 herald-alt 写时索引可换) croupier(客服,第二实现 croupier-alt 写时属主索引+键集游标可换) chirp(推送,第二实现 chirp-alt 环形日志+游标可换) warden(实名,第二实现 warden-alt 托管核验可换) scribe(配置/品牌/版本,第二实现 scribe-alt 写时维度索引可换) archivist(档案,第二实现 archivist-alt 事件日志可换) sage(助手,第二实现 sage-alt 写时预计算规范化可换) teller(支付,第二实现 teller-alt 事件账本投影可换)
```

L1–L3 在客户端侧,L4–L5 在服务端侧;L1 被所有层依赖,自己不依赖任何层。

## L1 SDK Contract(契约层)

**职责**

- 跨端公共类型与行为约定:DTO、统一错误模型、scope 规则、事件信封、SDK 生命周期状态机、各能力接口形状。
- 基元契约已立宪:[contract/](./contract/index.md)(primitives / errors / scope / versioning / events);域契约随里程碑冻结。

**边界**

- 不含任何实现与平台细节;**不感知任何供应商**——契约描述能力,不描述实现者。
- 采集/合规类能力(诊断、实名)的红线内建于契约:默认关闭、显式开启、开启明示数据范围。
- 变更走 [contract/index.md](./contract/index.md) 变更流程;冻结后只加不改,破坏性变更升版本。

**接口形状**

- 基元:primitives / errors / scope / versioning / events;域:auth(Frozen v1.1)、announcement/support/messages(Frozen v1.1)、realname(Frozen v1)、config/app/branding/player/diagnostics(Frozen v1 · M3)、assistant(Frozen v1 · M4)、payment(Frozen v1.1 · M5)、oauth(Frozen v1,批次 52)。

**现仓映射**:`docs/contract/`(本仓)+ `docs/contract/fixtures/` + `tools/contractgen`(契约测试骨架:fixture 生成六端契约源,漂移即测试红)。

**下一步**:基元五件、auth、M2 三域、realname、M3 五域(config/app/branding/player/diagnostics)、assistant(M4)、payment(M5)均已冻结;新域随新里程碑开工前冻结。

## L2 Core(平台无关内核)

**职责**

- 可单测纯逻辑:DTO 解析与校验、**生命周期状态机**(Uninitialized→…→PlayerReady、Suspended/Resuming)、token 存储接口、transport 接口、重试与超时、错误归一(错误码→本端枚举)。
- `CourierClient` 门面仅组 Identity / Session 两接入点;能力树九域接入点归 Service 包 `CourierServices` 门面(各端同构)。

**边界**

- 不 import 任何平台 API(ITransport / ITokenStore / IClock 注入);不写 UI;**不含品牌逻辑**;不感知 Provider 实现。

**接口形状**

- `CourierClient.Init({ gameId, env, endpoint })` → 按域子模块;域接口与 L1 一一对应。

**现仓映射**:`sdks/unity/packages/com.courier.core/Runtime`(Core/ 状态机与 API 客户端、Identity/ Session/ auth 域、CourierClient.cs 门面、Core/Contract/ 生成物);`CoreTests~`(65 例 Fact,xunit;dotnet test 本地可验,引擎 Test Runner 亦跑);`sdks/cocos src/core`(transport/apiClient/session/identity/sseStream/utf8,courierClient.ts 门面)+ `src/service`(九域客户端 + SseStreamClient 流编排,`CourierServices` 门面)+ `src/ui` / `src/diagnostics`(可选包),node:test 171 例(契约 6 / core 25 / coreTransport 13 / lifecycle 35 / utf8 8 / service 16 / service2 14 / stream 13 / wechat 7 / wechatStream 5 / ui 16 / diagnostics 13);`sdks/ue/core`(纯 ISO C++17 header-only:json 解析器、transport/apiClient/session/identity/lifecycle/courierClient/sse_stream)+ `sdks/ue/service`(SSE 解析器 + 流编排 + 九域客户端,`run_tests.sh` 七套 g++ 独立编译全绿);`sdks/godot src/core`(lifecycle/transport/tokenStore/courierError/apiClient/identity/session/courierClient)+ `src/service`(SSE 解析器 + 九域客户端 + `CourierServices` 门面,headless 八套 SceneTree 测试全绿);其余端同构。

**下一步**:六端 messages SSE 流客户端同构落位(unity/godot 真流读接线;TS 三端 fetch 流源 + UE SseSource 接缝,批次 38;微信 enableChunked 流源 + 手写增量 UTF-8 解码,批次 39;laya XHR 流源,批次 40);平台特化流源残留 UE SseSource 引擎接线随引擎环境;真机联调随部署面。

## L3 Platform Adapter(平台绑定层)

**职责**

- 平台翻译四件事:网络(原生 HTTP / SSE 流式读)、存储(ITokenStore 安全存储实现)、生命周期(前后台/进程信号→状态机事件)、线程(主线程回调调度);打包形态(UPM / npm 已落;UE 插件化与 Godot 插件化打包随引擎集成面,仓内 UE 为引擎无关 C++ 头文件树、Godot 为项目形态)。
- **可选 UI 包挂在平台侧**:AnnouncementPanel / CustomerServicePanel / AssistantPanel / RealNamePanel,消费 Branding 下发(见 L2 边界:Core 无品牌逻辑)。

**边界**

- **禁带业务**:Adapter 不出现任何业务语义;业务逻辑要么上移 Core/Service,要么属 UI 包。
- 每平台一个适配器;平台特有扩展以 Core 接口为基类;跨平台共享逻辑上移。

**接口形状**

- ITokenStore:Unity 加密存储 / 微信 storage / Layabox LocalStorage / Godot user:// 持久文件已落地;UE 平台凭证与 Cocos localStorage 未落仓(仓内仅 MemoryTokenStore,平台持久化随接入方/引擎面提供);ITransport:各平台网络栈。

**现仓映射**:Unity——`sdks/unity/packages/com.courier.core/Runtime/Adapter`(UnityWebRequest 传输 / SecureTokenStore 加密存储 / ApplicationLifecycleMonitor 前后台 + 引擎无关的 LifecycleSignalRelay 信号归一化(批次 81 拆出,去重/成对恢复/被拒不丢挂起/未绑定 no-op 纯 dotnet 可测)/ UnityWebRequestSseSource messages SSE 流读,批次 37 落位),`tools/packcheck` 结构验收,UPM 四包(core/service/ui/diagnostics)落位;TS 三端 adapter——cocos 与 miniprogram 的 `platform/wechat`、laybox 的 `platform/laya`(wx.request / wx storage / LayaAir 可见性与 onLine 信号;微信端 sseSource.ts——wx.request enableChunked 分块流读 + 增量 UTF-8 解码,批次 39 落位;laybox 端 sseSource.ts——XHR 流读 onprogress 增量 + onload 净关流重连,批次 40 落位);UE C++ header-only 批次 14 落位(引擎集成面 HttpModule 待引擎环境);Godot `src/platform`(httpTransport——HTTPClient poll 真传输、fileTokenStore——user:// 持久令牌存储、sseStreamTransport——messages SSE 流读 chunked 分框+退避重连、lifecycleMonitor——前后台信号→状态机事件,批次 34/35/36 落位,八套 headless 全绿)。

**下一步**:UE 引擎集成面与真机联调随部署面;Godot TLS 路径真机验证待接。

## L4 Service Provider(Provider 接口 + 默认实现,服务端)

**职责**

- 定义每个能力的 Provider 接口,并提供默认实现;数据事实留在实现侧,Courier 只做投影与编排。

| Provider | 接口职责 | 默认实现(可换/可关) |
| --- | --- | --- |
| AccountProvider | 注册/登录/凭证核验 | courier-account(自建;第二实现 courier-account-alt 同契约可换) |
| AnnouncementProvider | 玩家侧公告投影 | herald(读时扫描;第二实现 herald-alt 写时索引可换) |
| SupportProvider | 工单/FAQ 投影 | croupier(读时扫描;第二实现 croupier-alt 写时属主索引+键集游标可换) |
| MessageProvider | 推送通道/会话复用 | chirp(扇出;第二实现 chirp-alt 环形日志+游标可换) |
| RealNameProvider | 实名核验(热插拔/降级链/熔断) | warden(自建核验;第二实现 warden-alt 托管核验,判定经注入 CloudVerifier;阿里云/腾讯云慧眼/易盾/Webhook 同形状可接) |
| ConfigProvider | 远程配置下发 | scribe(条件投影 + 灰度分桶;config/branding/app 同由 scribe.AppProvider 单接口覆盖;第二实现 scribe-alt 写时维度索引,同契约可换) |
| BrandingProvider | 品牌素材下发 | scribe(与 config 同管道;第二实现 scribe-alt 同管道可换) |
| PlayerProvider | 账号档案 + 账号↔角色映射 | archivist(scope 隔离;第二实现 archivist-alt 事件日志可换) |
| AssistantProvider | FAQ 检索问答(命中原样快照不生成) | sage(自建;第二实现 sage-alt 写时预计算规范化可换;转人工复用 support 域,LLM 预留默认不依赖) |
| PaymentsProvider | 订单-回调支付(服务端定价;渠道签名即认证) | teller(沙箱渠道;真实渠道按同一回调签名面接入;第二实现 teller-alt 事件账本投影,同契约可换) |
| DiagnosticsProvider | 诊断上报 | 默认关闭;客户端可选包直发自托管端点(Sentry/GlitchTip/OTLP 生态),不经网关 |

**边界**

- 不做入口治理(鉴权/限流归 L5);不做 UI;Provider 未配置 = 能力降级(`COMMON_CAPABILITY_DISABLED`),不报错、不拖垮其他能力。
- 治理规则:配置驱动路由表(`primary`+`fallbacks[]`)、降级链、HealthCheck 全 Provider 通用;运行时热重载与熔断实名域已落地(`providers/router`),其余域为预留接缝(详见 architecture.md「Provider 治理」)。

**接口形状**

- 每域一个 Provider 接口;实现 `providers.Handler` 后由装配方 `reg.Register(h)` 注册(接口与实现分离,默认实现可整体替换)。

**现仓映射**:`gateway/providers/`(接口形状批次 2 冻结;`account/` AccountProvider 批次 3;`herald/` `croupier/` `chirp/` 三域默认实现批次 6,Notify 钩子装配解耦——发布/回复事件经注入的 `Publish` 进 chirp Hub,Provider 互不 import;`router/` 治理引擎 + `warden/` 实名自建核验批次 7——降级链/熔断/热重载首个完整落地,后续域复用;`scribe/` config/app/branding 同管道 + `archivist/` 玩家档案批次 8——M3 热生效/维护门/品牌热切换 e2e 验收;`sage/` 小助手批次 9——M4 检索问答/命中率统计/转人工 e2e 验收;`teller/` 支付批次 10——M5 服务端定价/回调 HMAC 签名面/断单对账 e2e 验收;`accountalt/` 第二 identity 实现(同契约异构,HMAC 自验证令牌)批次 18——装配期读 COURIER_GATEWAY_CONFIG 随 identity primary 重指;`wardenalt/` 第二 realname 实现(托管核验,判定经注入 CloudVerifier,网关侧只存判定态)批次 64——同 wire 场景一致性 e2e(批次 18 先例同构);`archivistalt/` 第二 player 实现(事件日志模型:变更追加事件、读=投影重放)批次 67——同 wire 场景一致性 e2e(批次 18/64 先例同构);`chirpalt/` 第二 messages 实现(共享环形日志 + 每连接游标拉取,发布只追加一次)批次 68——同 wire 场景一致性 e2e(批次 18/64/67 先例同构);`heraldalt/` 第二 announcements 实现(写时索引:map 详情 O(1) + 发布时维护 publishedAt 升序切片)批次 69——同 wire 场景一致性 e2e(批次 18/64/67/68 先例同构);`sagealt/` 第二 assistant 实现(写时预计算规范化文本:AddEntry 时规范化一次,查询零重复计算)批次 70——同 wire 场景一致性 e2e(批次 18/64/67/68/69 先例同构);`scribealt/` 第二 app 实现(写时维度索引:Publish 时条件切片转维度→允许值索引,投影按请求维度分阶段过滤)批次 71——同 wire 场景一致性 e2e(批次 18/64/67/68/69/70 先例同构);维护门回调跟随 app 域 primary 重指(同推送源先例);`telleralt/` 第二 payments 实现(追加式事件账本:状态转移追加不可变事件,status/时间戳=投影重放,历史不可变)批次 72——同 wire 场景一致性 e2e(批次 18/64/67/68/69/70/71 先例同构);`croupieralt/` 第二 support 实现(写时属主有序索引:建单/追加/回复/关单/附件登记即时重排,游标=`<毫秒>|<工单ID>` 键集锚点,读时零扫描零排序,并发插入不漂移)批次 73——同 wire 场景一致性 e2e + 跨实现分页等价显式证明(批次 18/64/67/68/69/70/71/72 先例同构))。

**下一步**:Diagnostics 为客户端可选包(不经网关,批次 8 落 `sdks/unity/packages/com.courier.diagnostics`);payments 批次 10 已接入(teller)。

## L5 Gateway(入口治理层,服务端)

**职责**

- 唯一入口:auth / session / scope / routing / aggregation / middleware(限流、trace、恢复、scope、维护门);审计依赖 trace 贯穿,实名域已落结构化审计事件面(`gateway/audit` 共享接缝:两个 realname 实现同注入 `AuditSink`,nil = 不产出;`COURIER_REALNAME_AUDIT=log` 开日志半边),落库随部署面。
- Provider 注册表管理:已知能力域的路由全量挂载;未配置能力的路由在分发期返回 `COMMON_CAPABILITY_DISABLED`(501),非路由缺位。

**边界**

- 不含业务目录(`accounts/`、`announcements/` 等名字不允许出现在 gateway 一级);中间件只处理横切面,不认识业务字段;无管理接口。

**接口形状**

```text
gateway/
├── cmd/gateway/
├── auth/  ├── session/  ├── scope/
├── routing/  ├── aggregation/  ├── middleware/
└── providers/
```

Go `http.ServeMux` + 中间件链(trace → 结构化错误 → 限流 → scope,冻结顺序;认证按能力在路由内挂载 Bearer 校验);`/healthz`;`/v1/{domain}/*`。

**现仓映射**:`gateway/{cmd,routing,middleware,scope,aggregation}`(批次 2 骨架)+ `auth`/`session`(批次 3 实装,Bearer→身份注入/会话验证)+ `e2e`(批次 6,M2 全链路;批次 8 增 M3 热生效/维护/品牌,批次 9 增 M4 检索问答/转人工,批次 10 增 M5 下单/回调/对账 e2e);能力驱动路由 `/v1/{capability}/` 随 Provider 注册自动挂载,未配置 = 501 降级;维护门 `middleware.Maintenance` 包入口(批次 8,app.md 白名单内建:维护开启 `/v1/*` 除 `/v1/app/*` 与 `/healthz` 一律 503);降级语义与中间件链已有测试覆盖。

**下一步**:持久化存储与多实例限流协同。

## 依赖规则

1. **只允许向下依赖**:L3 → L2 → L1;L5 → L4 → L1;禁止逆向。
2. **契约不感知供应商**:L1 提能力不提实现者;默认供应商只出现在 L4 的实现与配置里。
3. **内核不碰平台**:L2 面向接口注入;L4 只经 L5 中间件拿身份。
4. **契约先行**:L2–L5 的公共类型必须能在 L1 找到定义;各端不得私造字段。
5. **差异各归其位**:平台差异只在 L3;供应商差异只在 L4;L1/L2 跨端一致。
6. **红线内建**:默认关闭、开启明示数据范围是 L1 契约属性,L3–L5 不得绕过。
7. **UI 可选**:UI 包与 Diagnostics 包不进 Core 包;Core 无 UI、无品牌逻辑。

## 与现仓结构映射

| 层 | 现仓落点 | 状态 |
| --- | --- | --- |
| L1 SDK Contract | `docs/contract/` + `tools/contractgen` | 基元五件 Frozen v1 + auth v1.1 + M2 三域 v1.1 + realname v1 + M3 五域(config/app/branding/player/diagnostics)v1 + assistant(M4)v1 + payment(M5)v1.1 + oauth v1(批次 52);错误码 v8 |
| L2 Core | `sdks/unity/packages/com.courier.core/Runtime/{Core,Identity,Session}`;`sdks/cocos/src/core`;`sdks/ue/core` | Unity 批次 4 落位(状态机+auth 域,48 用例;批次 37 SseStream 接缝+退避);cocos TS 批次 12-13 落位(core+service 全域);批次 38 messages 流客户端落位(TS 三端 node:test 111 例,真 chunked 服务器 e2e);批次 39 增量 UTF-8 解码器落位(跨块多字节不撕裂,node:test 123 例);UE C++ 批次 14 落位(core+service 全域,纯 ISO C++17 header-only);批次 38 流编排落位(g++ 七套全绿) |
| L3 Platform Adapter | `sdks/unity/packages/com.courier.core/Runtime/Adapter`(+UPM 包;`tools/packcheck`);TS 三端 adapter(`sdks/cocos`、`sdks/miniprogram` 的 platform/wechat,`sdks/laybox` 的 platform/laya) | Unity 批次 5 落位(传输/安全存储/前后台);批次 6 service 域服务+SSE 解析、ui 面板;批次 8 App/Config/Branding/Player 域服务 + diagnostics 可选包(默认全关 no-op);批次 9 Assistant 域服务 + 助手面板(转人工);批次 10 Payment 域服务(下单/轮询);批次 37 SSE 流读接线(`UnityWebRequestSseSource`,编排退避/重连/解析在 Core+Service 纯逻辑,dotnet 77 例 [Fact] 覆盖——SSE 专属仅 16 例,引擎内 Test Runner 实跑随部署面);TS 三端批次 12-17 落位(wechat/laya 绑定;ui 包批次 17、diagnostics 包批次 20);批次 38 TS 三端 FetchSseSource(fetch 流读默认实现)+ UE SseSource 接缝落位;批次 39 微信 enableChunked 流源落位(cocos/miniprogram,头到回填连接 + 手写 UTF-8 解码);批次 40 laya XHR 流源落位(onprogress 增量 + onload 净关流重连;UE 引擎接线随引擎环境);ui 包批次 17、diagnostics 包批次 20 落位(cocos 首落,批次 41 三端对齐——TS 三端 node:test 各 123 例);Godot 批次 34/35/36 落位(HTTPClient 传输 + user:// 存储 + messages SSE 流读 + 前后台信号,真 socket 验证) |
| L4 Service Provider | `gateway/providers/*` | 接口形状批次 2 冻结;account 批次 3;herald/croupier/chirp 批次 6(M2 三域,e2e 验收);router 治理引擎 + warden 实名批次 7;scribe + archivist 批次 8(M3,e2e 验收);sage 批次 9(M4,e2e 验收);teller 批次 10(M5,e2e 验收);accountalt 第二 identity 实现(同契约异构)批次 18;wardenalt 第二 realname 实现(托管核验)批次 64;archivistalt 第二 player 实现(事件日志)批次 67;chirpalt 第二 messages 实现(环形日志+游标)批次 68;heraldalt 第二 announcements 实现(写时索引)批次 69;sagealt 第二 assistant 实现(写时预计算规范化)批次 70;scribealt 第二 app 实现(写时维度索引)批次 71;telleralt 第二 payments 实现(事件账本投影)批次 72;croupieralt 第二 support 实现(写时属主索引 + 键集游标)批次 73 |
| L5 Gateway | `gateway/{cmd,routing,middleware,scope,aggregation,auth,session,e2e}` | 批次 2 骨架;auth/session 批次 3;能力驱动路由+SSE 通道+e2e 批次 6;维护门批次 8 |

## 演进原则

- 每层先立「接口形状 + 一个最小实现」再横向铺;不做半个功能。
- 分批节奏与验收见 [todo](./todo.md);业务里程碑见 [roadmap](./roadmap.md)。
