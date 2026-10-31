# 邮件契约（Mail）——运营侧→玩家邮件

> 状态：**Frozen v1** · F62 · 2026-11-01。域前缀 `MAIL` 已注册（[errors.md](./errors.md)）；本文档是 F62 服务端（默认 Provider **carrier**）与各平台 SDK 的一致性基准。冻结后只加不改；破坏性变更升版本（见 [versioning.md](./versioning.md)）。

## 能力与范围

- 能力域 `mail`（`providers.CapMail`），路由 `/v1/mail/*`；默认 Provider **carrier**，可换可关。
- 这是**玩家侧投影**：运营经 Provider 管理面发邮件（接入方运营系统），不经网关玩家侧。
- 邮件只是附件的**载体**：不引入第二价值单位；发放走接入方发放钩子（附件描述发放内容）。
- 实时推送走 [messages.md](./messages.md) 通道（`mail.updated` 事件）；数据为最小提示，列表端点为准。

## 端点总览（挂载于 `/v1/mail/*`）

| 端点 | 认证 | 说明 |
| --- | --- | --- |
| `GET /v1/mail` | Bearer | 我的邮件列表（个人+全量，最新在前） |
| `GET /v1/mail/{mailId}` | Bearer | 邮件详情 |
| `POST /v1/mail/{mailId}/claim` | Bearer | 领取附件（按账号+邮件幂等） |

- 认证与 scope 头同 [auth.md](./auth.md)；信封与分页同 [primitives.md](./primitives.md)。

## 数据模型

### mail

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | id | 服务端生成，UUIDv7，前缀 `mail_` |
| `title` | string | 1–120 字符 |
| `body` | string | 1–4000 字符 |
| `attachments` | array | 不透明物品描述（type/amount/currency；无第二价值单位） |
| `createdAt` | timestamp | 发送时间；列表排序键（最新在前） |
| `expiresAt` | timestamp? | 可选过期；缺省=不过期 |

### attachment

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | string | 接入方定义物品 ID |
| `type` | string | 发放种类（开集；语义归接入方） |
| `amount` | int64 | 数量 |
| `currency` | string? | 可选货币标签（物品发放为空） |

## 端点契约

### `GET /v1/mail`

查询：`limit`（默认 20，最大 100）、`cursor`（不透明）。响应 `data` = 调用方可见邮件（全量+个人）分页，`createdAt` 倒序。

### `GET /v1/mail/{mailId}`

命中且可见 → 200。不存在/非本人/过期 → 404 `MAIL_NOT_FOUND`（不泄露存在性）。

### `POST /v1/mail/{mailId}/claim`

幂等领取：重复领取返回同结果，不二次发放。发放钩子由 Provider 构造注入；发放失败 → 503 `COMMON_UNAVAILABLE`，邮件保持未领（钩子成功前不标记领取）。

## 推送事件

`mail.updated`（载荷 `{ "mailId": "mail_..." }`）——「该去拉列表了」的提示；列表为准。

## 错误码域表（前缀 `MAIL`，errors.md v9）

| code | HTTP | retryable | 说明 |
| --- | --- | --- | --- |
| `MAIL_NOT_FOUND` | 404 | false | 邮件不存在或非当前玩家 |

## 客户端要求

- 能力未接入（501 `COMMON_CAPABILITY_DISABLED`）→ 查询返回 null；隐藏邮件 UI。
- 推送为提示，轮询为准。
