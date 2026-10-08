# Courier 架构

> **状态**:Draft — M0 契约冻结前为 Draft。分层细化(SDK Contract / Core / Platform Adapter / Service Provider / Gateway)见 [layers.md](./layers.md),分批落地见 [todo.md](./todo.md),契约宪法见 [contract/](./contract/index.md),竞品定位见 [research/competitive.md](./research/competitive.md)。

## 设计原则:SDK Contract > Everything

Courier 的核心是契约,不是某个后端:

```text
                 Courier
                    │
          ┌─────────▼─────────┐
          │   SDK Contract    │
          │  Auth / Session   │
          │  Player / App     │
          │  Announcement     │
          │  Support / Pay    │
          │  Message / …      │
          └─────────┬─────────┘
           ┌────────┼────────┐
           ▼        ▼        ▼
         Unity      UE      Cocos …
```

Gateway、Provider、生态后端全部是**契约的实现者 / 后端提供者**。新增引擎 = 新增 Adapter;新增服务 = 新增 Provider;契约本身稳定。

## Provider 原则:默认提供,可换可关

生态项目只能以「供应商(Provider)」角色存在:

1. **每个能力的后端 = 可插拔 Provider**,统一接口、配置驱动。
2. **Courier 默认提供一套开箱即用的实现**:公告默认 herald、客服默认 croupier、推送默认 chirp、风控默认 oddsmaker、账号默认自建 accounts——默认供应商可以打包,但**接不接、用哪家、换不换由接入方配置决定,SDK 不替用户做选择**。
3. **不硬绑定、不强制依赖**:任何 Provider 都可替换为自建实现、第三方服务,或直接关掉;关掉 = 能力降级(`COMMON_CAPABILITY_DISABLED`),不是报错。
4. **示例**:公告能力默认走 herald Provider;接入方可配置成自己的 HTTP 端点,或禁用公告(客户端安全地隐藏对应 UI)。

## 总体形态

```text
┌───────────────────────────────────────────────┐
│ Game Client                                   │
│   Courier SDK:Core / Service / UI(可选包)     │
└──────────────┬────────────────────────────────┘
               │ HTTPS JSON (+ WebSocket 推送, M2+)
┌──────────────▼────────────────────────────────┐
│ Courier Gateway(Go):auth · session · scope    │
│   · routing · aggregation · middleware        │
│   · providers(Provider 注册与治理)             │
└──────┬─────────┬─────────┬─────────┬──────────┘
       ▼         ▼         ▼         ▼
  AccountProvider(自建 accounts)…均经 Provider 接口
       ▼
  默认供应商:herald(公告)· croupier(客服)· chirp(推送)· oddsmaker(风控)
  —— 以上全部可被接入方替换或关闭
```

## 能力树

```text
Courier
├── Core          Lifecycle / Network / Error / Retry / Cache / Telemetry 挂钩
├── Identity      Login / Logout / Bind / Device / RealName(可选,合规)
├── Session       AccessToken / RefreshToken / Session
├── Player        Profile / Game Player
├── App           Version / Maintenance / Remote Config / Branding
├── Communication Announcement / Message / Push
├── Support       FAQ / Ticket(核心);Panel(可选 UI 包)
├── Payment       Order / Purchase / Receipt(仅契约,四形态可选)
├── Diagnostics   Crash / Error Tracking / OTel Trace / Perf / Analytics(可选包)
└── Platform      Unity / Unreal / Cocos / Godot / Layabox / MiniProgram
```

## SDK 生命周期状态机

游戏 SDK 与普通 REST SDK 的最大区别:生命周期是一等公民。状态机定义进 L1 契约(事件面见 [contract/events.md](./contract/events.md)),实现进 L2 Core:

```text
Uninitialized → Initializing → Ready → Authenticating → Authenticated → PlayerReady
                                   ▲                        │              │
                                   │        ┌───────────────┘              │
                                   │        ▼                              ▼
                                   └── SignedOut                Suspended ⇄ Resuming
                                                              (切后台/断网 → 恢复)
```

统一处理:游戏启动、切后台、恢复、token 过期(自动 refresh)、网络断开与恢复、切账号、退出登录。平台差异(前台/后台信号、进程存活)由 Adapter 注入,状态机本身平台无关。

## SDK 分包:Core / Service / UI

| 包 | 内容 | 必选? |
| --- | --- | --- |
| Core 包 | 内核 + Identity / Session(初始化、生命周期、token) | 必选 |
| Service 包 | 各能力服务面:`Support.CreateTicket()`、`Communication.Announcements.list()` 等——**接口在核心/服务包** | 按需 |
| UI 包 | `CourierLoginPanel` / `CourierAnnouncementPanel` / `CourierSupportPanel` 等组件,**消费 Branding 配置** | 可选 |

