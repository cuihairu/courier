# 实名认证契约(RealName)

> 状态:**Frozen v1** · 合规能力 · 默认关闭 · 里程碑 M2 后段 · 冻结日期 2026-10-08。
> 域前缀 `REALNAME` 已注册([errors.md](./errors.md));冻结后只加不改,破坏性变更升版本(见 [versioning.md](./versioning.md))。
> 红线:实名数据是最高敏感级个人数据。本能力默认关闭;接入即视为接入方向玩家收集实名信息,**数据范围明示义务在接入方**,本文档的数据范围表即对外口径的事实源。

## 能力与范围

- 能力域 `realname`(`providers.CapRealname`),路由 `/v1/realname/*`;**默认关闭**——未配置 Provider 时与其他能力一致的降级信号(`501 COMMON_CAPABILITY_DISABLED`),接入方据此隐藏实名 UI,不进报错路径。
- 开启须同时给出 Provider 配置与明示文案;二者缺一按关闭处理。
- 本契约**不覆盖**:核验执行本身(归 Provider:自建库/阿里云/腾讯云慧眼/易盾/Webhook)、防沉迷策略执行(时段/额度的**策略与配置**归接入方,本契约只提供**查询与校验端点**)。
- 调研依据:必备链路 F27–F30(见 [features.md §14](../research/features.md))——实名核验(F27)、时段查询(F28)、额度校验(F29)、S2S 上报(F30)。

## 客户端接口(L2/L4 投影)

```text
RealName
  .Submit({ name, idNumber })   提交核验 → 状态见下
  .Status()                     → { state, isMinor?, verifiedAt? }
  .Curfew()                     → { playable, nextWindowAt? }
  .ChargeCheck(amountCents)     → { allowed, singleLimitCents?, monthlyLimitCents?, monthlyUsedCents? }
```

- `state`:`UNVERIFIED | PENDING_REVIEW | VERIFIED | REJECTED`
- `isMinor`:仅 `VERIFIED` 后由服务端下发判定结果;客户端不得自行推断。
- 提交接口要求会话已认证(挂在 account 维度,非 player 维度)。
- 脱敏口径落 UI:姓名保留首字余 `*`(张三 → 张\*);证件号前 3 后 4,中间 `*`(110101199001011234 → 110\*\*\*\*\*\*\*\*\*\*\*\*1234)。SDK 提供 `RealNameMask` 纯函数(service 包实现,dotnet 可测;UI 面板与接入方共用),接入方不得自造口径。

## 端点总览(挂载 `/v1/realname/*`)

| 端点 | 认证 | 说明 | 调研落点 |
| --- | --- | --- | --- |
| `POST /v1/realname/verify` | Bearer | 提交实名核验 | F27 |
| `GET /v1/realname/status` | Bearer | 查询实名状态 | F27 |
| `GET /v1/realname/curfew` | Bearer | 可玩时段查询(未成年限玩窗口) | F28 |
| `POST /v1/realname/charge-check` | Bearer | 充值额度校验 | F29 |
| `POST /v1/realname/playtime-report` | **S2S** | 防沉迷数据上报(服务端对服务端) | F30 |

- 玩家侧端点认证与 scope header 要求同 [auth.md](./auth.md);缺失 → 对应通用码。
- `playtime-report` 为 **S2S 端点**:认证走接入方服务端凭证(非玩家 Bearer);SDK 不得调用——玩家端上报会造成重复统计,统一由接入方服务端汇总上报。
- 成功响应 HTTP 200,信封见 [primitives.md](./primitives.md)。

## 数据模型

### status(实名状态)

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `state` | 枚举 | `UNVERIFIED` / `PENDING_REVIEW` / `VERIFIED` / `REJECTED` |
| `isMinor` | bool? | 仅 `VERIFIED` 后下发;其余 state 缺省 |
| `verifiedAt` | timestamp? | 核验通过时间 |

### curfew(可玩时段,F28)

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `playable` | bool | 当前是否可玩 |
| `nextWindowAt` | timestamp? | 下一可玩窗口起点(不可玩时下发;可玩时缺省) |

- 判定输入 = `status.isMinor` + Provider 时段配置;成年人恒 `playable = true`。
- 策略配置(哪些日子、哪些窗口)归 Provider,不在契约——契约只保证查询语义。

### charge-check(F29)

请求 `{ "amountCents": int }`(本次充值金额,分)。

响应 `data`:

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `allowed` | bool | 本次充值是否放行 |
| `singleLimitCents` | int? | 单次上限 |
| `monthlyLimitCents` | int? | 当月上限 |
| `monthlyUsedCents` | int? | 当月已用 |

- `allowed = false` → 接入方引导;支付域(M5)在下单前调用本端点做前置校验。

### playtime-report(F30,S2S)

