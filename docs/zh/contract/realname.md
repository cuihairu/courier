
# 实名认证契约(RealName)

> 状态:**Frozen v1.1** · 合规能力 · 默认关闭 · 里程碑 M2 后段 · v1 冻结 2026-10-08 / v1.1 冻结 2026-10-18(PATROL1018E 代决:`consent` 组,只加不改)。
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

请求 `{ "accountId": "acc_...", "date": "YYYY-MM-DD", "playMinutes": int, "chargeAmountCents": int }`(裸对象信封外);响应 `data: {}`。

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
- v1.1 已冻结:海外年龄门/家长同意 `consent` 组,见下文「同意管理(consent,v1.1)」节(调研 F31;2026-10-18 PATROL1018E 代决冻结,只加不改;设计过程见 [research/consent-v1.1-draft.md](../research/consent-v1.1-draft.md))。

## 同意管理(consent,v1.1)

> v1.1 新增,只加不改:现有 `verify` / `status` / `curfew` / `charge-check` / `playtime-report` 端点与字段一律不动。2026-10-18 巡检 PATROL1018E 代决冻结(设计草案见 [research/consent-v1.1-draft.md](../research/consent-v1.1-draft.md))。**冻结 ≠ 实现**:网关与六端 SDK 实现面待开工拍板(F31,登记见 [todo.md](../todo.md))。

- **默认关闭**:未配置 consent 通道 = 能力关闭语义(`501 COMMON_CAPABILITY_DISABLED`),查询返回「未启用」,接入方隐藏年龄门 UI,**不进报错路径**(同 v1)。
- **判定经 200 载荷**:同意态进 `data` 不进错误码,**零新增错误码**。
- **客户端不得自行推断**:年龄与同意判定均服务端下发(同 v1 `isMinor` 原则)。
- **数据红线对齐 v1**:出生日期只经申报接口传输一次,不缓存、不落盘、不进诊断采集;UI 展示仅年份。

### 状态机(consent 状态)

| state | 语义 | 游戏侧处理 |
| --- | --- | --- |
| `UNDECLARED` | 未申报年龄 | 首启年龄门(申报前不放行受保护操作) |
| `ADULT` | 成年 | 终态,不再要求同意 |
| `PENDING_PARENT` | 未成年且同意未决 | 门槛关闭;可发起 / 重发 VPC 请求,按 `requestExpiresAt` 提示 |
| `GRANTED` | 家长同意成立 | 门槛放行 |
| `DENIED` | 家长拒绝 | 门槛关闭;是否允许再次请求归 Provider 策略 |
| `REVOKED` | 同意后被撤回 | 门槛关闭;撤回即停发诊断(见「诊断联动」) |

- **防回拨**:重复申报年龄仅「趋严」生效(成年 → 未成年可,反向不行);纠错走 Provider 运维面,不进玩家契约。

### 端点(挂载 `/v1/realname/consent/*`)

| 端点 | 认证 | 说明 |
| --- | --- | --- |
| `GET /v1/realname/consent/status` | Bearer | 同意状态查询 |
| `POST /v1/realname/consent/declare-age` | Bearer | 首启年龄申报 `{ birthDate: "YYYY-MM-DD" }`,响应 `data` = 下表 status(申报即得判定) |
| `POST /v1/realname/consent/parent-consent/request` | Bearer | 发起 VPC 请求 `{}` → `{ requestId, requestExpiresAt }`;`PENDING_PARENT` 期间幂等(未过期返回同一请求,过期换新 `requestId`) |
| 推送事件 `realname.consent.updated` | SSE | 状态变化通知(复用 M3 推送通道,同 `config.updated` 形状) |

- 玩家侧端点认证与 scope header 要求同 [auth.md](./auth.md);账号维度(同 v1,非 player 维度)。
- 家长验证本身**不在本契约**(归 Provider:信用卡验证 / 签署表单 / 政府证件 / 邮件增强等法域机制);本契约只表达「请求发起 + 结果状态 + 变化推送」。

### 数据模型:status(同意状态)

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `state` | 枚举 | 上表六态 |
| `declaredAt` | timestamp? | 年龄申报时间 |
| `grantedAt` | timestamp? | 同意成立时间(`GRANTED` 下发) |
| `revokedAt` | timestamp? | 撤回时间(`REVOKED` 下发) |
| `requestExpiresAt` | timestamp? | 当前 VPC 请求到期点(`PENDING_PARENT` 下发) |

请求 / 响应走 primitives 信封([primitives.md](./primitives.md))。

### 法域中立:阈值与规则归 Provider 配置

| 配置 | 说明 | 契约关系 |
| --- | --- | --- |
| `minimumAge` | 最低游玩年龄(低于则始终 `DENIED` 语义,由 Provider 决定表达) | 不进契约 |
| `digitalConsentAge` | 数字同意年龄(GDPR-K 各成员国 13–16;COPPA 13) | 不进契约 |
| `vpc.mechanism / ttl` | 家长验证机制与请求有效期 | 不进契约 |

- 判定输入 = `birthDate` + Provider 阈值配置;换法域 = 换配置,契约与六端 SDK 不动。

### Provider 接口扩展(加法,不动 v1 方法)

```text
RealNameProvider 追加:
  ConsentStatus(accountId) → ConsentState
  DeclareAge(accountId, birthDate) → ConsentState        // 防回拨:仅趋严生效
  RequestParentConsent(accountId) → ConsentRequest{ requestId, expiresAt }
```

