# Courier TODO — 分批落地

> 原则：**不堆业务，先立骨架**。每批交付「接口形状 + 最小实现 + 验收」；批次映射五层（见 [layers.md](./layers.md)），里程碑映射 [roadmap](./roadmap.md)。

## 批次 0 — 更名与定位（已完成，2026-10）

- [x] Concierge → Courier 全量更名（标识符、包名、环境变量、文档、站点、二进制）
- [x] 定位重写：Universal Game SDK + 信使隐喻 + 项目家族表
- [x] 五层架构设计文档（[docs/layers.md](./layers.md)）与本分批 todo（本文档）

## 批次 1 — L1 契约骨架（一切的地基）

- [ ] `docs/api/` 目录与契约规范（变更流程、版本策略）
- [ ] `docs/api/errors.md`：统一错误模型（code / message / traceId）与错误码表
- [ ] `docs/api/scope.md`：`game_id + env` 隔离规则（注入与校验约定）
- [ ] `docs/api/auth.md`：M1 登录域契约（路由、DTO、错误码、时序），冻结供各端对齐
- **验收**：Unity / Cocos 端可依契约各自手写 DTO 而无歧义

## 批次 2 — L5 Gateway 骨架

- [ ] `main.go` 重构：中间件链（trace → 结构化错误 → 限流 → scope 注入）+ 按域路由挂载
- [ ] `gateway/internal/` 目录骨架：每域一个模块包，可注册可摘除
- [ ] 保留 `/healthz`；错误统一结构化 JSON
- **验收**：go test 覆盖中间件行为；模块注册顺序与依赖方向符合 layers.md 依赖规则

## 批次 3 — L4 Service Provider：auth（M1 主体）

- [ ] accounts / sessions 模块：邮箱+密码 与 游客（设备）登录、access/refresh 轮换、吊销
- [ ] 数据表：accounts、account_credentials、sessions、devices
- [ ] 严格实现批次 1 的 auth 契约；基础限流生效
- **验收**：roadmap M1 服务端验收项（结构化错误、并发登录限流）

## 批次 4 — L2 Core（Unity 先行）

- [ ] `CourierClient` 门面 + Auth 域接口
- [ ] ITransport / ITokenStore / IClock 注入接口
- [ ] 登录状态机：登录 / refresh 轮换 / 吊销处理；重试与超时
- [ ] 纯逻辑单测（不依赖 Unity 运行时）
- **验收**：Core 可独立测试；与 auth 契约字段 100% 对齐

## 批次 5 — L3 Platform Adapter（Unity UPM）

- [ ] UPM 包骨架：package.json + Runtime / Editor / Samples~
- [ ] Unity 安全存储实现 ITokenStore（非明文 PlayerPrefs）
- [ ] UnityWebRequest 实现 ITransport
- **验收**：示例场景跑通 游客登录 → 绑定邮箱 → 登出 → refresh 轮换 → 吊销拒绝（M1 全链路）

## 批次 6+ — 横向铺开（按 roadmap M2–M4）

- [ ] Cocos / 小程序 / Layabox / Godot / UE 端 Core + Adapter 跟进
- [ ] announcements / support（herald、croupier 投影）+ chirp 推送通道（M2）
- [ ] assistant 编排（M3）；payments 渠道抽象与沙箱全链路（M4）

## 明确不做

- 运营端任何界面（归 Croupier）；自建长连接（归 chirp）；社交/好友/公会（后续再议）。
