# 支付契约(Payment)——形态开放

> 状态:**Frozen v1.1** · 里程碑 M5 · v1 冻结日期 2026-10-09;**v1.1(2026-10-09)追加推送事件**(`payment.paid` / `payment.delivered`,附加兼容变更,见「推送事件」)。
> 域前缀 `PAYMENT` 已注册([errors.md](./errors.md));本文档是 M5 服务端(默认供应商 **teller**)与各端 SDK 的一致性依据,冻结后只加不改,破坏性变更升版本(见 [versioning.md](./versioning.md))。

## 能力与范围

- 能力域 `payments`(`providers.CapPayments`),路由 `/v1/payments/*`;默认供应商 **teller**(自建订单/沙箱渠道),可换可关。
- 契约钉的是**订单-发货核心流**(下单 → 渠道回调 → 发货 → 对账),不假设钱包形态;渠道与钱包由接入方选择。

### 四形态(订单的发货目标,开放不预设)

| form | 语义 | 发货(DELIVERED)意味着 |
| --- | --- | --- |
| `DIRECT_PURCHASE` | 直购 | 道具/权益已发放到游戏 |
| `ACCOUNT_WALLET` | 账号钱包 | 余额已入账(账号维度) |
| `GAME_WALLET` | 游戏钱包 | 游戏内货币已入账(游戏维度) |
| `EXTERNAL_PAYMENT` | 外接支付 | 接入方自有商城订单已确认(本域仅登记对账) |

- **不做统一钱包假设**:余额怎么存、货币叫什么,都是接入方的事;契约只保证订单状态与金额事实可查可对账。
- 形态只是订单上的枚举标注与发货语义差异,不产生不同的端点或状态机。

## 安全边界(红线,实现不得绕过)

1. **服务端定价**:客户端下单请求体只有 `skuId`;金额/货币一律取自服务端 SKU 价格表——客户端报价不可信。
2. **回调只认渠道签名**:渠道→网关的 S2S 回调必须携带 `X-Payment-Signature`(对 raw body 的 HMAC-SHA256,密钥按渠道配置);签名缺失/不匹配一律 403,伪造签名**全部拒绝**。
3. **回调幂等**:同一订单重复回调(同状态)= 幂等受理(200),不二次发货;发货幂等性由接入方发货钩子兜底(契约要求按订单幂等)。
4. **订单归属校验**:非本人的订单一律 404 `PAYMENT_ORDER_NOT_FOUND`(不泄露存在性)。
5. **风控前置**:下单可经 RiskProvider 前置(大额/异常频次);拒绝 → 403 `PAYMENT_RISK_REJECTED`(Provider 缺省不拦,接入方显式接)。
6. **断单可对账恢复**:发货失败订单停在 `PAID`,不丢单;恢复走管理面对账重发(发货幂等)。

## 数据模型

### sku(价格表条目,管理面登记)

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | id | 接入方定义的 SKU 标识 |
| `productId` | string | 发货目标(道具/货币包 ID;语义归接入方) |
| `form` | 枚举 | 四形态之一 |
| `amountCents` | int64 | 服务端定价(分) |
| `currency` | string | ISO 4217(如 `CNY`) |

### order(订单)

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | id | 服务端生成,UUIDv7,前缀 `order_` |
| `status` | 枚举 | `CREATED` / `PAID` / `DELIVERED` / `CLOSED`(大写) |
| `skuId` / `productId` / `form` | — | 下单时从 SKU 落定(快照,SKU 后改不影响已建订单) |
| `amountCents` / `currency` | — | 同上 |
| `payToken` | string? | **仅下单响应**下发:渠道侧支付凭证(沙箱渠道为模拟凭证;真实渠道语义归渠道适配) |
| `createdAt` / `updatedAt` | timestamp | RFC 3339 毫秒 UTC |
| `paidAt` / `deliveredAt` | timestamp? | 可选,缺省不下发 |

状态机:`CREATED → PAID → DELIVERED`(正向);`CREATED/PAID → CLOSED`(关单);回调对 `DELIVERED/CLOSED` 订单幂等受理不回退。**断单态 = 停在 `PAID`**(已付未发,对账恢复)。

## 端点总览(挂载 `/v1/payments/*`)

| 端点 | 认证 | 说明 |
| --- | --- | --- |
| `GET /v1/payments/skus` | Bearer | 可购 SKU 列表(价格表投影) |
| `POST /v1/payments/orders` | Bearer | 下单(服务端定价;风控前置) |
| `GET /v1/payments/orders` | Bearer | 我的订单列表(`updatedAt` 倒序,分页) |
| `GET /v1/payments/orders/{orderId}` | Bearer | 订单详情(状态轮询;发货感知靠它) |
| `POST /v1/payments/callback` | **渠道签名** | S2S:渠道支付回调(HMAC-SHA256;非玩家端点,无 Bearer) |

