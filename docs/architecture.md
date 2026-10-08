# Courier 架构

> **状态**：Draft — 项目启动架构基线，M1 实现前冻结。分层细化（SDK Contract / Core / Platform Adapter / Service Provider / Gateway）见 [layers.md](./layers.md)，分批落地见 [todo.md](./todo.md)。

## 总体形态

```text
┌─────────────────────────────────────────────┐
│ Game Client                                 │
│  Courier SDK (Unity C# / UE C++ / Cocos TS)    │
└──────────────┬──────────────────────────────┘
               │ HTTPS JSON (+ WebSocket 推送, M2+)
┌──────────────▼──────────────────────────────┐
│ Courier Gateway (Go)                           │
│  accounts   账号、注册/登录、实名(预留)      │
│  sessions   token 签发/校验/吊销、设备绑定   │
│  announcements  公告聚合 (→ herald)         │
│  support    客服工单/FAQ (→ croupier)       │
│  assistant  小助手编排 (M3)                 │
│  payments   充值下单/回调/对账 (M4)         │
└──────┬──────────┬──────────┬────────┬───────┘
       │          │          │        │
    herald    croupier    chirp   oddsmaker
   (公告投递)  (客服后端)  (实时)   (风控)
```

## 关键决策

1. **网关是唯一入口。** 客户端永远只和 Courier Gateway 通信；生态后端的地址、凭证、拓扑对客户端不可见。网关负责鉴权、限流、聚合、审计前置。
2. **协议 HTTPS JSON 优先。** 游戏客户端友好、调试友好；二进制协议不是 MVP 目标。推送（公告/客服回复）M2 起用 WebSocket，复用 chirp 的会话通道而不是自建长连接。
3. **scope 唯一。** `game_id + env` 由客户端初始化 SDK 时指定一次，后续请求由网关注入与校验，与 Croupier/Oddsmaker 同一模型。
4. **账号体系是 M1 核心新建。** 表：`accounts`、`account_credentials`、`sessions`、`devices`。token 采用短期 access + 长期 refresh，吊销列表进 Redis（与 chirp gateway 的 kick 能力对齐）。
5. **能力编排而非复制。** 公告/客服/FAQ 只做玩家侧投影（只读列表、提单、订阅），数据事实留在 herald/croupier；assistant M3 复用 croupier faq 知识库做检索增强，不另建知识库。
6. **payments 渠道抽象先行。** M4 才实现，但接口形状 M1 就冻结：`Order / Channel / Callback / Reconcile` 四概念，避免后续返工。

## SDK 结构（多端同构）

```text
CourierClient
  .Auth        login / logout / refresh / bindDevice
  .Announcements  list / subscribe
  .Support     createTicket / listTickets / getFaq
  .Assistant   ask (M3)
  .Payments    createOrder / queryOrder (M4)
```

- 每端 = 平台无关 core（DTO、状态机、token 存储、重试）+ 平台 binding（Unity UPM / UE 插件 / Cocos TS / 微信小程序 / Layabox / Godot）。
- 公共 DTO 以 `docs/api/` 契约为唯一事实源，多端各自生成或手写对齐，禁止各自发明字段。
- token 存储走平台安全存储（Unity PlayerPrefs 加密 / UE 平台凭证 / Cocos localStorage 隔离 / 微信小程序 storage / Layabox/Godot 平台存储）。

## 安全边界

- 客户端不持有任何后端凭证、内部 URL、管理接口。
- 登录接口必须限流 + 风控前置（M1 基础限流，oddsmaker 风控 M4 前接入）。
- 支付回调只认渠道签名，不认客户端上报金额/状态。
- 所有写操作审计，trace 贯穿 gateway → 生态后端。

## 与生态的边界

| 能力 | Courier 负责 | 生态负责 |
| --- | --- | --- |
| 公告 | 玩家侧拉取/订阅 API、SDK 展示组件 | herald 投递、croupier 运营发布 |
| 客服 | 玩家侧提单/查询 API、SDK 界面 | croupier 工单流转、坐席处理 |
| 聊天/推送 | 复用 chirp 会话，不建长连接 | chirp gateway |
| 风控 | 登录/支付前置校验调用 | oddsmaker 规则与判定 |
