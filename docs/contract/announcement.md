# 公告契约(Announcement)

> 状态:**Frozen v1.1** · 里程碑 M2(v1 冻结 2026-10-08;v1.1 两批追加:2026-10-12 定向过滤 F35,2026-10-13 多语内容 F76)。
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
| `GET /v1/announcements/{announcementId}` | Bearer | 公告详情 |

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
| `filters` | object? | 定向过滤维度 map(v1.1):`维度 → string[]` **开集**;服务端匹配依据,只读回显;缺省 = 无过滤,全量可见 |

- **可见性**:`publishedAt <= now` 且 `now ∈ [startAt?, endAt?]`(缺省边界视为无界);仅可见公告进入列表与详情——过期/未到 `startAt` 一律 `ANNOUNCEMENT_NOT_FOUND`。
- 时间戳一律 RFC 3339 毫秒 UTC 原文([primitives.md](./primitives.md));未知字段容忍。

## 端点契约

### `GET /v1/announcements`

查询参数:

| 参数 | 说明 |
| --- | --- |
| `limit` | 每页条数,默认 20,最大 100;越界 → `COMMON_INVALID_ARGUMENT` |
| `cursor` | 分页游标(上页 `nextCursor` 原样回传;非法 → `COMMON_INVALID_ARGUMENT`) |
| `platform` / `appVersion` / `region` / `language` | 可选定向维度(v1.1);**缺省维度不参与过滤**;未知查询参数容忍忽略(基元容忍规则) |

**定向过滤匹配规则(v1.1)**:`filters` 中出现的每个维度,若请求携带对应查询参数且值不在集合内 → 该公告对该请求不可见(列表不返回,详情 404 `ANNOUNCEMENT_NOT_FOUND`);请求缺该维度参数 → 该维度不排除。复用 [config.md](./config.md) 已冻结的玩家侧投影语义(缺省维度跳过、同请求稳定、条件 DSL 不进玩家契约),不另造一套过滤机制;`filters` 内容 SDK 不解释、不判断、不校验;零新错误码。

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

---

## v1.1 冻结记录(2026-10-12)

F35 公告定向过滤([features.md](../research/features.md) F35)按 2026-10-11 提案冻结:`filters` 开集 map + 四个可选查询维度,匹配规则见上文「端点契约」;复用 config 投影语义,零新错误码。原提案「扩展与演进」表随冻结生效:

| 可能的扩展 | 兼容处理方式 |
| --- | --- |
| 新过滤维度(渠道 / 大区细分 / 自定义标签) | 管理面新维度 key + 客户端新查询参数,**wire 兼容不升版**;旧 SDK 不传新参数 → 该维度自动不参与过滤(缺省跳过语义兜底) |
| 定向命中集变化后的刷新 | 复用 `announcement.published` 事件,不动列表契约 |
| 灰度 / 分桶 / 百分比放量 | 管理面语义(同 config 灰度),玩家侧只见稳定投影 |
| 命中解释 / 调试 | 管理面能力,玩家侧不暴露——避免泄露运营定向规则 |

---

## 多语内容(F76,Frozen · 批次 57,2026-10-13)

> 来源:[features.md](../research/features.md) F76,与已冻结的 `language` 过滤维度配合。**状态:Frozen**(2026-10-13,批次 57 冻结;原 Draft 提案 2026-10-11 批次 48,按原文冻结不改语义)。

### 数据模型

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `language` | string? | RFC 5646 标签(`zh-CN` / `en` / `ja`);缺省 = 不限定语种,全量可见 |
| `groupKey` | string? | 管理面关联同内容多语条目;客户端不解释,仅供「同公告去重」类产品逻辑 |

- 模型:**一条公告 = 单语内容**;多语 = 管理面分条发布(v1「接入方多语经管理面分条」口径的契约化),`title` / `body` 字段类型不变,零破坏。
- 客户端:`language` 查询参数由 SDK **自动附带**(取系统/应用 locale,游戏可显式覆写);服务端按 `language` 维度走 filters 同一匹配规则。
- 自动附带口径(SDK 实现定死):接入方在初始化配置提供 locale(`CourierConfig.language`,引擎侧系统语言由接入方读入;SDK 本体零引擎 API,测试可注入),列表请求未显式带 `language` 查询参数时自动追加;显式传入 = 覆写;配置缺省 = 不附带(与 v1 行为一致)。

### 扩展与演进

| 可能的扩展 | 兼容处理方式 |
| --- | --- |
| 多语同时可见(单条多语对象) | 破坏性(`title` 变对象),走 v2,永不原地改 |
| 语种回退(`zh-CN` 缺失回退 `zh`) | 管理面分条时自行兜底(发布兜底语种条目);客户端契约不加回退逻辑 |
| 新过滤维度(渠道 / 大区细分 / 自定义标签) | 已随 v1.1 定向过滤冻结(上文冻结记录表):管理面新维度 key + 客户端新查询参数,wire 兼容不升版 |

冻结生效:原提案「扩展与演进」表随冻结生效(多语同时可见 = 破坏性走 v2,永不原地改);`language` / `groupKey` 为加法可选字段,旧客户端容忍未知字段兼容读取;零新错误码(errors 版本不动);匹配不新增机制——`language` 查询参数复用已冻结的 filters 匹配规则,行内 `language` 字段是语种声明元数据,不参与匹配(可见性仍由 `filters` 决定)。