- `Support.CreateTicket()` 属核心/服务包;`CourierSupportPanel` 属可选 UI 包——SDK 不强制任何 UI。
- 各引擎各取所需:Unity 有 UPM UI 包,UE 走 UMG,Cocos 自绘;UI 包缺席不影响功能面。
- Diagnostics 同为可选包,与 UI 包同级。

## Gateway:入口治理,不含业务

Gateway 只做入口治理,业务一律经 Provider 接口接入,**不留业务目录**:

```text
gateway/
├── cmd/gateway/   入口
├── auth/          鉴权中间件(token 校验)
├── session/       会话管理
├── scope/         game_id + env 注入与校验
├── routing/       路由与 Provider 注册表
├── aggregation/   跨 Provider 聚合
├── middleware/    限流 / trace / 审计 / 恢复
└── providers/     Provider 接口 + 默认实现
    ├── account/        AccountProvider        默认:自建 accounts/sessions
    ├── announcement/   AnnouncementProvider   默认:herald
    ├── support/        SupportProvider        默认:croupier support/ticket/faq
    ├── message/        MessageProvider        默认:chirp
    ├── risk/           RiskProvider           默认:oddsmaker
    ├── realname/       RealNameProvider       默认:关闭(可接自建/阿里云/腾讯云/易盾/Webhook)
    ├── config/         ConfigProvider         Remote Config,默认自建(可由 croupier 发布)
    ├── branding/       BrandingProvider       默认自建(与 config 同管道)
    └── diagnostics/    DiagnosticsProvider    默认:关闭
```

- 未配置的 Provider:对应路由不注册,客户端得到 `COMMON_CAPABILITY_DISABLED`(见 [contract/errors.md](./contract/errors.md))。
- `accounts` / `announcements` 这类名字不得作为 gateway 一级业务目录出现——它们是 Provider 的默认实现,住在 `providers/` 下。

## Provider 治理:热插拔 / 降级链 / 熔断

- **配置驱动**:每个 Provider 由配置声明 `primary` + `fallbacks[]`;运行时配置重载即生效,不重启(路由表原子替换,在途请求按旧表完成)。
- **降级链**:主 Provider 失败/超时 → 依序 fallback;全部失败按域定义安全态(如 RealName → `PENDING_REVIEW`)。
- **熔断**:连续 N 次失败开路 T 秒,半开探测恢复;熔断期直接走 fallback。
- **HealthCheck**:注册表剔除不健康实例。
- RealName([contract/realname.md](./contract/realname.md))是第一个完整适用方;规则对所有 Provider 通用。

## 新能力设计

### Remote Config(M3)

- SDK:`Courier.Config.Get("login.enable_guest")`。
- 匹配维度:`game_id / environment / platform / version / region`——配置按条件命中。
- 链路:接入方或默认供应商(如 croupier)发布 → ConfigProvider → Gateway 缓存 → SDK(本地缓存 + `config.updated` 推送热生效)。

### Courier.App(M3)

- `GetVersion / CheckUpdate / CheckMaintenance / GetEnvironment`。
- 典型启动序列:Game Start → Initialize → CheckMaintenance → CheckVersion → Auth → Player → Game。

### Branding(M3)

品牌定制:公司名/logo/icon/主题色/关于页/客服入口,按 `game_id+env` 下发,UI 可选包消费,Core 不含品牌逻辑。见 [contract/branding.md](./contract/branding.md)。

### RealName(M2 后段,合规)

实名认证与防沉迷钩子接口位;Provider 热插拔(自建/阿里云/腾讯云慧眼/易盾/Webhook),默认关闭。见 [contract/realname.md](./contract/realname.md)。

### Diagnostics(M3 同期或后,可选)

崩溃/错误追踪(Sentry/GlitchTip 可自托管)、OTel Trace(官方 OTel SDK → 任意 OTLP collector)、性能指标、事件分析;默认全关,未配置即 no-op 零开销。见 [contract/diagnostics.md](./contract/diagnostics.md)。

### Payment:只定 Contract(M5)

支付形态因游戏而异,Contract 不设「统一钱包」假设,允许:

```text
Payment
├── Account Wallet     账号维度钱包(余额跨游戏共享)
├── Game Wallet        游戏维度钱包
├── Direct Purchase    直购
└── External Payment   外部支付(渠道 IAP 等)
```