请求 `{ "date": "YYYY-MM-DD", "playMinutes": int, "chargeAmountCents": int }`;响应 `data: {}`。

- 优先服务端 S2S 上报,避免客户端重复上报;幂等键 = `accountId + date`(同日重报取最新)。

## 字段脱敏与数据红线

| 环节 | 口径 |
| --- | --- |
| 传输 | 仅 HTTPS(TLS 1.2+);SDK 不做二次加密,信封加密归 Provider 实现 |
| SDK 侧 | **不落盘**:不进安全存储、不进日志、不进 breadcrumb / trace / 诊断采集 |
| UI 展示 | 一律脱敏(姓\*、证件号前 3 后 4;口径见「客户端接口」节,由 UI 包函数统一) |
| 服务端 | 加密存储;日志与 trace 禁明文;保留期按接入方合规要求,取最短必要 |
| 诊断 | 实名接口的请求/响应体**永不进入**任何诊断采集(见 [diagnostics.md](./diagnostics.md) 红线) |

采集字段清单(全部):`name`、`idNumber`、核验结果、成年/未成年判定、时段/额度判定输出。其余一律不采。

## 防沉迷钩子(接口位)

- `Status().isMinor` 与 `Curfew().playable` 是唯二判定输出;策略执行不在本契约(接入方游戏侧,或未来独立防沉迷模块)。
- 预留:`OnRealNameVerified` 客户端回调;服务端 Provider 结果中保留 `policyExtension` 扩展位,当前必须忽略未知字段。
- v1.1 预留(只加不改):海外年龄门/家长同意 `consent` 组(调研 F31,出海需求触发再冻结)。

## Provider 热插拔(服务端,治理首个完整适用方)

统一接口 `RealNameProvider`:

```text
Verify(accountId, identity) → Result      Query(accountId) → Status
Curfew(accountId, now) → Curfew           ChargeCheck(accountId, amountCents) → ChargeDecision
HealthCheck() → bool
```

**内置适配(按需实现):**

| Provider | 说明 |
| --- | --- |
| 自建核验 | 本地库比对,数据不出自建环境(**首个实现**) |
| 阿里云实人认证 | 云 API 适配(按需) |
| 腾讯云慧眼 | 云 API 适配(按需) |
| 网易易盾 | 云 API 适配(按需) |
| 自定义 Webhook | 接入方自有核验服务的通用 HTTP 适配 |

**热切换:**

- 配置驱动路由表:`primary` + `fallbacks[]`;运行时配置重载即生效,**不重启**。
- 路由表原子替换;在途请求按旧表完成,新请求走新表。

**降级链与熔断:**

- 主 Provider 失败/超时 → 依序尝试 fallback;全部失败 → 返回 `PENDING_REVIEW`(安全态:不通过也不拒绝放量,转人工/延迟复核)。
- 熔断:连续 N 次(默认 5)失败开路 T 秒(默认 30s),半开放行探测;熔断期间直接走 fallback。
- 业务性「不匹配」(核验结果 `REJECTED`)是**有效结果**,不计入熔断失败;熔断只统计调用失败(网络/超时/5xx)。
- HealthCheck 供路由表剔除不健康实例。

以上热插拔/降级/熔断规则为通用 Provider 治理规则(见 [architecture.md](../architecture.md)「Provider 治理」),RealName 是第一个完整适用方。

## 错误码(域表)

| code | HTTP | retryable | 说明 |
| --- | --- | --- | --- |
| `REALNAME_REQUIRED` | 403 | false | 需要实名(未提交) |
| `REALNAME_REJECTED` | 403 | false | 实名未通过 |
| `REALNAME_PENDING_REVIEW` | 403 | false | 待复核(降级链全挂时的安全态) |
| `REALNAME_CURFEW_BLOCKED` | 403 | false | 不可玩时段(F28;`data` 可带 `nextWindowAt`) |
| `REALNAME_CHARGE_BLOCKED` | 403 | false | 超出充值额度(F29;`data` 可带限额字段) |

其余(认证、scope、限流、能力关闭)一律通用码,见 [errors.md](./errors.md)。

## 客户端要求(各端 SDK)

- `501 COMMON_CAPABILITY_DISABLED` = 实名未开启:查询接口返回「未启用」,UI 隐藏入口,**不进报错路径**。
- `PENDING_REVIEW` 是安全态:不引导重试、不放行受保护操作,提示「审核中」;是否转人工由接入方决定。
- `REALNAME_REQUIRED` → 引导进入实名流程;`REALNAME_CURFEW_BLOCKED` / `REALNAME_CHARGE_BLOCKED` → 按 `data` 字段展示,不得自造文案口径。
- 姓名与证件号**只经 `Submit` 传输一次**:不缓存、不重放、不打印;UI 展示一律走 `RealNameMask`。
