[English](./errors.en.md) | [中文](./errors.md)

# 错误契约

> 状态:Frozen v1(2026-10-08,M0 跨端评审冻结;错误码表 **v8**——v2 追加 M2 域码,v3 追加 REALNAME 时段/额度码,v4 追加 M5 PAYMENT 域码,v5 追加 AUTH 注销码,v6 追加 OAUTH 域码,v7 追加 SUPPORT 附件码(2026-10-12),v8 追加 SUPPORT 举报/申诉码(2026-10-13))。跨 Unity / UE / Cocos / Godot 的错误一致性是 Universal SDK 的核心价值:同一 `code` 在各端映射为同一枚举、同一重试语义。

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

`COMMON` / `AUTH` / `SESSION` / `SCOPE` / `RATE` / `ANNOUNCEMENT` / `SUPPORT` / `ASSISTANT` / `PAYMENT` / `REALNAME` / `CONFIG` / `APP` / `DIAG` / `OAUTH`

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
| AUTH_ACCOUNT_PENDING_DELETE | 403 | false | 账号注销宽限期内,登录/绑定被拒(M1 auth.md v1.1 冻结新增) |
| AUTH_DELETE_TOKEN_INVALID | 401 | false | 注销撤销 token 缺失/失效/过期(M1 auth.md v1.1 冻结新增) |
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
| OAUTH_PROVIDER_UNKNOWN | 400 | false | 提供方未注册或未开启(oauth.md v1 冻结新增) |
| OAUTH_IDENTITY_INVALID | 401 | false | 第三方凭证验证失败/过期/伪造;不区分无效与过期,防枚举(oauth.md v1 冻结新增) |
| OAUTH_IDENTITY_TAKEN | 409 | false | 该第三方身份已绑定其他账号(oauth.md v1 冻结新增) |
| SUPPORT_ATTACHMENT_TOO_LARGE | 413 | false | 附件超单文件上限(缺省 10 MiB;support.md v1.1 冻结新增) |
| SUPPORT_ATTACHMENT_LIMIT | 409 | false | 工单附件数达上限(缺省 3;support.md v1.1 冻结新增) |
| SUPPORT_PENALTY_NOT_FOUND | 404 | false | 处罚不存在或不属于当前玩家;不泄露存在性(support.md v1.1 F45 冻结新增) |
| SUPPORT_APPEAL_EXISTS | 409 | false | 该处罚已有进行中申诉,每处罚至多一个(support.md v1.1 F45 冻结新增) |

各域业务错误码随该域契约冻结;本表 code 一经冻结不得改语义。

> **注册保留码(当前无触发点)**:`REALNAME_REQUIRED` / `REALNAME_REJECTED` / `REALNAME_PENDING_REVIEW` / `REALNAME_CURFEW_BLOCKED` / `REALNAME_CHARGE_BLOCKED` / `SESSION_CONFLICT` / `COMMON_PERMISSION_DENIED` / `CONFIG_NOT_FOUND` / `APP_VERSION_UNSUPPORTED` 已注册,当前网关均无触发点;客户端按未知码兜底分支处理(下方逐码说明保留理由)。
>
> **保留理由(巡检代决,2026-10-15;原「错误化触发待设计拍板」结案)**:判定类结果一律走
> 200 载荷,不走错误码——verify / curfew / charge-check 是**判定端点**,返回 403 会把
> 「查一下能不能充」变成异常路径;且 200 载荷(status.state + isMinor +
> curfew.playable + charge-check.allowed)能同时表达五种实名态,单个 403 码不能。
> 逐码:五个 `REALNAME_*` 见 [realname.md](./realname.md) 判定载荷;`SESSION_CONFLICT`
> 属异地踢出等**玩家可见登录策略**(本仓契约采多端并存:设备上限 5 + 解绑吊销 +
> 重放另走 `AUTH_REFRESH_REUSED`),单会话策略接入方把设备上限配成 1 即可;
> `COMMON_PERMISSION_DENIED` 各域一律用更具体的域码且不泄露存在性(工单/公告归属他人
> 一律 `*_NOT_FOUND`);`CONFIG_NOT_FOUND` 见 [config.md](./config.md)(v1.1 单键查询
> 预留);`APP_VERSION_UNSUPPORTED` 见 [app.md](./app.md)(版本判定走 forceUpdate)。
> 这九条由 `tools/contractgen` 的 `TestErrorCodesWiredOrReserved` 逐条核对:新码忘接线,
> 或接线后忘从本注记摘掉,门禁即失败。

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

---

## 扩展预留(错误码登记口径,2026-10-11 补注)

| 可能的扩展 | 兼容处理方式 |
| --- | --- |
| 新增错误码 | **兼容**——客户端必须容忍未知 code 走兜底分支([versioning.md](./versioning.md) 硬性要求);新码入表随域契约冻结 |
| 新增域前缀 | 兼容;新能力域 = 新前缀,注册于本表 |
| 废弃错误码 | 服务端停发,表内**保留不删、语义不重用**;客户端枚举可保留 |
| 已有码的 HTTP 映射 / retryable / 语义变更 | 破坏性 = 升版 |