契约只冻结 `Order / Purchase / Receipt` 与安全边界(回调只认渠道签名);选哪种形态由各游戏配置决定。接口形状(`Order / Channel / Callback / Reconcile`)仍提前冻结避免返工。

## 关键决策

1. **契约是宪法。** M0 立宪并冻结([contract/](./contract/index.md));M1 Auth 开工的前提。SDK Contract > Everything。
2. **网关是唯一入口。** 客户端只和 Courier Gateway 通信;Provider 与后端拓扑对客户端不可见。
3. **协议 HTTPS JSON 优先。** 推送 M2 起走 WebSocket,复用默认推送 Provider(chirp)的会话通道,且通道本身可替换。
4. **scope 唯一。** `game_id + env` 初始化指定一次,header 注入,见 [contract/scope.md](./contract/scope.md)。
5. **Gateway 不含业务目录。** 业务一律 Provider 接口接入;`providers/` 下放接口与默认实现。
6. **默认提供,可换可关。** 生态项目只是默认供应商;未接入 = 能力降级而非报错。
7. **采集/合规默认关闭。** Diagnostics 与 RealName 的红线内建于契约。
8. **Payment 只定契约。** 四形态开放,钱包假设不进契约。
9. **UI 永远可选。** Core/Service/UI 三包分离;Core 无 UI、无品牌逻辑。
10. **账号 ≠ 玩家。** account 公司级,player 游戏级,永不混用。

## SDK 结构(多端同构)

```text
CourierClient(门面)
  .Identity        login / logout / bind / device
  .Session         token 状态与轮换
  .Player          profile / gamePlayer
  .App             version / checkUpdate / checkMaintenance / config / branding
  .Communication   announcements / messages / push
  .Support         createTicket / listTickets / getFaq        ← 核心/服务包
  .Payment         createOrder / queryOrder(M5,仅契约面)
  .RealName        submit / status(可选,合规)
  .Diagnostics     可选包接入点(默认 no-op)
  UI 包            LoginPanel / AnnouncementPanel / SupportPanel(可选,消费 Branding)
```

- 每端 = 平台无关 Core(DTO、生命周期状态机、token 存储、重试)+ 平台 binding(Unity UPM / UE 插件 / Cocos TS / 微信小程序 / Layabox / Godot)。
- **Adapter 禁带业务**:Adapter 只做「网络、存储、生命周期、线程」的平台翻译;任何业务语义出现即架构违规(上移 Core 或 Service)。
- 公共 DTO 以 [contract/](./contract/index.md) 为唯一事实源,多端生成或手写对齐,禁止各自发明字段。
- token 存储走平台安全存储(Unity 加密存储 / UE 平台凭证 / Cocos localStorage 隔离 / 微信 storage / Layabox、Godot 平台存储)。

## 安全边界

- 客户端不持有任何 Provider 凭证、内部 URL、管理接口。
- 登录限流 + 风控前置(M1 基础限流;RiskProvider 默认 oddsmaker,可换)。
- 支付回调只认渠道签名,不认客户端上报金额/状态。
- 实名:字段最小化、传输加密、SDK 不落盘、日志/trace 禁明文([contract/realname.md](./contract/realname.md))。
- 诊断:默认关闭、显式开启、开启明示数据范围([contract/diagnostics.md](./contract/diagnostics.md))。
- 所有写操作审计,trace 贯穿 gateway → Provider。

## 与后端 Provider 的边界

| 能力 | Courier 负责 | Provider 侧负责 | 默认供应商(可换/可关) |
| --- | --- | --- | --- |
| 账号/会话 | 客户端面与网关治理 | 账号存储与核验 | 自建 accounts/sessions |
| 公告 | 玩家侧拉取/订阅 API、SDK 组件 | 投递编排、运营发布 | herald + croupier message |
| 客服 | 玩家侧提单/查询 API | 工单流转、坐席 | croupier support/ticket/faq |
| 推送 | 事件契约与拉取兜底 | 长连接通道 | chirp |
| 风控 | 前置调用 | 规则与判定 | oddsmaker |
| 实名 | 契约与 Provider 治理 | 核验执行 | 默认关闭;自建/阿里云/腾讯云/易盾/Webhook |
| 诊断 | 契约与红线 | 上报存储与分析 | 默认关闭;Sentry/GlitchTip/OTLP |

## 竞品定位

开源没有「服务型聚合 SDK」这个品类(Nakama 是服务器、XtraLife 停滞、AWS 是云模板),商业聚合(MSDK/QuickSDK/XDSDK)证明需求存在但闭源绑渠道。Courier 取位与边界(实时多人/匹配不做、渠道聚合不做)的完整盘点见 [research/competitive.md](./research/competitive.md)。
