<p align="center"><img src="docs/public/logo.svg" width="64" height="64" alt="logo" /></p>

<h1 align="center">Courier — Universal Game SDK</h1>

<p align="center">
  <img src="https://img.shields.io/badge/TypeScript-3178C6?logo=typescript&logoColor=white" alt="TypeScript" />
  <img src="https://img.shields.io/badge/Go-00ADD8?logo=go&logoColor=white" alt="Go" />
  <img src="https://img.shields.io/badge/Node.js-339933?logo=nodedotjs&logoColor=white" alt="Node.js" />
  <img src="https://img.shields.io/badge/license-Apache%202.0-blue.svg" alt="License: Apache-2.0" />
</p>

<p align="center">[English](README.md) | [中文](README.zh.md)</p>

> **A universal SDK for integrating common game services across engines and platforms.**
>
> Courier 是一个面向游戏的通用 SDK,为 Unity、Unreal、Cocos、Godot 等游戏引擎提供统一的账号、认证、玩家、消息、公告、客服、支付、实名、远程配置、品牌与诊断等能力接口。

Courier 是信使:它不是某一种具体业务,而是替游戏把各种服务能力送到客户端。

```text
 各服务能力(Provider):账号 · 公告 · 客服 · 推送 · 支付 · 实名 · 配置 · 品牌 · 诊断
                             │
                             ▼ 取件
                       Courier(信使)
                             │
                             ▼ 送达
                       Game Client
        Unity / Unreal / Cocos / Godot / 小程序 / Layabox
```

## 场景

一家游戏公司运营着多款游戏(MMO、卡牌、塔防……),过去每款游戏各自建账号、公告、客服、支付系统,玩家在每款游戏里都要重新注册,运营团队在每个后台之间来回切换。

Courier 解决这个问题:

```text
玩家视角:
  注册一个账号 → 登录任意一款游戏 → 按游戏接入支付 → 统一客服

运营视角:
  一套契约覆盖所有游戏的公共能力 → 各游戏按需选装 → 数据互通
```

## 设计原则:SDK Contract > Everything

契约是宪法;Gateway、Provider、后端服务都是契约的**实现者**。新增引擎 = 新增 Adapter,而不是重新设计 SDK。

**Provider 原则:默认提供,可换可关。** 每个能力的后端都是可插拔 Provider——Courier 默认提供一套开箱即用的实现,但接不接、用哪家、换不换由接入方配置决定;不硬绑定、不强制依赖,未接入 = 能力降级而非报错。

## 架构:五层

```text
SDK Contract      各端 SDK 的公共契约(类型、错误码、协议),跨端一致,M0 立宪
Core             引擎无关的核心逻辑(生命周期状态机、会话、缓存、重试、序列化)
Platform Adapter 平台适配层(Unity/Unreal/Cocos/Godot/小程序:生命周期、存储、网络;禁带业务)
Service Provider 服务提供者接口 + 默认实现(可插拔:自建/第三方/关闭)
Gateway          玩家 API 网关(Go:auth、session、scope、routing、aggregation、middleware、providers)
```

分层设计见 [docs/layers.md](docs/layers.md);总体架构、能力树、SDK 生命周期、Provider 治理见 [docs/architecture.md](docs/architecture.md);**契约宪法见 [docs/contract/](docs/contract/index.md)**;分批落地见 [docs/todo.md](docs/todo.md)。

## 定位

Courier 不是又一套独立后端。它是**玩家端 SDK + 轻量 API 网关**,后端能力一律以 Provider 接口接入:

