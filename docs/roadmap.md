# Courier 路线图

> 原则:每个里程碑都端到端可用(gateway + 至少一端 SDK + 验收),不做"半个功能"。**契约先行:上一里程碑的契约在本里程碑开工前冻结。**分批节奏见 [todo.md](./todo.md),五层定义见 [layers.md](./layers.md)。

## M0 — SDK Contract Foundation(已完成,2026-10)

**范围**

- 冻结基元契约:Error / Request / Response / Pagination / Timestamp / ID / TraceID / Scope / Version / Event(`docs/contract/`,初稿已立)。
- 域契约初稿:RealName / Branding / Diagnostics(红线与 Provider 口径内建)。
- 跨端评审:至少 Unity + 一个非 C# 端过一遍;冻结后 Contract 升 v1。

**验收**

- Unity / Cocos 端可依契约各自手写 DTO 与错误枚举而无歧义;契约测试骨架就位。

## M1 — Auth(地基)(已完成,2026-10)

**范围**

- gateway 瘦身骨架:目录重构 auth / session / scope / routing / aggregation / middleware / providers;Provider 注册表;未配置 Provider → `COMMON_CAPABILITY_DISABLED`。
- AccountProvider 默认实现(自建):邮箱+密码与游客(设备)登录、access/refresh 轮换、吊销、设备绑定、基础限流。数据表:`accounts`、`account_credentials`、`sessions`、`devices`。
- Unity SDK 先行:生命周期状态机 + Identity/Session 域 + token 安全存储 + 自动 refresh;Core/Service/UI 三包目录就位(UI 包可空)。
- 契约:`docs/contract/auth.md` 冻结。

**验收**

- Unity 示例完成 游客登录 → 绑定邮箱 → 登出 → refresh 轮换 → 吊销后拒绝 全链路。
- 并发登录限流生效;错误为结构化 JSON(code/message/traceId,跨端枚举一致)。

## M2 — 公告 + 客服(合规后段:实名)(已完成,2026-10)

**范围**

- AnnouncementProvider(herald/croupier 玩家侧投影)、SupportProvider(croupier 工单/FAQ)、MessageProvider(chirp 会话推送通道,可关=仅拉取)。
- 可选 UI 包首发(公告栏/客服页),消费 Branding(此时全默认标)。
- **实名(M2 后段,合规)**:RealNameProvider 接口 + 热插拔/降级链/熔断落地;默认关闭;首个实现建议自建核验,云厂商(阿里云/腾讯云/易盾)按接入方需要接。

**验收**

- 运营发布公告 → 玩家端 5s 内收到推送并可拉取历史;推送通道关闭时拉取兜底可用。
- 玩家提单 → 坐席回复 → 玩家端实时可见。
- 实名:主 Provider 熔断 → fallback 接管 → 全挂转 `PENDING_REVIEW` 安全态;全程默认关、开启明示数据范围。

## M3 — Remote Config + Player Profile + App(版本/维护)+ Branding + Diagnostics(已完成,2026-10)

**范围**

- ConfigProvider:`game_id / env / platform / version / region` 条件匹配;`config.updated` 推送热生效。
- Player Profile:账号↔角色(player)档案接口。
- Courier.App:GetVersion / CheckUpdate / CheckMaintenance / GetEnvironment。
- BrandingProvider 与 config 同管道;UI 包全量消费品牌配置(公司名/logo/主题色/客服入口),构建期物料指引随模板工程落档。
- Diagnostics 可选包:Crash(Sentry/GlitchTip 可自托管)、OTel Trace(任意 OTLP collector)、性能指标、埋点;默认全关,未配置即 no-op 零开销。

**验收**

- 修改远程配置 → 命中条件的客户端热生效;未命中不受影响。
- 维护模式开启 → 新会话收到 `APP_MAINTENANCE`;品牌配置变更 → UI 包热切换;诊断四类默认零上报,逐类开启逐类生效。

## M4 — Assistant(已完成,2026-10)

**范围**

- 基于 FAQ 知识库的检索问答(默认 Provider 可接 croupier faq);预留 LLM 接口但默认不依赖;答不出一键转人工(复用 M2 链路)。

**验收**

- 常见问题命中率可统计;转人工链路端到端。

## M5 — Payment(只定 Contract,形态开放)(契约与网关已完成,2026-10;真实渠道沙箱随部署面)

**范围**

