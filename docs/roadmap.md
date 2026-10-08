# Courier 路线图

> 原则：每个里程碑都端到端可用（gateway + 至少一端 SDK + 验收），不做"半个功能"。按五层分批的落地节奏见 [todo.md](./todo.md)。

## M1 — 登录验证授权（地基）

**范围**

- gateway：`accounts` / `sessions` 模块，邮箱+密码与游客（设备）两种登录方式，access/refresh token，吊销，基础限流。
- 数据表：`accounts`、`account_credentials`、`sessions`、`devices`（GORM + AutoMigrate，同生态约定）。
- Unity SDK 先行；微信小程序、Layabox、Godot SDK 规划中（M1+逐步补齐）：`CourierClient.Auth` 全流程 + token 安全存储 + 自动 refresh。
- API 契约：`docs/api/auth.md` 冻结。

**验收**

- Unity 示例场景完成 游客登录 → 绑定邮箱 → 登出 → refresh 轮换 → 吊销后拒绝 全链路。
- 并发登录限流生效；错误为结构化 JSON（code/message/traceId）。

## M2 — 公告 + 客服

**范围**

- gateway：`announcements`（对接 herald/croupier message 的玩家侧投影）、`support`（工单提交/查询、FAQ 检索，对接 croupier）。
- WebSocket 推送通道（公告与工单回复），复用 chirp 会话。
- Unity + Cocos + 微信小程序 SDK 两模块；Layabox/Godot SDK 跟进（Auth + Announcements）。

**验收**

- 运营在 croupier 发布公告 → 玩家端 5s 内收到推送并可拉取历史。
- 玩家提单 → croupier 坐席回复 → 玩家端实时可见。

## M3 — 游戏小助手

**范围**

- gateway：`assistant` 编排层，基于 croupier faq 知识库做检索回答；预留 LLM 接口但默认不依赖。
- SDK：助手机器人 UI 组件（Unity 先行）。

**验收**

- 常见问题命中率可统计；答不出时一键转人工工单（复用 M2 链路）。

## M4 — 充值

**范围**

- gateway：`payments` 模块——下单、渠道抽象、支付回调（签名校验）、发货通知、对账骨架、退款预留。
- 第一个真实渠道接入前，必须先用沙箱渠道跑通 下单 → 回调 → 发货 → 对账 全链路。
- oddsmaker 风控前置（大额/异常频次）。

**验收**

- 沙箱渠道全链路 E2E；回调伪造签名全部拒绝；断单可对账恢复。

## 非目标（近期不做）

- 社交/好友/公会（chirp social 成熟后再议）。
- 运营端任何界面（归 Croupier）。
- 自建长连接推送通道（归 chirp）。