- 热插拔 / 降级链 / 熔断沿用 v1 通用 Provider 治理规则;降级安全态 = `PENDING_PARENT`(不放行不拒绝,转人工)。
- 审计:新增事件 `realname.consent.declared / parent_requested / granted / denied / revoked`,只含账号维度元数据;出生日期与家长身份输入**永不进事件**(同 v1 红线);经共享接缝 `gateway/audit.AuditSink`,两实现同形状。
- 同意记录与身份数据**分存储、独立保留期**(合规要求可分别举证);保留天数归接入方合规口径(F33 同款边界)。

### SDK 面(L2/L4 投影)

```text
RealName.Consent
  .Status()                  → { state, declaredAt?, grantedAt?, revokedAt?, requestExpiresAt? }
  .DeclareAge(birthDate)     → status
  .RequestParentConsent()    → { requestId, requestExpiresAt }
OnConsentChanged 回调预留(消费 realname.consent.updated 推送)
```

- `REVOKED` 到达 → SDK 内部触发诊断停发联动([diagnostics.md](./diagnostics.md) 预留位),无需接入方逐事件接线;「关闭立即生效」红线已内建。

### 错误码

**零新增**——全部判定经 200 载荷表达(2026-10-15 代决口径)。实现面评审时若接入方游戏侧策略执行确需错误码,按「注册保留码」流程补([errors.md](./errors.md) + `TestErrorCodesWiredOrReserved` 门禁),不预注册。

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
- 熔断:连续 N 次(默认 5)失败开路,开路后 **30 次调用**半开放行探测(实现按调用次数驱动,无定时器);熔断期间直接走 fallback。
- 业务性「不匹配」(核验结果 `REJECTED`)是**有效结果**,不计入熔断失败;熔断只统计调用失败(网络/超时/5xx)。
- HealthCheck 供路由表剔除不健康实例。
- 审计留痕:关键动作(verify / curfew / charge_check / playtime_report)已落结构化审计事件面,接收面为接入方注入点(缺省 = 不产出;内置日志半边与 HTTP 直写半边,见 provider-guide §4);事件只含账号维度元数据,身份输入不入事件;保留策略与查询面归接入方存储实现(契约面不变)。接缝在共享包 `gateway/audit`,两个实现(warden / warden-alt)同形状注入——切换 Provider 不改变留痕面。

以上热插拔/降级/熔断规则为通用 Provider 治理规则(见 [architecture.md](../architecture.md)「Provider 治理」),RealName 是第一个完整适用方。

## 错误码(域表)

| code | HTTP | retryable | 说明 |
| --- | --- | --- | --- |
| `REALNAME_REQUIRED` | 403 | false | 需要实名(未提交) |
| `REALNAME_REJECTED` | 403 | false | 实名未通过 |
| `REALNAME_PENDING_REVIEW` | 403 | false | 待复核(降级链全挂时的安全态) |
| `REALNAME_CURFEW_BLOCKED` | 403 | false | 不可玩时段(F28;`data` 可带 `nextWindowAt`) |
| `REALNAME_CHARGE_BLOCKED` | 403 | false | 超出充值额度(F29;`data` 可带限额字段) |

> **本表当前为注册保留码,网关不发**(2026-10-15 巡检代决,判定载荷为唯一真相源):
> 实名判定经 200 判定态载荷表达——`status.state`(`UNVERIFIED`/`PENDING_REVIEW`/
> `VERIFIED`/`REJECTED`)+ `isMinor` + `curfew.playable` + `charge-check.allowed`。
> `verify` / `curfew` / `charge-check` 是判定端点,返回 403 会把「查一下能不能充」
> 变成异常路径;载荷可同时表达五种状态,单个 403 码不能。上表码保留不删,接入方在
> 游戏侧执行防沉迷策略时仍可使用(见下文「策略执行不在本契约」);全表同 [errors.md](./errors.md)
> 「注册保留码」注记,由 `TestErrorCodesWiredOrReserved` 门禁核对。

其余(认证、scope、限流、能力关闭)一律通用码,见 [errors.md](./errors.md)。

## 客户端要求(各端 SDK)

- `501 COMMON_CAPABILITY_DISABLED` = 实名未开启:查询接口返回「未启用」,UI 隐藏入口,**不进报错路径**。
- `PENDING_REVIEW` 是安全态:不引导重试、不放行受保护操作,提示「审核中」;是否转人工由接入方决定。
- `REALNAME_REQUIRED` → 引导进入实名流程;`REALNAME_CURFEW_BLOCKED` / `REALNAME_CHARGE_BLOCKED` → 按 `data` 字段展示,不得自造文案口径。
  (网关当前不发这三个码——同一判定在 200 载荷里:`state=UNVERIFIED` → 引导实名;
  `curfew.playable=false` / `charge-check.allowed=false` → 按载荷 `nextWindowAt`、
  限额字段展示。接入方在游戏侧执行策略并改用错误码时,客户端映射已就位,照此处理。)
- 姓名与证件号**只经 `Submit` 传输一次**:不缓存、不重放、不打印;UI 展示一律走 `RealNameMask`。

## 扩展预留(2026-10-11 补注)

| 可能的扩展 | 兼容处理方式 |
| --- | --- |
| 海外年龄门/家长同意(F31) | `consent` 端点组已于 v1.1 冻结(2026-10-18 PATROL1018E 代决,见「同意管理(consent,v1.1)」节);现有字段与端点不动,实现面待开工拍板 |
| Provider 结果扩展 | `policyExtension` 扩展位已预留,未知字段必须忽略;新策略字段 = 加法兼容 |
| 验证通过回调 | `OnRealNameVerified` 客户端回调已预留;触发时机细化 = 加参数提案 |
| 第三方核验生态 | `RealNameProvider` 接口可插拔 + 路由表热切换已冻结;接新供应商 = 新适配,契约不动 |
| 时段/额度规则调整 | 管理面配置(curfew 窗口、分龄限额);契约不冻结具体值,红线(默认关、显式开)不因扩展松动 |
