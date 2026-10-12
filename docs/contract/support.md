# 客服契约(Support)

> 状态:**Frozen v1.1** · 里程碑 M2(v1 冻结 2026-10-08;v1.1 追加三批:2026-10-12 工单上下文 F43 与工单附件 F44,2026-10-13 举报与处罚申诉 F45)。
> 域前缀 `SUPPORT` 已注册([errors.md](./errors.md));本文档是 M2 服务端(默认供应商 **croupier**)与各端 SDK 的一致性依据,冻结后只加不改,破坏性变更升版本(见 [versioning.md](./versioning.md))。

## 能力与范围

- 能力域 `support`(`providers.CapSupport`),路由 `/v1/support/*`;默认供应商 **croupier**,可换可关。
- 玩家侧:提单、追加消息、查列表/详情、FAQ 检索。**坐席侧**(回复、关单、分派)走 croupier 内部管理面,不经网关——网关只有玩家可读可写自己的工单。
- 坐席回复的实时可见走 [messages.md](./messages.md) 通道(`support.ticket_replied` 事件);通道关闭时靠拉取详情兜底。
- 本契约**不覆盖**:坐席工作台、工单分派策略(附件已由下文「工单附件」节覆盖,F44;v1 冻结时明示不覆盖,后按提案冻结为扩展节)。

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
| `context` | object? | 工单上下文(v1.1):扁平**开集** map,仅原始值;服务端零解释透传,提单时写入、详情 `ticket.context` 原样回显;缺省 = 无上下文 |
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
{ "title": "...", "body": "...", "category": "PAYMENT", "context": { "level": 42, "device": "Pixel 8" } }
```

- `category` 可选,大写下划线枚举(接入方自定义集合,未知值容忍不拒);缺省 `OTHER`。
- `context` 可选(v1.1):扁平**开集** map,仅原始值(string / number / boolean);键 1–32 字符 `[a-zA-Z0-9_.]`、≤10 键、单值 ≤256 字符、整体 ≤1 KiB;超限 → `COMMON_INVALID_ARGUMENT`(400)。
- **服务端零解释**:不白名单、不校验业务含义,原样存入并在工单详情 `ticket.context` 回显(坐席可见);未知键容忍不拒。追加消息不携带 context(上下文属工单级,不属消息级)。
- 零新错误码。
- 成功 `data` = ticket(`status=OPEN`,含 `context` 回显)。
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
- 提单时 SDK **自动附带**已知事实面(`gameId` / `platform` / `appVersion`,来自 scope 与生命周期);游戏可追加自定义键(关卡 / 角色 / 复现步骤)。**自动附带键不可被业务代码覆写**(v1.1)。

---

## v1.1 冻结记录(2026-10-12)

F43 工单上下文附带([features.md](../research/features.md) F43)按 2026-10-11 提案冻结:`context` 扁平开集 map(上文「端点契约」与 ticket 数据模型)、服务端零解释透传、SDK 自动附带事实面(上文「客户端要求」)、零新错误码。原提案「扩展与演进」表随冻结生效:

| 可能的扩展 | 兼容处理方式 |
| --- | --- |
| 新上下文键(任意业务字段) | 游戏自定,开集 map,无需契约变更 |
| 某键升级为结构化字段(如设备信息) | v1.2 加可选字段,`context` 旧键保留兼容读取,不删不改 |
| 附件 / 日志上传(F44) | 独立端点(下文「工单附件」节,已冻结);**不用 `context` 传二进制**;诊断日志走 diagnostics 自有端点 |
| 坐席侧筛选 / 聚合上下文 | 管理面能力(croupier),玩家契约不动 |

---

## 举报与处罚申诉(F45,Frozen · 批次 56,2026-10-13)

> 来源:[features.md](../research/features.md) F45。**处罚的判定与执行全在管理面**(croupier / GM / 接入方风控);本契约只是玩家侧只读投影 + 申诉入口——与公告/配置同模式,网关不执行风控。**状态:Frozen**(2026-10-13,批次 56 冻结;原 Draft 提案 2026-10-11 批次 48,按原文冻结不改语义)。

### 端点

| 端点 | 认证 | 说明 |
| --- | --- | --- |
| `POST /v1/support/reports` | Bearer | 提交举报 |
| `GET /v1/support/penalties` | Bearer | 我的处罚列表(分页,`issuedAt` 倒序) |
| `GET /v1/support/penalties/{penaltyId}` | Bearer | 处罚详情(含申诉状态) |
| `POST /v1/support/penalties/{penaltyId}/appeal` | Bearer | 提交申诉(每处罚至多一个进行中申诉) |

### 数据模型

**report(举报)**:`id`(`rpt_` 前缀)、`targetType`(`PLAYER` / `MESSAGE` / `TICKET`,开集可扩)、`targetId`、`reason`(`CHEATING` / `ABUSE` / `SPAM` / `INAPPROPRIATE` / `OTHER`,管理面可扩)、`detail?`(≤1000 字符)。受理即返回 `id`,**举报进展不向举报人反馈**(防骚扰设计)。

**penalty(处罚投影)**:`id`(`pen_`)、`type`(`MUTE` / `SUSPENSION` / `LEADERBOARD_BAN`,开集,客户端容忍未知)、`reason`(面向玩家的人类可读说明,不含内部与坐席信息)、`state`(`ACTIVE` / `EXPIRED` / `LIFTED`)、`issuedAt` / `expiresAt?`、`appeal?`(`{ "state": "PENDING" | "ACCEPTED" | "REJECTED", "submittedAt", "decidedAt?" }`)。

### 语义

- 申诉判定走管理面,结果以拉取为准(`appeal.state`);已有进行中申诉再提交 → 409 `SUPPORT_APPEAL_EXISTS`。
- 非本人 / 不存在处罚 → 404 `SUPPORT_PENALTY_NOT_FOUND`(不泄露存在性)。
- **新增 2 个错误码**:`SUPPORT_PENALTY_NOT_FOUND`(404,retryable=false)、`SUPPORT_APPEAL_EXISTS`(409,retryable=false)。
- v1 **不改变 auth 登录语义**:处罚是否拦截登录由接入方策略与管理面决定(见扩展节)。

### 扩展与演进

| 可能的扩展 | 兼容处理方式 |
| --- | --- |
| 新处罚类型 / 新举报理由 | 管理面扩集合;type 开集客户端容忍未知,reason 集合登记于本节即可 |
| 处罚拦截登录 | v1.2 可选语义提案(如 `AUTH_ACCOUNT_SUSPENDED` 联动);v1 不动 auth |
| 举报证据附件 | 与 F44 附件共用独立端点(`targetType` 维度泛化留 v1.2 提案),不塞 report |
| 跨游戏 / 公司级处罚 | penalty 增可选 `gameId?`(缺省 = 签发 scope)走加字段扩展,v1 仅当前 scope |
| 举报流转进度 | 不向举报人反馈(防骚扰),管理面内部流转;产品若需要可另提匿名化进度端点提案 |

冻结生效:原提案「扩展与演进」表随冻结生效(新处罚类型 / 新举报理由走管理面扩集合,`type` 开集客户端容忍未知,不升版本);新增错误码入 errors.md v8;v1 **不动 auth 登录语义**(处罚拦截登录留 v1.2 提案)。

---

## 工单附件(F44,Frozen · 批次 53,2026-10-12)

> 来源:[features.md](../research/features.md) F44 反馈+日志上传。v1 明示附件不覆盖、留新端点扩展,本节兑现。**网关不落盘二进制**:契约只定义「申请直传」的形状,存储本体挡在 Provider 后面(同公告/配置的投影模式)——换 S3 / OSS / 本地签名 URL,契约不动。**状态:Frozen**(2026-10-12,批次 53 冻结;原 Draft 提案 2026-10-11 批次 50,按原文冻结不改语义)。

### 端点

| 端点 | 认证 | 说明 |
| --- | --- | --- |
| `POST /v1/support/tickets/{ticketId}/attachments` | Bearer | 申请附件直传,返回短时效预签名 URL |

- 单端点设计:客户端拿 `uploadUrl` **直传存储**(HTTP `PUT`,不经网关),完成确认走存储→Provider 回调,玩家侧以工单详情为准——网关二进制零流量,天然免疫大文件打爆网关。

### 请求与响应

请求体 `{ "fileName", "contentType", "sizeBytes", "kind"? }`:

| 字段 | 说明 |
| --- | --- |
| `fileName` | 1–255 字符,展示用原文回显,服务端不解释 |
| `contentType` | MIME;须在管理面 allowlist 内(缺省 `image/png` / `image/jpeg` / `text/plain` / `application/zip` / `application/pdf`),不在 → `COMMON_INVALID_ARGUMENT` |
| `sizeBytes` | ≤ 10 MiB;超限 → 413 `SUPPORT_ATTACHMENT_TOO_LARGE` |
| `kind` | 可选枚举 `IMAGE` / `LOG` / `OTHER`(开集,客户端容忍未知) |

响应 `data`:`{ "attachmentId": "att_…", "uploadUrl", "method": "PUT", "headers": {…}, "expiresAt" }`——`uploadUrl` 为短时效预签名 URL(缺省 10 分钟,管理面可配),过期重新申请即可,**无恢复语义**。

### 语义

- 工单须归属当前玩家(否则 404 `SUPPORT_TICKET_NOT_FOUND`)且未关闭(409 `SUPPORT_TICKET_CLOSED`);每工单 ≤ 3 个附件,超限 → 409 `SUPPORT_ATTACHMENT_LIMIT`。
- 工单详情(已冻结 `GET /tickets/{id}`)追加 `attachments[]`(加法扩展,旧客户端容忍未知字段):`{ id, fileName, contentType, sizeBytes, kind?, createdAt, downloadUrl? }`——`downloadUrl` 同为短时效预签名,玩家只见自己的;坐席侧走 croupier 管理面。
- **新增 2 个错误码**(errors.md v7):`SUPPORT_ATTACHMENT_TOO_LARGE`(413,retryable=false)、`SUPPORT_ATTACHMENT_LIMIT`(409,retryable=false)。
- 红线:服务端与 SDK **零内容解释**(不扫描语义、不改写);病毒扫描/内容审核归管理面;附件与 `context`(F43 提案)互斥承载——二进制永不进 `context`。

### 扩展与演进

| 可能的扩展 | 兼容处理方式 |
| --- | --- |
| 举报证据附件(F45 已冻结) | 端点组泛化提案(`targetType` 维度,v1.2 一并),共用同一存储面与限额语义 |
| 存储供应商更换(S3 / OSS / 本地盘) | 预签名 URL 抽象已把存储挡在 Provider 后;契约只认「短时效直传 URL」形状,换存储 = 换 Provider 配置,契约不动 |
| 新附件类型(视频 / 大文件) | 管理面 allowlist 与限额配置扩;超大文件走 Provider 直传面,不进玩家契约(字段最小化) |
| 缩略图 / 预览 | Provider/管理面能力;玩家侧只认 `downloadUrl`,契约不加预览语义 |
| 附件删除 | v1 只增不删(工单关闭即整体不可再传);玩家侧删除入口如有合规需求走 v1.2 加端点提案 |
