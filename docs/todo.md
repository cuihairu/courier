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

## 批次 4 — M1:Unity Core(L2)

- [ ] `CourierClient` 门面 + Identity/Session 域
- [ ] 生命周期状态机(Uninitialized→…→PlayerReady、Suspended/Resuming)+ 生命周期事件
- [ ] ITransport / ITokenStore / IClock 注入接口;重试与超时
- [ ] 纯逻辑单测(不依赖 Unity 运行时)
- **验收**:状态机全事件单测;与 auth 契约字段 100% 对齐

## 批次 5 — M1:Unity Adapter + 三包骨架(L3)

- [ ] UPM 包骨架:Core / Service / UI 三包目录(UI 包可空,结构就位)
- [ ] 安全存储实现 ITokenStore(非明文 PlayerPrefs);UnityWebRequest 实现 ITransport
- [ ] 前后台/进程信号 → 状态机事件
- **验收**:游客登录 → 绑定邮箱 → 登出 → refresh 轮换 → 吊销拒绝(M1 全链路);切后台恢复状态正确

## 批次 6 — M2:公告/客服/推送 Provider + UI 包

- [ ] AnnouncementProvider / SupportProvider / MessageProvider(herald、croupier、chirp 为默认供应商,均可配置替换或关闭)
- [ ] 推送通道:chirp 会话复用;关闭 = 仅拉取兜底
- [ ] 可选 UI 包首发(公告栏/客服页),消费 Branding(默认标)
- **验收**:roadmap M2 前两项

## 批次 7 — M2 后段:实名(合规)

- [ ] RealNameProvider 热插拔:路由表热重载、降级链、熔断(首个实现:自建核验;云厂商按需)
- [ ] 默认关闭;开启即数据范围明示;脱敏口径落 UI
- **验收**:roadmap M2 实名验收项

## 批次 8 — M3:Config / Player / App / Branding / Diagnostics

- [ ] ConfigProvider(条件匹配 + 热生效);Player Profile;Courier.App
- [ ] BrandingProvider(config 同管道);UI 包消费品牌;构建期物料指引
- [ ] Diagnostics 可选包(四类 Provider 适配;默认全关,no-op 零开销)
- **验收**:roadmap M3 验收项

## 批次 9 — M4:Assistant;批次 10 — M5:Payment

- [ ] FAQ 检索问答 + 转人工(M4)
- [ ] Payment Contract 冻结(Order/Purchase/Receipt + 四形态)+ 沙箱渠道全链路(M5)

## 明确不做

- 实时多人/匹配/大厅(Nakama / Agones 地盘);渠道包聚合联运(MSDK / QuickSDK 地盘);运营端界面(默认供应商 Croupier,可自建);自建长连接(默认 Provider chirp,可换);统一钱包假设。
