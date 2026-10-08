# 推送通道契约(Messages)

> 状态:**Frozen v1** · 里程碑 M2 · 冻结日期 2026-10-08。
> 本域**无专属错误码前缀**(通道层错误全复用通用码,见 [errors.md](./errors.md) 说明);本文档是 M2 服务端(默认供应商 **chirp**)与各端 SDK 的一致性依据,冻结后只加不改,破坏性变更升版本(见 [versioning.md](./versioning.md))。

## 能力与范围

- 能力域 `messages`(`providers.CapMessages`),路由 `/v1/messages/*`;默认供应商 **chirp**,可换可关。
- **关闭 = 仅拉取兜底**:能力未配置 / 显式关闭 → `501 COMMON_CAPABILITY_DISABLED`,各端 SDK 转纯拉取模式(公告轮询 [announcement.md](./announcement.md)、客服手动/定时刷新 [support.md](./support.md)),**功能不残废**。
- **会话复用**:单连接承载全部域事件(公告、客服、后续域),不为每个域各开一条——移动端连接是稀缺资源。
- 传输选型:**SSE**(text/event-stream)——标准库可全实现、HTTP 语义免费复用(scope/trace/限流/信封)、Unity `UnityWebRequest` 可流式读;WebSocket 留后续扩展(双端需写时升版本)。
- 本契约**不覆盖**:离线推送(APNs/FCM,后续域)、事件补发(M2 语义见下)。

## 端点总览(挂载 `/v1/messages/*`)

| 端点 | 认证 | 说明 |
| --- | --- | --- |
| `GET /v1/messages/stream` | Bearer | SSE 事件流(长连接) |

- Bearer 必选:通道为**已认证玩家**服务,匿名 → 401 `COMMON_UNAUTHENTICATED`。
- scope header 同 [auth.md](./auth.md);限流按连接建立次数计(不在流内计)。
- 能力关闭 → `501 COMMON_CAPABILITY_DISABLED`(降级信号,SDK 据此安全关闭推送 UI)。

## 流协议

响应头:`Content-Type: text/event-stream`(SSE 标准,[WHATWG HTML §server-sent-events](https://html.spec.whatwg.org/multipage/server-sent-events.html))。

帧格式(SSE 标准,非 JSON 信封):

```
id: 42
event: announcement.published
data: {"id":"ann_...","title":"...","body":"...","severity":"INFO","publishedAt":"..."}

```

- `event` = 事件 type,两段小写 `<domain>.<event>`,多词下划线(同 [events.md](./events.md) 命名规则)。
- `data` = 单行 JSON,**与该域拉取端点的 DTO 同构**——客户端零新类型,只复用域 DTO。
- `id` = 连接内单调递增序号(调试/日志用;**M2 不做断线补发**,`Last-Event-ID` 头可忽略)。
- 心跳:服务端每 **25s** 发注释帧 `: ping\n\n`(穿代理保活);客户端不产生业务语义。
- 断线语义:只保证**在线即达**;离线期间事件不补,靠拉取兜底——通道是「优化」,不是「依赖」。

## 事件登记(M2)

| event | data(与拉取 DTO 同构) | 触发 |
| --- | --- | --- |
| `announcement.published` | announcement([announcement.md](./announcement.md)) | 运营发布可见公告 |
| `support.ticket_replied` | `{ "ticketId": "tkt_..." }` | 坐席回复/关单工单;客户端拉取详情增量展示 |

M3 追加(与拉取 DTO 不同构:只带版本号,客户端重拉比对——同 config/branding 契约定):

| event | data | 触发 |
| --- | --- | --- |
| `config.updated` | `{ "configVersion": 7 }`([config.md](./config.md)) | 管理面发布新配置版本 |
| `branding.updated` | `{ "version": 3 }`([branding.md](./branding.md)) | 管理面变更品牌物料 |

新事件随域契约冻结在此登记;`type` 冻结后不改,废弃事件服务端停发、类型保留。

## 语义规则

1. **事件可能重复/乱序**:通道至多一次(at-most-once);客户端按域 DTO `id` 幂等去重。
2. **事件不是权威**:以拉取端点为准(先到的事件只是「该去拉了」的提示,支持直接渲染但校验可见期)。
3. **连接生命周期**:服务端可随时断流(发版/迁移/负载);客户端指数退避重连(建议 1s 起、上限 60s,`Retry-After` 头优先)。
4. **认证失效**:流内 token 过期不中断已有连接(M2);新连接按当前 token 校验。

## 错误(通道层,全通用码)

| 场景 | 响应 |
| --- | --- |
| 未认证 | 401 `COMMON_UNAUTHENTICATED`(结构化信封) |
| 能力关闭 | 501 `COMMON_CAPABILITY_DISABLED` |
| 限流 | 429 `RATE_LIMITED` + `Retry-After` |
| scope 缺失/不一致 | 400 `COMMON_INVALID_ARGUMENT` / `SCOPE_MISMATCH` |

## 客户端要求(各端 SDK)

- 通道健康 = 「有」或「没有」两态;没有 → 纯拉取模式,不半死轮询。
- 收到事件先去重(`id`/域 `id`),再按域 DTO 解析;未知 `event` type **必须容忍**(忽略,同契约 versioning.md 未知容忍原则)。
- 重连退避见语义规则 3;应用切后台可主动断流省电,回前台重连(生命周期事件见 [events.md](./events.md))。
