# 事件契约

> 状态:Frozen v1(2026-10-08,M0 跨端评审冻结;具体业务事件随域契约注册)。定义事件信封、生命周期事件与推送通道语义。

## 事件信封

```json
{
  "id": "evt_01JAXXXX…",
  "type": "announcement.published",
  "occurredAt": "2026-10-08T03:20:00.123Z",
  "payload": { }
}
```

- `type`:`<domain>.<event>` 两段小写点分,多词用下划线(`lifecycle.token_expired`);域名与错误契约的域前缀对应(小写形式)。
- `id`:全局唯一,客户端按 id 幂等去重。
- `payload` 结构由各域契约定义,信封层不感知。

## 传输通道

- M2 起 WebSocket 推送,复用默认推送 Provider(chirp)的会话通道;**通道本身也是 Provider 能力**,接入方可换或关闭(关闭后仅拉取)。
- **更正(2026-10-10)**:传输选型以 [messages.md](./messages.md)(M2 冻结)为准——**SSE**(text/event-stream),非本文冻结时(M0)暂定的 WebSocket;WebSocket 留后续扩展(双端需写时升版本)。
- 拉取兜底:所有推送事件都有对应拉取端点;推送丢失以拉取补偿,不承诺至少一次。
- 语义:最多一次推送 + 拉取补偿 = 客户端以「列表为准、推送为提示」消费。

## 生命周期事件(SDK 状态机事件面)

状态机定义见 [architecture.md](../architecture.md)「SDK 生命周期」;其对外事件:

| type | 触发 |
| --- | --- |
| `lifecycle.initialized` | Init 完成,进入 Ready |
| `lifecycle.suspended` | 切后台/断网,进入 Suspended |
| `lifecycle.resumed` | 恢复,重新可用 |
| `lifecycle.token_expired` | access 过期,自动 refresh 开始 |
| `lifecycle.signed_out` | 登出/被踢,回到 Ready(未认证) |
| `lifecycle.account_switched` | 切换账号 |

## 已注册业务事件

| type | 域契约 | 说明 |
| --- | --- | --- |
| `announcement.published` | announcements(M2) | 新公告 |
| `support.ticket_replied` | support(M2) | 工单新回复 |
| `config.updated` | config(M3) | 远程配置变更 |
| `app.maintenance_changed` | app(M3) | 维护状态变化 |
| `app.force_update` | app(M3) | 强更通知 |
| `payment.paid` | payment(M5,v1.1) | 订单已支付(提示,载荷只含 orderId;[payment.md](./payment.md)) |
| `payment.delivered` | payment(M5,v1.1) | 订单已发货(含对账恢复;提示,载荷只含 orderId) |

注册新事件 = 域契约变更,走 [总纲](./index.md) 变更流程。
