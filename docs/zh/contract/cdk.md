# 兑换码契约（CDK）——礼包/兑换码

> 状态：**Frozen v1** · F62 · 2026-11-01。域前缀 `CDK` 已注册（[errors.md](./errors.md)）；本文档是 F62 服务端（默认 Provider **voucher**）与各平台 SDK 的一致性基准。冻结后只加不改；破坏性变更升版本（见 [versioning.md](./versioning.md)）。

## 能力与范围

- 能力域 `cdk`（`providers.CapCdk`），路由 `/v1/cdk/*`；默认 Provider **voucher**，可换可关。
- 一个码**每账号兑换一次**（幂等键=账号+码）。码可单次（`maxRedemptions=1`）或限次（计数）；语义归 Provider。
- 码生成、批量创建、兑换报表属管理面/接入方 live-ops 工具；网关只暴露玩家侧兑换端点。
- 反滥用由既有全局限流 + 失败退避覆盖。

## 端点总览（挂载于 `/v1/cdk/*`）

| 端点 | 认证 | 说明 |
| --- | --- | --- |
| `POST /v1/cdk/redeem` | Bearer | 兑换一个码（按账号+码幂等） |

请求体：`{ "code": "..." }`。成功响应 `data` = `{ "code", "grant", "repeated" }`。

## 数据模型

### code（管理面注册）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `value` | string | 码字符串 |
| `grant` | string | 发放描述（语义归接入方） |
| `maxRedemptions` | int | 总允许兑换次数（≥1） |

### redemption result

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `code` | string | 已兑换码 |
| `grant` | string | 发放描述回显 |
| `repeated` | bool | 该账号此前已兑换过此码 |

## 端点契约

### `POST /v1/cdk/redeem`

- 未知码 → 404 `CDK_NOT_FOUND`。
- 限次已达 → 409 `CDK_EXHAUSTED`。
- 同账号+码重复 → 200 且 `repeated:true`，不二次发放。
- 发放钩子由 Provider 构造注入；发放失败 → 503 `COMMON_UNAVAILABLE`，码保持可兑换。

## 推送事件

`cdk.redeemed`（载荷 `{ "code": "..." }`）——提示；不含敏感数据（兑换结果经兑换响应/接入方报表拉取）。

## 错误码域表（前缀 `CDK`，errors.md v9）

| code | HTTP | retryable | 说明 |
| --- | --- | --- | --- |
| `CDK_NOT_FOUND` | 404 | false | 码不存在或未注册 |
| `CDK_EXHAUSTED` | 409 | false | 码兑换限次已达 |

## 客户端要求

- 能力未接入（501 `COMMON_CAPABILITY_DISABLED`）→ 兑换返回 null；隐藏兑换 UI。
