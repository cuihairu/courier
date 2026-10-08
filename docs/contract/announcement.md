# 公告契约(Announcement)

> 状态:**Frozen v1** · 里程碑 M2 · 冻结日期 2026-10-08。
> 域前缀 `ANNOUNCEMENT` 已注册([errors.md](./errors.md));本文档是 M2 服务端(默认供应商 **herald**)与各端 SDK 的一致性依据,冻结后只加不改,破坏性变更升版本(见 [versioning.md](./versioning.md))。

## 能力与范围

- 能力域 `announcements`(`providers.CapAnnouncements`),路由 `/v1/announcements/*`;默认供应商 **herald**,可换可关(Provider 原则:默认提供,可换可关)。
- 本契约是**玩家侧只读投影**:运营发布走 Provider 管理面(接入方运营系统/后台),不经网关玩家侧——网关只读投影,天然免疫越权写。
- 实时推送不经本域端点:统一走 [messages.md](./messages.md) 通道(`announcement.published` 事件,data 与本文档 DTO 同构)。
- 本契约**不覆盖**:公告管理/编辑/删除(Provider 管理面)、本地化多语(title/body 单语,接入方多语经 extras 或管理面分条)。

## 端点总览(挂载 `/v1/announcements/*`)

| 端点 | 认证 | 说明 |
| --- | --- | --- |
| `GET /v1/announcements` | Bearer | 可见公告列表(分页,`publishedAt` 倒序) |
| `GET /v1/announcements/{id}` | Bearer | 公告详情 |

- 认证与 scope header 要求同 [auth.md](./auth.md)(`Authorization: Bearer` + `X-Courier-Game-Id` / `X-Courier-Env`);缺失 → 对应通用码。
- 成功响应 HTTP 200,信封与分页见 [primitives.md](./primitives.md)(`items` + `nextCursor`,`nextCursor` 空/缺省 = 末页)。

## 数据模型

### announcement(公告)

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | id | 服务端生成,UUIDv7,前缀 `ann_` |
| `title` | string | 1–120 字符 |
| `body` | string | 1–4000 字符 |
| `severity` | 枚举 | `INFO` / `WARNING` / `CRITICAL`(大写;玩家侧 UI 配色分级) |
| `startAt` | timestamp? | 可见期起点(缺省 = 发布即可见) |
| `endAt` | timestamp? | 可见期终点(缺省 = 不过期) |
| `publishedAt` | timestamp | 发布时间,列表排序键 |

- **可见性**:`publishedAt <= now` 且 `now ∈ [startAt?, endAt?]`(缺省边界视为无界);仅可见公告进入列表与详情——过期/未到 `startAt` 一律 `ANNOUNCEMENT_NOT_FOUND`。
- 时间戳一律 RFC 3339 毫秒 UTC 原文([primitives.md](./primitives.md));未知字段容忍。

## 端点契约

### `GET /v1/announcements`

查询参数:

| 参数 | 说明 |
| --- | --- |
| `limit` | 每页条数,默认 20,最大 100;越界 → `COMMON_INVALID_ARGUMENT` |
| `cursor` | 分页游标(上页 `nextCursor` 原样回传;非法 → `COMMON_INVALID_ARGUMENT`) |

响应 `data`:

```json
{
  "items": [ { "id": "ann_...", "title": "...", "body": "...", "severity": "INFO",
               "startAt": "2026-10-08T00:00:00.000Z", "endAt": null,
               "publishedAt": "2026-10-08T00:00:00.000Z" } ],
  "nextCursor": ""
}
```

### `GET /v1/announcements/{announcementId}`

- 命中且在可见期 → `data` = 单个 announcement;
- 不存在 / 不可见 → 404 `ANNOUNCEMENT_NOT_FOUND`。

## 错误码(域表)

| code | HTTP | retryable | 说明 |
| --- | --- | --- | --- |
| `ANNOUNCEMENT_NOT_FOUND` | 404 | false | 公告不存在或不在可见期 |

其余(认证、scope、限流、能力关闭)一律通用码,见 [errors.md](./errors.md)。

## 客户端要求(各端 SDK)

- 列表/详情失败按信封 `error` 处理;`ANNOUNCEMENT_NOT_FOUND` 不重试。
- 通道关闭([messages.md](./messages.md) 501 `COMMON_CAPABILITY_DISABLED`)时,公告完全以**拉取**(本域)兜底——M2 验收「推送通道关闭时拉取兜底可用」。
- 事件 `announcement.published` 到达时,客户端可增量更新列表或整体重拉;data 结构与本域 DTO 同构,不得另造字段。
