# 错误契约

> 状态:Frozen v1(2026-10-08,M0 跨端评审冻结;错误码表 **v4**——v2 追加 M2 域码,v3 追加 REALNAME 时段/额度码,v4 追加 M5 PAYMENT 域码)。跨 Unity / UE / Cocos / Godot 的错误一致性是 Universal SDK 的核心价值:同一 `code` 在各端映射为同一枚举、同一重试语义。

## 错误体

```json
{
  "error": {
    "code": "AUTH_TOKEN_EXPIRED",
    "message": "session expired",
    "retryable": false
  },
  "traceId": "4bf92f3577b34da6a3ce929d0e0e4736"
}
```

- `code`:机器可读,`<域>_<原因>` 大写下划线,各端映射为枚举后做分支判断的唯一依据。
- `message`:人读,可随语言变化,**不得**用于客户端分支判断。
- `retryable`:`true` 时客户端可按退避策略自动重试;配合 `Retry-After` header(秒)。

## 域前缀注册表

`COMMON` / `AUTH` / `SESSION` / `SCOPE` / `RATE` / `ANNOUNCEMENT` / `SUPPORT` / `ASSISTANT` / `PAYMENT` / `REALNAME` / `CONFIG` / `APP` / `DIAG`

新域必须先在此注册前缀,再定义具体码。

## HTTP 状态映射

| HTTP | 语义 |
| --- | --- |
| 400 | 参数/格式错误 |
| 401 | 未认证(凭证无效/过期/吊销) |
| 403 | 已认证但无权限/被拒(实名未过、账号禁用) |
| 404 | 资源不存在 |
| 409 | 冲突(会话/状态竞争) |
| 426 | 版本过旧需升级 |
| 429 | 限流 |
| 500 | 内部错误 |
| 501 | 能力未开启(降级信号,见下) |
| 503 | 依赖不可用/维护中 |

## 错误码表 v1(基元冻结集)

| code | HTTP | retryable | 说明 |
| --- | --- | --- | --- |
| COMMON_INTERNAL | 500 | true | 内部错误 |
| COMMON_INVALID_ARGUMENT | 400 | false | 参数错误 |
| COMMON_UNAUTHENTICATED | 401 | false | 未认证 |
| COMMON_PERMISSION_DENIED | 403 | false | 无权限 |
| COMMON_NOT_FOUND | 404 | false | 资源不存在 |
| COMMON_CAPABILITY_DISABLED | 501 | false | **能力未接入/已关闭(降级信号)** |
| COMMON_UNAVAILABLE | 503 | true | 依赖不可用 |
| RATE_LIMITED | 429 | true | 限流,遵守 Retry-After |
| SCOPE_MISMATCH | 400 | false | scope 不一致(见 scope.md) |
| AUTH_INVALID_CREDENTIALS | 401 | false | 凭证错误 |
| AUTH_TOKEN_EXPIRED | 401 | false | access token 过期(应 refresh) |
| AUTH_TOKEN_REVOKED | 401 | false | token 被吊销(应重登) |
| AUTH_REFRESH_REUSED | 401 | false | refresh token 重放(安全事件,应重登) |
| AUTH_ACCOUNT_DISABLED | 403 | false | 账号禁用 |
| AUTH_DEVICE_LIMIT | 403 | false | 设备数超限 |
| AUTH_EMAIL_TAKEN | 409 | false | 邮箱已被注册(M1 auth.md 冻结新增) |
| SESSION_CONFLICT | 409 | false | 会话冲突(异地踢出等) |
| ANNOUNCEMENT_NOT_FOUND | 404 | false | 公告不存在或不在可见期(M2 announcement.md 冻结新增) |
| SUPPORT_TICKET_NOT_FOUND | 404 | false | 工单不存在或不属于当前玩家(M2 support.md 冻结新增) |
| SUPPORT_TICKET_CLOSED | 409 | false | 工单已关闭,拒绝追加消息(M2 support.md 冻结新增) |
| REALNAME_REQUIRED | 403 | false | 需要实名(未提交) |
| REALNAME_REJECTED | 403 | false | 实名未通过 |
| REALNAME_PENDING_REVIEW | 403 | false | 待复核(降级链全挂时的安全态) |
| REALNAME_CURFEW_BLOCKED | 403 | false | 不可玩时段(M2 后段 realname.md 冻结新增;data 可带 nextWindowAt) |
| REALNAME_CHARGE_BLOCKED | 403 | false | 超出充值额度(M2 后段 realname.md 冻结新增;data 可带限额字段) |
| CONFIG_NOT_FOUND | 404 | false | 配置键不存在 |
| APP_MAINTENANCE | 503 | false | 维护中(payload 带预计恢复时间,可选) |
| APP_VERSION_UNSUPPORTED | 426 | false | 版本过旧(payload 带下载地址,可选) |
| PAYMENT_SKU_NOT_FOUND | 404 | false | SKU 不存在或不可购(M5 payment.md 冻结新增) |
| PAYMENT_ORDER_NOT_FOUND | 404 | false | 订单不存在或不属于当前玩家(M5 payment.md 冻结新增) |
| PAYMENT_RISK_REJECTED | 403 | false | 风控前置拒绝:大额/异常频次(M5 payment.md 冻结新增) |
| PAYMENT_INVALID_SIGNATURE | 403 | false | 渠道回调签名校验失败;伪造一律拒绝(M5 payment.md 冻结新增) |
| PAYMENT_ORDER_STATE | 409 | false | 订单状态机非法转移(M5 payment.md 冻结新增) |

各域业务错误码随该域契约冻结;本表 code 一经冻结不得改语义。

> **messages 域(推送通道,M2 [messages.md](./messages.md))不设专属前缀**:通道层
> 错误全部复用通用码(关闭 `COMMON_CAPABILITY_DISABLED`、未认证 `COMMON_UNAUTHENTICATED`、
> 限流 `RATE_LIMITED`);推送事件的业务语义由事件 `type` 与 data 承载,不是错误。

## 降级信号:COMMON_CAPABILITY_DISABLED

Provider 未接入或被配置关闭时,网关返回 `501 COMMON_CAPABILITY_DISABLED`。这是**能力关闭**,不是故障:

- SDK 将其映射为「能力不可用」状态(查询接口返回空/未启用,不抛异常路径)。
- 接入方据此可安全地隐藏对应 UI,而不是处理报错。
- 这是「不因未接某供应商而残废」原则的落点:缺 Provider = 该能力安静地不存在。

## 客户端跨端映射(示例)

```csharp
if (error.Code == ErrorCode.TokenExpired) { /* refresh */ }
```

```cpp
if (error.code == ErrorCode::TokenExpired) { /* refresh */ }
```

```typescript
if (error.code === ErrorCode.TokenExpired) { /* refresh */ }
```

各端枚举命名随本端语言习惯,取值集合与本文档一一对应;契约测试保证不漂移。
