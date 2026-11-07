
# Courier 架构

> **状态**:Current — 随实现维护(2026-10-08 M0 立宪冻结,M1–M5 已全部落地)。分层细化(SDK Contract / Core / Platform Adapter / Service Provider / Gateway)见 [layers.md](./layers.md),分批落地见 [todo.md](./todo.md),契约宪法见 [contract/](./contract/index.md),竞品定位见 [research/competitive.md](./research/competitive.md)。

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

**快递隐喻**:Courier(快递员)之名即产品形态——能力拆成一个个小块「包裹」,每块独立运作、可替换、可拒收;契约是统一的包装规格,块粒度以「大小能运作」为准——够小才可换,能跑才交付;分拣系统(Gateway)不关心包裹里是什么业务,只认面单(接口边界)。

## Provider 原则:默认提供,可换可关

生态项目只能以「供应商(Provider)」角色存在:

1. **每个能力的后端 = 可插拔 Provider**,统一接口、配置驱动。
2. **Courier 默认提供一套开箱即用的实现**:公告默认 herald、客服默认 croupier、推送默认 chirp、实名默认 warden(自建核验)、账号默认自建 courier-account(第二实现 courier-account-alt 同契约可换)——默认供应商可以打包,但**接不接、用哪家、换不换由接入方配置决定,SDK 不替用户做选择**。
3. **不硬绑定、不强制依赖**:任何 Provider 都可替换为自建实现、第三方服务,或直接关掉;关掉 = 能力降级(`COMMON_CAPABILITY_DISABLED`),不是报错。
4. **示例**:公告能力默认走 herald Provider;接入方可配置成自己的 HTTP 端点,或禁用公告(客户端安全地隐藏对应 UI)。
5. **接口统一 ≠ 数据迁移**:Provider 机制承诺「接口统一」与「实现可替换」;数据可迁移(账号/订单/档案历史数据搬后台)**不是默认承诺**,需按域单独评估——切 Provider 前先回答用户 ID、会话有效性、在途订单是否兼容。
6. **接入方式两种**:服务端 Provider 是默认方式(客户端只见网关,凭证与路由全在服务端;账号/支付/实名等敏感能力必须走此方式);客户端直连外部服务仅限明确允许直连的公开服务,不得成为默认切换机制,服务端密钥永不下客户端。
7. **每个模块至少两种实现**:默认实现 + 可替换实现(真实第二实现或测试替身),接入方按配置选择。**只有一种实现的能力,「可换」名存实亡**——现状账号域已兑现(courier-account + courier-account-alt,同冻结契约异构实现 + 同场景 e2e 行为证明,批次 18),实名域已兑现(warden 自建核验 + warden-alt 托管核验,判定来源/落库内容异构 + 同场景 e2e 行为证明,批次 64),玩家档案域已兑现(archivist 状态表 + archivist-alt 事件日志,存储模型异构 + 同场景 e2e 行为证明,批次 67),推送域已兑现(chirp 扇出推送 + chirp-alt 共享环形日志/游标拉取,枢纽模型异构 + 同场景 e2e 行为证明,批次 68),公告域已兑现(herald 读时扫描排序 + herald-alt 写时索引,索引模型异构 + 同场景 e2e 行为证明,批次 69),助手域已兑现(sage 读时规范化 + sage-alt 写时预计算规范化,预处理模型异构 + 同场景 e2e 行为证明,批次 70),配置域已兑现(scribe 读时逐条目条件 AND + scribe-alt 写时维度索引/分阶段过滤,求值模型异构 + 同场景 e2e 行为证明,批次 71),支付域已兑现(teller 可变状态表 + teller-alt 追加式事件账本/投影重放,状态表示模型异构 + 同场景 e2e 行为证明,批次 72),客服域已兑现(croupier 读时扫描排序/偏移游标 + croupier-alt 写时属主有序索引/键集游标,列表组织模型异构 + 同场景 e2e 行为证明,批次 73),邮件域已兑现(carrier 读时扫描排序 + carrier-alt 写时索引,列表组织模型异构 + 同场景 e2e 行为证明,2026-11-01 F62),兑换码域已兑现(voucher 已核销集合去重 + voucher-alt 事件账本,状态模型异构 + 同场景 e2e 行为证明,2026-11-01 F62)——十一个能力域全部两实现,阶段 C「每域至少两种实现」已收口。

