# 五层架构:L1–L5

> **状态**:Draft。原则:**不堆业务,先立骨架**;总纲见 [architecture.md](./architecture.md)(设计原则 / Provider 原则 / 能力树 / 生命周期),契约宪法见 [contract/index.md](./contract/index.md),分批节奏见 [todo.md](./todo.md)。

## 分层总览

```text
 游戏客户端:Unity / Unreal / Cocos / Godot / 小程序 / Layabox
 ───────────────────────── 客户端侧 ─────────────────────────
 L1 SDK Contract      语言无关契约:DTO、错误模型、scope、事件、生命周期(唯一事实源)
 L2 Core              平台无关内核:状态机、token 存储接口、重试、序列化
 L3 Platform Adapter  平台绑定:原生网络栈、安全存储、生命周期、打包形态(+可选 UI 包)
 ───────────────────────── 线 路 ───────────────────────────
                      HTTPS JSON(WebSocket 推送 M2+,通道为 Provider 能力)
 ───────────────────────── 服务端 ───────────────────────────
 L5 Gateway           唯一入口:auth / session / scope / routing / aggregation / middleware
 L4 Service Provider  Provider 接口 + 默认实现(默认供应商可换可关)
 ───────────────────────── 生 态 ────────────────────────────
   默认供应商(仅以 Provider 角色存在):herald(公告) croupier(客服) chirp(推送) oddsmaker(风控)
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

- 基元:primitives / errors / scope / versioning / events;域:auth(Frozen v1)、realname/branding/diagnostics(初稿已立)、announcements/support/config/payment(各里程碑前冻结)。

**现仓映射**:`docs/contract/`(本仓)+ `docs/contract/fixtures/` + `tools/contractgen`(契约测试骨架:fixture 生成六端契约源,漂移即测试红)。

**下一步**:基元五件与 auth 已冻结;announcements / support / config / payment 随各里程碑开工前冻结;realname / branding / diagnostics 实现前冻结。

## L2 Core(平台无关内核)

**职责**

- 可单测纯逻辑:DTO 解析与校验、**生命周期状态机**(Uninitialized→…→PlayerReady、Suspended/Resuming)、token 存储接口、transport 接口、重试与超时、错误归一(错误码→本端枚举)。
- `CourierClient` 门面按能力树组域:Identity / Session / Player / App / Communication / Support / Payment / RealName / Diagnostics 接入点。

**边界**

- 不 import 任何平台 API(ITransport / ITokenStore / IClock 注入);不写 UI;**不含品牌逻辑**;不感知 Provider 实现。

**接口形状**

- `CourierClient.Init({ gameId, env, endpoint })` → 按域子模块;域接口与 L1 一一对应。

**现仓映射**:`sdks/unity/packages/com.courier.core/Runtime`(Core/ 状态机与 API 客户端、Identity/ Session/ auth 域、CourierClient.cs 门面、Core/Contract/ 生成物);`CoreTests~`(48 用例,xunit);`sdks/cocos src/core`(规划);其余端同构。

**下一步**:M1 Unity 已就位;Identity/Session 之外域(Player/App/…)随 M2+ 按需扩。

## L3 Platform Adapter(平台绑定层)

**职责**

- 平台翻译四件事:网络(原生 HTTP/WebSocket)、存储(ITokenStore 安全存储实现)、生命周期(前后台/进程信号→状态机事件)、线程(主线程回调调度);打包形态(UPM / UE 插件 / npm / Godot 插件)。
- **可选 UI 包挂在平台侧**:CourierLoginPanel / AnnouncementPanel / SupportPanel,消费 Branding 下发(见 L2 边界:Core 无品牌逻辑)。

**边界**

- **禁带业务**:Adapter 不出现任何业务语义;业务逻辑要么上移 Core/Service,要么属 UI 包。
- 每平台一个适配器;平台特有扩展以 Core 接口为基类;跨平台共享逻辑上移。

**接口形状**

- ITokenStore:Unity 加密存储 / UE 平台凭证 / Cocos localStorage / 微信 storage / Layabox、Godot 平台存储;ITransport:各平台网络栈。

**现仓映射**:Unity 先行——`sdks/unity/packages/com.courier.core/Runtime/Adapter`(UnityWebRequest 传输 / SecureTokenStore 加密存储 / ApplicationLifecycleMonitor 前后台),`tools/packcheck` 结构验收;UPM 三包(core/service/ui)骨架就位;其余五端规划。

**下一步**:M1 Unity UPM 骨架(Core/Service/UI 三包目录就位,UI 包可空)。

## L4 Service Provider(Provider 接口 + 默认实现,服务端)

**职责**

- 定义每个能力的 Provider 接口,并提供默认实现;数据事实留在实现侧,Courier 只做投影与编排。

| Provider | 接口职责 | 默认实现(可换/可关) |
| --- | --- | --- |
| AccountProvider | 注册/登录/凭证核验 | 自建 accounts/sessions |
| AnnouncementProvider | 玩家侧公告投影 | herald |
| SupportProvider | 工单/FAQ 投影 | croupier |
| MessageProvider | 推送通道/会话复用 | chirp |
| RiskProvider | 登录/支付风控前置 | oddsmaker |
| RealNameProvider | 实名核验(热插拔/降级链/熔断) | 默认关闭;自建/阿里云/腾讯云慧眼/易盾/Webhook |
| ConfigProvider | 远程配置下发 | 自建(可由 croupier 发布) |
| BrandingProvider | 品牌素材下发 | 自建(与 config 同管道) |
| DiagnosticsProvider | 诊断上报 | 默认关闭;Sentry/GlitchTip/OTLP |

**边界**

- 不做入口治理(鉴权/限流归 L5);不做 UI;Provider 未配置 = 能力降级(`COMMON_CAPABILITY_DISABLED`),不报错、不拖垮其他能力。
- 治理规则全 Provider 通用:配置驱动路由表(`primary`+`fallbacks[]`)、运行时热重载、降级链、熔断、HealthCheck(详见 architecture.md「Provider 治理」)。

**接口形状**

- 每域一个 Provider 接口 + `Register(registry)` 注册;接口与实现分离,默认实现可整体替换。

**现仓映射**:`gateway/providers/`(接口形状批次 2 冻结;`account/` AccountProvider 批次 3;`herald/` `croupier/` `chirp/` 三域默认实现批次 6,Notify 钩子装配解耦——发布/回复事件经注入的 `Publish` 进 chirp Hub,Provider 互不 import)。

**下一步**:RealName 热插拔(降级链/熔断,批次 7);Config/Branding/Diagnostics 随 M3 接入(批次 8)。

## L5 Gateway(入口治理层,服务端)

**职责**

- 唯一入口:auth / session / scope / routing / aggregation / middleware(限流、trace、审计、恢复)。
- Provider 注册表管理:路由按「已配置的 Provider」动态挂载;未配置能力的路由不注册。

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

Go `http.ServeMux` + 中间件链(auth → rate limit → scope → audit);`/healthz`;`/v1/{domain}/*`。

**现仓映射**:`gateway/{cmd,routing,middleware,scope,aggregation}`(批次 2 骨架)+ `auth`/`session`(批次 3 实装,Bearer→身份注入/会话验证)+ `e2e`(批次 6,M2 全链路验收);能力驱动路由 `/v1/{capability}/` 随 Provider 注册自动挂载,未配置 = 501 降级;降级语义与中间件链已有测试覆盖。

**下一步**:持久化存储与多实例限流协同;路由表热重载(批次 7)。

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
| L1 SDK Contract | `docs/contract/` + `tools/contractgen` | 基元五件 Frozen v1 + auth v1 + M2 三域(announcement/support/messages)v1;错误码 v2(26 码);realname/branding/diagnostics 初稿实现前冻结 |
| L2 Core | `sdks/unity/packages/com.courier.core/Runtime/{Core,Identity,Session}` | Unity 批次 4 落位(状态机+auth 域,48 用例);其余端规划 |
| L3 Platform Adapter | `sdks/unity/packages/com.courier.core/Runtime/Adapter`(+UPM 三包;`tools/packcheck`) | Unity 批次 5 落位(传输/安全存储/前后台);批次 6 service 包域服务+SSE 解析(17 用例)、ui 包双面板;SSE 流式传输留引擎卡点 |
| L4 Service Provider | `gateway/providers/*` | 接口形状批次 2 冻结;account 批次 3;herald/croupier/chirp 批次 6(M2 三域,e2e 验收) |
| L5 Gateway | `gateway/{cmd,routing,middleware,scope,aggregation,auth,session,e2e}` | 批次 2 骨架;auth/session 批次 3;能力驱动路由+SSE 通道+e2e 批次 6 |

## 演进原则

- 每层先立「接口形状 + 一个最小实现」再横向铺;不做半个功能。
- 分批节奏与验收见 [todo](./todo.md);业务里程碑见 [roadmap](./roadmap.md)。