- Payment Contract:`Order / Purchase / Receipt` + 安全边界;形态由接入方选择(Account Wallet / Game Wallet / Direct Purchase / External Payment),契约不做统一钱包假设。
- 沙箱渠道先行:下单 → 回调(签名校验)→ 发货 → 对账全链路;RiskProvider 前置(大额/异常频次)。

**验收**

- 沙箱渠道全链路 E2E;伪造签名全部拒绝;断单可对账恢复;回调只认渠道签名。

## 后续节奏:验证与交付(2026-10-11 评审归档)

> M0–M5 已落地;重点从「扩展能力」转向「证明现有架构可被外部游戏项目可靠使用」(批次 46)。功能面全量对照与缺口清单见 [research/implementation-status.md](./research/implementation-status.md)。

- **阶段 A — 实现状态与证据审计**(已启动,批次 45):76 项功能逐条对照仓库证据——核心 47(✅45/❌2)、Provider 13、待议 9、不做 7(批次 57 时点,见 implementation-status.md);缺口入账不等于立项,按需走契约提案。
- **阶段 B — 跨引擎一致性**:生命周期/取消/并发刷新/切号/SSE 重连等行为场景六端同结果;真实引擎工程安装/编译/运行验证(UE 引擎集成面、真机联调随部署面)。**进度**(批次 63):五场景六端覆盖审计完成——生命周期/取消/SSE 重连六端已齐;并发刷新补齐:审计发现 Unity 串行不共享(与 TS/Godot「并发共享一次轮换」不一致),修为在途任务共享并以挂起响应构造真实并发重叠验收,UE 同步核心以重入防护验收(一次轮换只发一次请求);切号 client-level 补齐(Unity/Godot 新增,UE/TS 三端原有);剩余开放项 = Unity ApplicationLifecycleMonitor 适配层测试(MonoBehaviour 耦合引擎运行时,纯 dotnet 工程不可覆盖)、纯 HTTP 取消(六端一致 SSE-only,非跨引擎不一致)。
- **阶段 C — Provider 可替换性**:每域至少两种实现(默认 + 替代/测试替身,账号域批次 18 起逐域兑现);禁用/热重载不破坏在途请求;账号/实名/支付定义独立安全切换策略(接口可替换 ≠ 数据可迁移)。**进度**(批次 61):敏感域切换策略已定义并落 architecture.md「敏感域独立切换策略」(账号/实名/支付各自的配置切换面 + 不迁什么 + 实证索引);「禁用/热重载不破坏在途请求」测试覆盖已齐——实名侧 `router` 单测 + e2e,账号侧新增 `Switchable` 并发换源单测(-race);剩余开放项 = 每域第二实现(账号域批次 18、实名域批次 64、玩家档案域批次 67、推送域批次 68、公告域批次 69、助手域批次 70、配置域批次 71、支付域批次 72、客服域批次 73 已兑现——第二实现与默认实现同场景 e2e 行为证明;九个能力域全部两实现,「每域至少两种实现」收口)。
- **阶段 D — 可交付 SDK**:六端最小示例 + quickstart 实测 + 自定义 Provider 接入指南(与批次 44 立项重合,以该批为准)。**进度**(批次 55):六端 `examples/` + quickstart 实测 + 选装清单(`docs/sdk-selection.md`)已落地(`425fe55`/`c5c4814`);自定义 Provider 接入指南已落地(`docs/provider-guide.md`,批次 58)——阶段 D 四项全闭环。

**验收七问**(衡量标准):

1. 新增一个引擎,是否主要新增 Adapter 而非重写业务服务?
2. 新增一个后台供应商,是否主要新增 Provider 而非修改所有客户端?
3. 关闭某项能力,其他能力是否继续正常运行?
4. 不同引擎是否遵守同一套契约与生命周期语义?
5. 接入方能否不修改业务代码替换普通服务?
6. 账号/支付/实名等有状态或高风险能力,是否明确区分接口替换与数据迁移?
7. 新团队能否仅凭文档与示例独立接入?

## 非目标(不做)

- 实时多人/匹配/大厅服务器(Nakama / Agones 的地盘)。
- 渠道包聚合联运(MSDK / QuickSDK 的地盘)。
- 运营端任何界面(默认供应商 Croupier,可自建)。
- 自建长连接推送通道(默认 Provider chirp,可换)。
- 社交/好友/公会(后续再议)。
- 统一钱包假设(支付形态开放,见 M5)。