## 总体形态

```text
┌───────────────────────────────────────────────┐
│ Game Client                                   │
│   Courier SDK:Core / Service / UI(可选包)     │
└──────────────┬────────────────────────────────┘
               │ HTTPS JSON (+ SSE 推送通道, M2)
┌──────────────▼────────────────────────────────┐
│ Courier Gateway(Go):auth · session · scope    │
│   · routing · aggregation · middleware        │
│   · providers(Provider 注册与治理)             │
└──────┬─────────┬─────────┬─────────┬──────────┘
       ▼         ▼         ▼         ▼
  AccountProvider(自建 accounts)…均经 Provider 接口
       ▼
  默认供应商:herald(公告)· croupier(客服)· chirp(推送)· warden(实名)· scribe(app)· archivist(档案)· sage(助手)· teller(支付)
  —— 以上全部可被接入方替换或关闭
```

## 能力树

```text
Courier
├── Core          Lifecycle / Network / Error / Retry / Cache / Telemetry 挂钩
├── Identity      Login / Logout / Bind / Device / OAuth(可选,第三方)/ RealName(可选,合规)
├── Session       AccessToken / RefreshToken / Session
├── Player        Profile / Game Player
├── App           Version / Maintenance / Remote Config / Branding
├── Communication Announcement / Message / Push
├── Support       FAQ / Ticket(核心);Panel(可选 UI 包)
├── Assistant     FAQ 检索问答(可选,LLM 供应商可替换)
├── Payment       Order / Purchase / Receipt(契约+内置沙箱渠道,四形态可选)
├── Diagnostics   Crash / OTel Trace / Perf / Analytics(可选包)
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
| UI 包 | `AnnouncementPanel` / `CustomerServicePanel` / `AssistantPanel` / `RealNamePanel` 等组件,**消费 Branding 配置** | 可选 |

- `Support.CreateTicket()` 属核心/服务包;`CustomerServicePanel` 属可选 UI 包——SDK 不强制任何 UI。
- 各引擎各取所需:Unity 有 UPM UI 包,UE 走 UMG(UI 面属引擎集成面,未落仓),Cocos 自绘;UI 包缺席不影响功能面。
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
├── audit/         实名审计事件接缝(AuditSink / LogSink / HTTPSink)
├── middleware/    限流 / trace / 恢复 / scope / 维护门 / 1MiB 请求体上限(最外层)
└── providers/     Provider 接口 + 默认实现(能力域 → 注册名)
    ├── account/        AccountProvider(identity)   默认:courier-account(自建
    │                   accounts/sessions)
    ├── accountalt/    AccountProvider(identity 第二实现,同契约异构,可换)
    ├── herald/         AnnouncementProvider(announcements)
    ├── heraldalt/      AnnouncementProvider(announcements 第二实现,写时索引,同契约可换)
    ├── croupier/       SupportProvider(support)
    ├── croupieralt/    SupportProvider(support 第二实现,写时属主索引 + 键集游标,同契约可换)
    ├── chirp/          MessageProvider(messages,SSE)
    ├── chirpalt/       MessageProvider(messages 第二实现,环形日志+游标,同契约可换)
    ├── warden/         RealNameProvider(realname)  默认:自建核验;阿里云/腾讯云
    │                   慧眼/易盾/Webhook 按需适配同一形状
    ├── wardenalt/      RealNameProvider(realname 第二实现,托管核验,同契约可换)
    ├── scribe/         AppProvider(app:config/branding/版本/维护,自建管道,
    │                   可由 croupier 发布)
    ├── scribealt/      AppProvider(app 第二实现,写时维度索引,同契约可换)
    ├── archivist/      PlayerProvider(player)
    ├── archivistalt/   PlayerProvider(player 第二实现,事件日志模型,同契约可换)
    ├── sage/           AssistantProvider(assistant,FAQ 检索问答)
    ├── sagealt/       AssistantProvider(assistant 第二实现,写时预计算规范化,同契约可换)
    ├── teller/         PaymentsProvider(payments:订单-回调-发货;渠道沙箱随部署面)
    └── telleralt/      PaymentsProvider(payments 第二实现,事件账本投影,同契约可换)
```