```text
Game Client (Unity / UE / Cocos / 小程序 / Layabox / Godot)
  -> Courier SDK (C# / C++ / GDScript / TypeScript / JavaScript)
  -> Courier Gateway (玩家 API 网关:auth、session、scope、路由、聚合)
  -> 后端 Provider(默认提供,可换可关):
     - AccountProvider       默认:courier-account(自建 accounts/sessions;
                             第二实现 courier-account-alt 同契约可换)
     - AnnouncementProvider  默认:herald(事件驱动通知投递)
     - SupportProvider       默认:croupier support / ticket / faq
     - MessageProvider       默认:chirp(gateway + session)
     - RealNameProvider      默认:warden(自建核验;阿里云/腾讯云慧眼/易盾/Webhook 同形状可接)
     - ConfigProvider        默认:scribe(远程配置,自建管道)
     - BrandingProvider      默认:scribe(与 Config 同管道)
     - PlayerProvider        默认:archivist(档案 + 账号↔角色映射)
     - AssistantProvider     默认:sage(FAQ 检索问答)
     - PaymentsProvider      默认:teller(订单-回调-发货)
     - DiagnosticsProvider   直发接入方端点不经网关(契约红线;Sentry/GlitchTip/OTLP 可接)
```

默认供应商只是开箱即用的便利,不是绑定:任何一项都可配置为自建 HTTP 端点、第三方服务,或直接关闭(客户端安全地隐藏对应能力)。

### 账号模型

```text
一个公司账号(account)
  ├── 游戏A:角色1(player_id_A)
  ├── 游戏B:角色2(player_id_B)
  └── 游戏C:角色3(player_id_C)
```

- **一个账号登录所有游戏**:玩家注册一次,凭同一套凭证进入公司旗下任意游戏。
- **每个游戏角色独立**:同一账号在不同游戏中有独立的角色数据、背包、等级。
- **支付形态开放**:契约只定 Order/Purchase/Receipt,账号钱包/游戏钱包/直购/外部支付由各游戏选择,不做统一钱包假设。
- **统一客服**:工单/FAQ 按 game_id 隔离,同一账号的历史工单可跨游戏查看。

scope 模型:`game_id + env` 全局隔离,初始化指定一次,URL 与 payload 不得携带(见 [契约 scope.md](docs/contract/scope.md))。

## 能力树

```text
Courier
├── Core          Lifecycle / Network / Error / Retry / Cache / Telemetry 挂钩
├── Identity      Login / Logout / Bind / Device / RealName(可选,合规)
├── Session       AccessToken / RefreshToken / Session
├── Player        Profile / Game Player
├── App           Version / Maintenance / Remote Config / Branding
├── Communication Announcement / Message / Push
├── Support       FAQ / Ticket(核心)+ Panel(可选 UI 包)
├── Payment       Order / Purchase / Receipt(仅契约,四形态可选)
├── Diagnostics   Crash / Error Tracking / OTel Trace / Perf / Analytics(可选包)
└── Platform      Unity / Unreal / Cocos / Godot / Layabox / MiniProgram
```

SDK 分包:**Core(必选)/ Service(按需)/ UI(可选包)**——`CreateTicket()` 在核心,`CourierSupportPanel` 在可选 UI 包;Diagnostics 同为可选包,默认全关,未配置即 no-op 零开销。

| 能力域 | 说明 | 默认 Provider(可换/可关) | 里程碑 |
| --- | --- | --- | --- |
| Identity / Session | 注册/登录(邮箱+密码、游客设备)、token 轮换、设备绑定;一个账号登录所有游戏 | 自建 accounts/sessions | M1 |
| 公告 | 拉取/订阅公告、活动、维护通知(按游戏隔离) | herald + croupier message | M2 |
| 客服 | 工单提交/查询、FAQ 检索(账号维度可跨游戏) | croupier support/ticket/faq | M2 |
| 实名(可选) | 实名核验 + 防沉迷钩子接口位;Provider 热插拔(降级链+熔断);默认关闭,开启明示数据范围 | 自建/阿里云/腾讯云慧眼/易盾/Webhook | M2 后段 |
| Remote Config | 按 game_id/env/platform/version/region 条件匹配,热生效 | 自建(可由 croupier 发布) | M3 |
| Player Profile | 账号↔角色映射与档案接口,角色数据归各游戏 | 自建(轻量)+ 各游戏服 | M3 |
| App | GetVersion / CheckUpdate / CheckMaintenance / GetEnvironment | 自建 | M3 |
| Branding | 公司名/logo/主题色/关于页/客服入口,按 game_id+env 下发;UI 包消费,Core 无品牌逻辑,默认 Courier 标兜底 | 自建(与 Config 同管道) | M3 |
| Diagnostics(可选) | 崩溃/错误追踪、OTel Trace、性能指标、埋点;默认全关 | Sentry/GlitchTip/自托管 OTLP | M3+ |
| Assistant | FAQ 机器人、游戏内向导 | 可接 croupier faq | M4 |
| Payment | 仅契约:Order/Purchase/Receipt;四形态由游戏选择 | 渠道抽象 + RiskProvider 前置 | M5 |