- 认证、scope header、信封与分页同 [auth.md](./auth.md) / [primitives.md](./primitives.md)。

### `POST /v1/payments/orders`

请求体(裸对象):`{ "skuId": "..." }`。

成功响应 `data` = order 快照(`CREATED` + `payToken`)。SKU 不存在/不可购 → 404 `PAYMENT_SKU_NOT_FOUND`;风控拒绝 → 403 `PAYMENT_RISK_REJECTED`。

### `POST /v1/payments/callback`(S2S)

请求头:`X-Payment-Signature: <hex HMAC-SHA256(channelSecret, rawBody)>`;请求体(裸对象):

```json
{ "orderId": "order_...", "paidAt": "2026-10-09T12:00:00.000Z" }
```

- 签名验证**先于** body 解析(raw body 参与 HMAC);失败 → 403 `PAYMENT_INVALID_SIGNATURE`,不区分「密钥错/签名错/伪造」。
- 受理 = 订单转 `PAID` → 同步执行发货钩子 → 成功转 `DELIVERED`;发货失败订单停 `PAID`(受理仍 200——回调本身成功,发货是对账问题)。
- 对 `PAID`(已回调未发货)与 `DELIVERED` 订单重复回调 = 幂等 200,不二次发货。

## 发货与对账(管理面,不经网关玩家路由)

- **发货钩子**:Provider 构造注入(接入方按订单发货:道具/入账/外部确认);幂等要求按订单。
- **对账重发**:管理面枚举停 `PAID` 的订单逐个重试发货(断单可对账恢复的落点);本 v1 为进程内方法,持久化与定时对账随部署面。

## 推送事件(v1.1 新增)

经推送通道([messages.md](./messages.md),默认 Provider chirp)**广播**;载荷是最小提示,**不含金额/SKU 等业务字段**(广播信道人人可收,业务数据归拉取端点按归属过滤):

| type | 触发 | payload |
| --- | --- | --- |
| `payment.paid` | 订单 `CREATED → PAID`(渠道回调受理) | `{ "orderId": "order_..." }` |
| `payment.delivered` | 订单转 `DELIVERED`(回调同步发货;含对账恢复的重发) | `{ "orderId": "order_..." }` |

- 语义同 [events.md](./events.md):at-most-once + 拉取兜底——推送只是「该去拉了」的提示,客户端收到后 `GET /v1/payments/orders/{orderId}`(归属校验:非本人 404,广播不泄露存在性以外的信息)。幂等重复回调不重复推送(只报状态转移,不报受理)。
- 发货感知升级:**推送为提示、轮询为准**——未连流/丢帧时轮询(`GET orders/{id}`)仍是完整可用路径,不做实时期望。

## 错误码域表(前缀 `PAYMENT`,errors.md v4)

| code | HTTP | retryable | 说明 |
| --- | --- | --- | --- |
| `PAYMENT_SKU_NOT_FOUND` | 404 | false | SKU 不存在或不可购 |
| `PAYMENT_ORDER_NOT_FOUND` | 404 | false | 订单不存在或不属于当前玩家 |
| `PAYMENT_RISK_REJECTED` | 403 | false | 风控前置拒绝(大额/异常频次) |
| `PAYMENT_INVALID_SIGNATURE` | 403 | false | 渠道回调签名校验失败(伪造一律拒绝) |
| `PAYMENT_ORDER_STATE` | 409 | false | 订单状态机非法转移(管理面误操作防护) |

## 客户端要求(各端 SDK)

- 能力未接(501 `COMMON_CAPABILITY_DISABLED`)→ 各查询返回 null,隐藏商城/充值 UI。
- **发货感知 = 推送为提示、轮询为准**(v1.1 起有 `payment.paid`/`payment.delivered` 推送;未连流/丢帧轮询详情仍完整可用,不做实时期望)。
- `payToken` 只在下单响应出现一次:沙箱渠道直接回传模拟渠道完成支付;真实渠道按渠道 SDK 语义消费(契约不解释渠道 SDK)。
- 金额展示用 `amountCents`/`currency` 原样渲染,**不得**在客户端做任何金额计算。
- `PAYMENT_RISK_REJECTED` / `PAYMENT_INVALID_SIGNATURE` 不重试;429 按 `Retry-After` 退避。