> 诊断不经网关:契约红线(diagnostics.md)规定诊断数据直发接入方自有端点,网关无 DiagnosticsProvider。

- 未配置的 Provider:路由照常注册(`/v1/<能力域>/` 前缀全挂载),请求时网关以 501 降级信封返回 `COMMON_CAPABILITY_DISABLED`(见 [contract/errors.md](./contract/errors.md))——客户端以此探测能力开关,而非得到 404。
- `accounts` / `announcements` 这类名字不得作为 gateway 一级业务目录出现——它们是 Provider 的默认实现,住在 `providers/` 下。

## Provider 治理:热插拔 / 降级链 / 熔断

- **配置驱动**:每个 Provider 由配置声明 `primary` + `fallbacks[]`;运行时配置重载即生效,不重启(路由表原子替换,在途请求按旧表完成)。现状:全域路由表热重载经 `routing.Hot` 落地(批次 99,F78 一期)——`SIGHUP` 重读 `COURIER_GATEWAY_CONFIG`,失败保留旧表(fail-safe);跟随 primary 的接缝(身份验证器/推送源/维护门)随重载双向重指;实名域另有域内细粒度治理 `providers/router.Reload`(降级链/熔断,批次 7)。管理面重载端点未实现(二期,前置=管理面鉴权契约)。
- **降级链**:主 Provider 失败/超时 → 依序 fallback;全部失败按域定义安全态(如 RealName → `PENDING_REVIEW`)。
- **熔断**(实名域已落地):按调用次数驱动——连续 N 次失败开路,开路后 30 次调用半开探测恢复(无定时器);熔断期直接走 fallback。
- **HealthCheck**:逐请求探活,失败落 fallback、全链不健康 503;注册表不做实例剔除(接缝预留)。
- RealName([contract/realname.md](./contract/realname.md))是第一个完整适用方;规则对所有 Provider 通用。

### 敏感域独立切换策略(账号 / 实名 / 支付)

接口可替换 ≠ 数据可迁移;三个敏感域各自定义切换面与不迁什么:

| 域 | 切换面(配置即换) | 不迁 / 保守面 | 实证 |
| --- | --- | --- | --- |
| 账号 | 验证器与主存同源:路由表 identity primary 换到第二实现即经 `auth.Switchable` 原子重指(fallbacks 不参与,验证器不逐请求切) | 令牌语义不跨实现:account(不透明随机令牌 + 会话表)与 accountalt(HMAC 自验证令牌)互不认,切换后旧令牌验不过 → 重新登录 | 两实现同 wire 由一致性 e2e 证明(`e2e/identity_conformance_test.go`,批次 18);并发换源下在途验证只返回旧/新目标身份、无中间态(`auth` 包 `-race` 单测) |
| 实名 | 默认关闭,路由表显式开启;`router.Router` 路由表原子替换 + 熔断 + 全链失败安全态(PENDING_REVIEW / 不可玩 / 不放行) | 身份输入只活调用栈(禁日志/trace/诊断,只允许存指纹)——切换不得借机把明文落库 | `router` 包单测(降级序 / 熔断半开 / 安全态 / 在途按旧表)+ `e2e/realname_test.go`(默认 501 / fallback 链 / 热重载续服) |
| 支付 | 渠道切换 = 换回调签名密钥配置(按渠道 HMAC-SHA256)+ `Deliver` 发货钩子;密钥空 = 回调一律 403 | 订单状态与台账不随渠道走(台账归部署面 / 接入方存储);回调幂等与断单对账语义契约不变 | `payment.md` 订单-发货核心流 + 回调幂等红线;`teller.Options.ChannelSecret` / `Deliver` |

