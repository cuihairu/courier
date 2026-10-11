# 客服契约(Support)

> 状态:**Frozen v1** · 里程碑 M2 · 冻结日期 2026-10-08。
> 域前缀 `SUPPORT` 已注册([errors.md](./errors.md));本文档是 M2 服务端(默认供应商 **croupier**)与各端 SDK 的一致性依据,冻结后只加不改,破坏性变更升版本(见 [versioning.md](./versioning.md))。

## 能力与范围

- 能力域 `support`(`providers.CapSupport`),路由 `/v1/support/*`;默认供应商 **croupier**,可换可关。
- 玩家侧:提单、追加消息、查列表/详情、FAQ 检索。**坐席侧**(回复、关单、分派)走 croupier 内部管理面,不经网关——网关只有玩家可读可写自己的工单。
- 坐席回复的实时可见走 [messages.md](./messages.md) 通道(`support.ticket_replied` 事件);通道关闭时靠拉取详情兜底。
- 本契约**不覆盖**:坐席工作台、工单分派策略、附件(后续扩展,经 `extras` 或新端点)。

## 端点总览(挂载 `/v1/support/*`)

| 端点 | 认证 | 说明 |
| --- | --- | --- |
| `POST /v1/support/tickets` | Bearer | 提单 |
| `GET /v1/support/tickets` | Bearer | 我的工单列表(分页,`updatedAt` 倒序) |
| `GET /v1/support/tickets/{ticketId}` | Bearer | 工单详情(含消息) |
| `POST /v1/support/tickets/{ticketId}/messages` | Bearer | 追加玩家消息 |
| `GET /v1/support/faq` | Bearer | FAQ 检索(关键词,最多 20 条) |

- 认证、scope header、信封与分页同 [auth.md](./auth.md) / [primitives.md](./primitives.md)。
- 工单**归属校验**:非本人工单一律 404 `SUPPORT_TICKET_NOT_FOUND`(不泄露存在性)。

## 数据模型

### ticket(工单)

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | id | 服务端生成,UUIDv7,前缀 `tkt_` |
| `title` | string | 1–120 字符 |
| `status` | 枚举 | `OPEN` / `REPLIED` / `CLOSED`(大写;提单 = `OPEN`,坐席回复 = `REPLIED`,任一侧关单 = `CLOSED`) |
| `createdAt` / `updatedAt` | timestamp | RFC 3339 毫秒 UTC |

### ticket_message(消息)

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `senderType` | 枚举 | `PLAYER` / `AGENT` / `SYSTEM` |
| `body` | string | 1–4000 字符 |
| `createdAt` | timestamp | RFC 3339 毫秒 UTC |

详情响应的 `messages` 按 `createdAt` 升序;提单的 `body` 即首条 `PLAYER` 消息。

### faq(知识条目)

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | id | UUIDv7,前缀 `faq_` |
| `question` / `answer` | string | 问题与答案 |
| `keywords` | string[] | 检索用关键词(可空) |

## 端点契约

### `POST /v1/support/tickets`

请求体(裸对象,信封外):

```json
{ "title": "...", "body": "...", "category": "PAYMENT" }
```

- `category` 可选,大写下划线枚举(接入方自定义集合,未知值容忍不拒);缺省 `OTHER`。
- 成功 `data` = ticket(`status=OPEN`)。
- 滥用防护:提单限流(默认每账号 5 次/分钟,超限 429 `RATE_LIMITED` + `Retry-After`)。

### `POST /v1/support/tickets/{ticketId}/messages`

请求体 `{ "body": "..." }` → `data` = ticket_message(`senderType=PLAYER`)。

- 工单 `CLOSED` → 409 `SUPPORT_TICKET_CLOSED`;非本人 / 不存在 → 404 `SUPPORT_TICKET_NOT_FOUND`。
- 追加成功后 `status` 保持 `OPEN`(玩家说话不改变状态;坐席回复才转 `REPLIED`)。

### `GET /v1/support/tickets`

`limit` 默认 20 最大 100,`cursor` 同 [announcement.md](./announcement.md);`items` = ticket,按 `updatedAt` 倒序。

### `GET /v1/support/tickets/{ticketId}`

`data` = `{ "ticket": {...}, "messages": [...] }`(消息升序)。

### `GET /v1/support/faq`

| 参数 | 说明 |
| --- | --- |
| `keyword` | 关键词,1–64 字符,命中 = question/answer/keywords 包含(不区分大小写);缺省 = 返回全部 |
| `limit` | 默认 20,最大 50 |

`data` = `{ "items": [faq...], "nextCursor": "" }`;无命中返回空 `items`(不是错误)。

## 错误码(域表)

| code | HTTP | retryable | 说明 |
| --- | --- | --- | --- |
| `SUPPORT_TICKET_NOT_FOUND` | 404 | false | 工单不存在或不属于当前玩家 |
| `SUPPORT_TICKET_CLOSED` | 409 | false | 工单已关闭,拒绝追加消息 |

## 客户端要求(各端 SDK)

- 提单成功后监听 `support.ticket_replied`([messages.md](./messages.md)):到达 → 拉取该工单详情增量展示;通道关闭 → 轮询或用户手动刷新兜底。
- `SUPPORT_TICKET_CLOSED` 不重试;429 按 `Retry-After` 退避([primitives.md](./primitives.md) 重试语义)。
- FAQ 检索空结果为常态,UI 呈现「无命中」而非错误。

---

## v1.1 提案(Draft,未冻结 · 2026-10-11)

> 来源:[features.md](../research/features.md) F43 上下文自动附带。目标是省掉客服「你的 ID 是多少」,且**不把上下文语义钉死**——SDK 与游戏各附各的键,服务端零解释透传。未评审冻结前不得实现。

### `POST /v1/support/tickets`(拟追加可选字段)

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `context` | object? | 扁平**开集** map,仅原始值(string / number / boolean);键 1–32 字符 `[a-zA-Z0-9_.]`、≤10 键、单值 ≤256 字符、整体 ≤1 KiB;超限 → `COMMON_INVALID_ARGUMENT` |

- **服务端零解释**:不白名单、不校验业务含义,原样存入并在工单详情 `ticket.context` 回显(坐席可见);未知键容忍不拒。
- 追加消息不携带 context(上下文属工单级,不属消息级)。
- 客户端要求:SDK **自动附带**已知事实面(`gameId` / `platform` / `appVersion`,来自 scope 与生命周期);游戏可追加自定义键(关卡 / 角色 / 复现步骤)。**自动附带键不可被业务代码覆写**。
- 零新错误码。

### 扩展与演进

| 可能的扩展 | 兼容处理方式 |
| --- | --- |
| 新上下文键(任意业务字段) | 游戏自定,开集 map,无需契约变更 |
| 某键升级为结构化字段(如设备信息) | v1.2 加可选字段,`context` 旧键保留兼容读取,不删不改 |
| 附件 / 日志上传(F44) | 独立端点提案;**不用 `context` 传二进制**(本契约已明示附件不覆盖);诊断日志走 diagnostics 自有端点 |
| 坐席侧筛选 / 聚合上下文 | 管理面能力(croupier),玩家契约不动 |