## 仓库结构

```text
gateway/    Courier API 网关(Go):auth、session、scope、routing、aggregation、middleware、providers
sdks/
  unity/    Unity SDK(C#,UPM 包)
  ue/       Unreal Engine SDK(C++ 插件)
  cocos/    Cocos Creator SDK(TypeScript)
  miniprogram/  微信小程序 SDK(JavaScript)
  laybox/    Layabox SDK(TypeScript/JavaScript)
  godot/     Godot SDK(GDScript/C#)
docs/       契约(contract/)、架构、五层设计、竞品调研(research/)、路线图、TODO
```

## 设计边界

1. **SDK 只面向玩家**:不包含任何运营/管理能力;运营能力不在本 SDK 范围(默认供应商 Croupier,可自建)。
2. **网关是唯一入口**:客户端不直连任何后端 Provider;网关做鉴权、限流、聚合和风控前置。
3. **Provider 只进 providers/**:Gateway 不含业务目录,业务一律经 Provider 接口接入;未接入 = 能力降级(`COMMON_CAPABILITY_DISABLED`),不是报错。
4. **一个账号多款游戏**:账号是跨游戏的最高层实体;角色(player)是游戏维度的,每个游戏独立创建。
5. **支付只定契约**:四形态开放,统一钱包假设不进契约;沙箱渠道全链路先行,回调只认渠道签名。
6. **采集/合规默认关闭**:Diagnostics 与 RealName 的红线内建于契约——默认关闭、显式开启、开启明示数据范围。
7. **UI 永远可选**:Core/Service/UI 三包分离;Core 无 UI、无品牌逻辑。
8. **无 any/unknown 敷衍**:各端 SDK 公共类型以契约对齐,禁止各自发明字段。
9. **实时与渠道不做**:实时多人/匹配(Nakama/Agones 地盘)与渠道聚合联运(MSDK/QuickSDK 地盘)不在范围内,详见[竞品调研](docs/research/competitive.md)。

## 文档索引

- 契约宪法(M0):[docs/contract/](docs/contract/index.md) — 基元/错误/Scope/版本/事件基元契约 + 全部已冻结域契约:认证(M1)、公告/客服/推送(M2)、实名(M2)、配置/应用/品牌/档案/诊断(M3)、助手(M4)、支付(M5);诊断为客户端可选包,直发不经网关
- 架构:[docs/architecture.md](docs/architecture.md) · 五层:[docs/layers.md](docs/layers.md)
- 竞品调研:[docs/research/competitive.md](docs/research/competitive.md)
- 路线图:[docs/roadmap.md](docs/roadmap.md) · 分批 TODO:[docs/todo.md](docs/todo.md)

## 开发

本地门禁(commit 前全绿):

```text
go test ./...                      # 根模块:contractgen + packcheck
(cd gateway && go test ./...)      # gateway 模块:全部 Provider + e2e
(cd gateway && go vet ./...)       # copylocks 等(go test 默认 vet 子集之外)
node --test tests/*.test.ts        # 在 sdks/cocos、sdks/miniprogram、sdks/laybox 各自目录
sh sdks/ue/run_tests.sh            # UE 无引擎独立编译(ISO C++17)
```

Unity 测试在 Unity Test Runner 跑(需引擎本体)。文档站:`npm install && npm run docs:build`(vitepress)。CI 每次 push/PR 跑同一套门禁:[.github/workflows/tests.yml](.github/workflows/tests.yml)。

## License

Apache-2.0