## 新能力设计

### Remote Config(M3)

- SDK:`Courier.Config.Get("login.enable_guest")`。
- 匹配维度:`game_id / environment / platform / version / region`——配置按条件命中。
- 链路:接入方或默认供应商(如 croupier)发布 → ConfigProvider → Gateway 缓存 → SDK(本地缓存 + `config.updated` 推送热生效)。

### Courier.App(M3)

- `CheckUpdate / CheckMaintenance / GetEnvironment`(Unity 门面 `CheckUpdateAsync` / `CheckMaintenanceAsync` / `GetEnvironmentAsync`,对应 `GET /v1/app/version|maintenance|environment` 三个端点;无独立 GetVersion 门面)。
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

## 源码分层

以 `af2b0a2` 为基线的源码分析把仓库归纳为 7 个职责层，而不是照抄目录名。术语证据见[术语表](./glossary.md)。

### 运行层

- **职责**：启动进程、校验 scope、验证会话、限流、恢复故障、选择健康供应商链并序列化响应信封。
- **实现落点**：进程入口 `gateway/cmd/gateway/main.go:47`；能力分发 `gateway/routing/routing.go:28`；治理链 `gateway/middleware/chain.go:32`；会话验证 `gateway/session/session.go:46`。
- **配置 schema 与输入**：进程与供应商装配输入来自 `gateway/cmd/gateway/main.go:48`、`gateway/cmd/gateway/main.go:314`、`gateway/cmd/gateway/main.go:341`；JSON 路由表形状是 `gateway/providers/config.go:14`。
- **专属协议**：通用 wire 不变量在 [contract/primitives](./contract/primitives.md)、[contract/scope](./contract/scope.md)、[contract/errors](./contract/errors.md)、[contract/versioning](./contract/versioning.md)；服务端编码是 `gateway/aggregation/envelope.go:148`。
- **文案挂载**：[Provider 接入指南](./provider-guide.md)说明运行环境输入与替换验收。

### 契约层

- **职责**：冻结 wire 形状、scope、错误、分页、事件、兼容规则和每个域的玩家侧语义。
- **实现落点**：规范性 Markdown 在 `docs/contract/`；机器可读输入是 `docs/contract/fixtures/errors.json`、`docs/contract/fixtures/scope.json`、`docs/contract/fixtures/dto.json`。
- **配置 schema**：协议 fixture 就是 schema 快照；`tools/contractgen/main.go:77` 逐字节校验生成的客户端面。
- **专属协议**：`docs/contract/` 全部是协议事实源；`docs/contract/events.md:21` 单独冻结 SSE 推送语义。
- **文案挂载**：[契约总纲](./contract/index.md)是立宪页；架构能力树交叉链接域契约。

### SDK 核心层

- **职责**：提供平台无关配置、生命周期、会话、重试、信封解析、typed 错误和域服务编排。
- **实现落点**：Cocos/TS Core 在 `sdks/cocos/src/core/`；门面与延迟接线在 `sdks/cocos/src/core/courierClient.ts:26`；Unity Core 在 `sdks/unity/packages/com.courier.core/Runtime/Core`；UE Core 在 `sdks/ue/core`；Godot Core 在 `sdks/godot/src/core`。
- **配置 schema**：`CourierConfig` 字段与校验在 `sdks/unity/packages/com.courier.core/Runtime/Core/CourierConfig.cs:13`；TS 形状在 `sdks/cocos/src/core/types.ts:5`；Godot 在 `sdks/godot/src/core/courierClient.gd:18` 读取同名 `gameId`、`env`、`endpoint`、`language`。
- **专属协议**：生成的客户端契约文件是具体协议面，例如 `sdks/cocos/src/contract/errors.ts`、`sdks/ue/contract/envelope.hpp`。
- **文案挂载**：[SDK 结构](#sdk-结构多端同构)与 `examples/README.md` 说明接入形态。

### 平台适配层

- **职责**：把传输、令牌持久化、前后台信号和可选诊断上报翻译进各引擎或宿主运行时，不携带业务语义。
- **实现落点**：Unity 适配器在 `sdks/unity/packages/com.courier.core/Runtime/Adapter`；微信/Cocos 适配器在 `sdks/cocos/src/platform/wechat`；Layabox 适配器在 `sdks/laybox/src/platform/laya`；Godot 绑定在 `sdks/godot/src`；UE 宿主绑定经 UE Core 接缝注入。
- **配置 schema**：可选诊断对象在 `sdks/cocos/src/diagnostics/diagnosticsTypes.ts:20`；传输与令牌存储是构造依赖，见 `sdks/cocos/src/core/courierClient.ts:11`。
- **专属协议**：诊断在[契约 diagnostics](./contract/diagnostics.md)中定义直发 schema；普通调用复用契约层和 SDK 核心层。
- **文案挂载**：[架构 SDK 结构](#sdk-结构多端同构)，以及存在的各 SDK README。

### 域供应商层

- **职责**：在通用 Provider 处理器后实现 11 个能力域，包含域状态、事件发布、外部核验/支付接缝和异构存储模型。
- **实现落点**：处理器按 `gateway/providers/*` 分组；能力到目录的映射从 `gateway/providers/account/account.go:82` 开始，注册在 `gateway/cmd/gateway/main.go:218`、`gateway/cmd/gateway/main.go:223`。
- **配置 schema**：选择配置是 `gateway/providers/config.go:14`；凭证与持久化输入在 `gateway/cmd/gateway/main.go:139` 至 `gateway/cmd/gateway/main.go:216` 接线，并在[Provider 接入指南](./provider-guide.md#5-凭证与配置注入)汇总。
- **专属协议**：`docs/contract/` 下各域契约定义端点与 DTO 协议；推送事件登记在[契约 events](./contract/events.md)。
- **文案挂载**：[Provider 接入指南](./provider-guide.md)映射每个能力、Options 类型与第二实现。

### 构建与守同工具层

- **职责**：生成跨端契约产物、强制 TS 字节一致、校验 Unity 包隔离，并定义仓库测试矩阵。
- **实现落点**：生成入口 `tools/contractgen/main.go:112`；一致性守同 `tools/tsync/main_test.go:44`；包断言 `tools/packcheck/main_test.go:43`；测试矩阵 `.github/workflows/tests.yml:47`。
- **配置 schema**：生成路径与 fixture 输入声明在 `tools/contractgen/main.go:77`；CI 工具版本和工程编码在 `.github/workflows/tests.yml`、`package.json`。
- **专属协议**：`docs/contract/fixtures/` 既是协议快照，也是生成输入。
- **文案挂载**：README 的测试与仓库布局章节将本层连接到命令。

### 验收层

- **职责**：执行单元、纯逻辑、无头引擎和本地网关端到端检查；证明配对供应商具备同一 wire 行为。
- **实现落点**：网关一致性套件是 `gateway/e2e/*_conformance_test.go`；引擎套件随 SDK 放置；CI 编排是 `.github/workflows/tests.yml:47`。
- **配置 schema**：CI 命令与引擎版本是 `.github/workflows/tests.yml:54` 到 `.github/workflows/tests.yml:137`；本层不新增运行时配置。
- **专属协议**：fixture 与一致性场景构成验收协议；配对示例包括 `gateway/e2e/identity_conformance_test.go`、`gateway/e2e/payments_conformance_test.go`。
- **文案挂载**：[Provider 接入指南 · 替换验收](./provider-guide.md#6-替换验收一致性测试先例)与仓库 TODO 台账。

### 拆分理由

1. **契约先于代码**：兼容性规则先于端点定义（`docs/contract/versioning.md:14`），未知错误码/字段必须容忍（`docs/contract/versioning.md:26`）。独立契约层防止某个 SDK 或网关实现悄悄变成 wire 定义。
2. **运行治理保持通用**：`Capability` 明确禁止后端实体名（`gateway/providers/capability.go:3`），分发器只看到健康探测与供应商链（`gateway/routing/routing.go:64`）。更换供应商不需要改入口治理。
3. **scope 与身份必须共享同一路径**：scope 只经两个已校验 header 进入（`gateway/middleware/chain.go:68`），会话再与签发绑定比对（`gateway/session/session.go:68`）。若放进域代码，会出现按域漂移。
4. **跨域语义由接缝拥有**：依赖域收到可切换验证器，并经原子事件目标发布（`gateway/cmd/gateway/main.go:69`、`gateway/cmd/gateway/main.go:80`）；重载时验证器、发布器、维护门一起重指（`gateway/cmd/gateway/reload.go:53`），避免 primary 切换后残留旧依赖。
5. **客户端可移植需要两个边界**：构造 `CourierClient` 前注入传输与令牌存储（`sdks/cocos/src/core/courierClient.ts:14`）。重试/信封/生命周期逻辑因此不进入任何引擎绑定，内存与安全持久化也变成显式适配器选择（`sdks/cocos/src/core/tokenStore.ts:27`）。
6. **域实现必须可独立替换**：支付状态藏在 `OrderStore`/`StateFile` 后（`gateway/providers/teller/teller.go:77`），客服附件未注入签名器即 fail-closed（`gateway/providers/croupier/croupier.go:48`）。异构账本或对象存储因此不会泄漏进契约层或运行层。
7. **生成面必须机械守同**：契约生成把 fixture 映射到固定的 Unity/TS/UE/Godot 路径（`tools/contractgen/main.go:99`），漂移退出非零（`tools/contractgen/main.go:112`）；TS 共享目录逐字节比较（`tools/tsync/main_test.go:44`）。分层漂移成为测试失败，而不是只能靠评审记忆的结论。

## 关键决策

1. **契约是宪法。** M0 立宪并冻结([contract/](./contract/index.md));M1 Auth 开工的前提。SDK Contract > Everything。
2. **网关是唯一入口。** 客户端只和 Courier Gateway 通信;Provider 与后端拓扑对客户端不可见。
3. **协议 HTTPS JSON 优先。** 推送 M2 起走 SSE(messages.md 冻结选型:标准库可全实现、HTTP 语义复用、Unity UnityWebRequest 可流式读;WebSocket 留后续扩展,需写时升版本),复用默认推送 Provider(chirp)的会话通道,且通道本身可替换。
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
  .Identity        login / logout / bind / device / oauth(可选,第三方)
  .Session         token 状态与轮换
  .Assistant       faq 检索问答(可选,LLM 供应商可替换)
  .Player          profile / gamePlayer
  .App             version / checkUpdate / checkMaintenance / config / branding
  .Communication   announcements / messages / push
  .Support         createTicket / listTickets / getFaq        ← 核心/服务包
  .Payment         createOrder / queryOrder(M5,契约+内置沙箱渠道)
  .RealName        submit / status(可选,合规)
  .Diagnostics     可选包接入点(默认 no-op)
  UI 包            AnnouncementPanel / CustomerServicePanel / AssistantPanel(可选,消费 Branding)
```

- 每端 = 平台无关 Core(DTO、生命周期状态机、token 存储、重试)+ 平台 binding(Unity UPM / UE 插件 / Cocos TS / 微信小程序 / Layabox / Godot——项目形态已落位,插件化打包随引擎集成面)。
- **Adapter 禁带业务**:Adapter 只做「网络、存储、生命周期、线程」的平台翻译;任何业务语义出现即架构违规(上移 Core 或 Service)。
- 公共 DTO 以 [contract/](./contract/index.md) 为唯一事实源,多端生成或手写对齐,禁止各自发明字段。
- token 存储接口统一(`ITokenStore` / `TokenStore`),按端落地:Unity 加密存储、微信 storage(miniprogram `platform/wechat`)、Layabox LocalStorage、Godot `user://` 持久文件、Cocos `sys.localStorage`、UE 文件持久化(`FileTokenStore`,经注入 `FileSystem` 接缝)已实现;`MemoryTokenStore` 仍为仓内默认(测试与不落盘场景,详见 [layers.md](./layers.md))。

## 安全边界

- 客户端不持有任何 Provider 凭证、内部 URL、管理接口。
- 敏感端点限流内建 account Provider(register/login/guest/bind/refresh 等敏感端点更严口径);网关 middleware 仅全局基础限流;实名域无限流。支付沙箱期前置风控随部署面(roadmap M5)。
- 支付回调只认渠道签名,不认客户端上报金额/状态。
- 实名:字段最小化、传输加密、SDK 不落盘、日志/trace 禁明文([contract/realname.md](./contract/realname.md))。
- 诊断:默认关闭、显式开启、开启明示数据范围([contract/diagnostics.md](./contract/diagnostics.md))。
- 写操作可审计:trace 贯穿 gateway → Provider;实名域关键动作(verify/curfew/charge_check/playtime_report)已落结构化审计事件面(`gateway/audit` 共享接缝,两个 realname 实现同注入 `AuditSink`,nil = 不产出;`COURIER_REALNAME_AUDIT=log` 开日志半边),事件只含账号维度元数据(身份输入不进事件,realname.md 红线),落库随部署面。

## 与后端 Provider 的边界

| 能力 | Courier 负责 | Provider 侧负责 | 默认供应商(可换/可关) |
| --- | --- | --- | --- |
| 账号/会话 | 客户端面与网关治理 | 账号存储与核验 | courier-account(自建;第二实现 courier-account-alt 同契约可换) |
| 公告 | 玩家侧拉取/订阅 API、SDK 组件 | 投递编排、运营发布 | herald + croupier message;第二实现 herald-alt(写时索引,同契约可换) |
| 客服 | 玩家侧提单/查询 API | 工单流转、坐席 | croupier support/ticket/faq;第二实现 croupier-alt(写时属主索引 + 键集游标,同契约可换) |
| 推送 | 事件契约与拉取兜底 | 长连接通道 | chirp(扇出;第二实现 chirp-alt 环形日志+游标,同契约可换) |
| 实名 | 契约与 Provider 治理 | 核验执行 | warden(自建核验;阿里云/腾讯云慧眼/易盾/Webhook 同形状可接) |
| 配置/品牌/版本 | 契约、条件投影、热生效 | 内容发布管道 | scribe(自建;第二实现 scribe-alt 写时维度索引,同契约可换) |
| 玩家档案 | 契约与治理 | 档案存储、角色绑定 | archivist(自建;第二实现 archivist-alt 事件日志,同契约可换) |
| 助手 | 契约与检索面 | FAQ 知识库、坐席 | sage(自建 FAQ;第二实现 sage-alt 写时预计算规范化,同契约可换) |
| 支付 | 契约与订单面 | 渠道回调、发货、对账 | teller(自建;第二实现 teller-alt 事件账本投影,同契约可换) |
| 诊断 | 契约与红线 | 上报存储与分析 | 不经网关,直发接入方端点;Sentry/GlitchTip/OTLP |

## 竞品定位

开源没有「服务型聚合 SDK」这个品类(Nakama 是服务器、XtraLife 停滞、AWS 是云模板),商业聚合(MSDK/QuickSDK/XDSDK)证明需求存在但闭源绑渠道。Courier 取位与边界(实时多人/匹配不做、渠道聚合不做)的完整盘点见 [research/competitive.md](./research/competitive.md)。
