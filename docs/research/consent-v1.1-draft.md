
# F31 海外年龄门与家长同意:`consent` 组 v1.1 设计草案

> 状态:**已冻结(v1.1)** · 2026-10-16 FW1016D 代决启动设计 → 2026-10-18 巡检 PATROL1018E 代决冻结,已落 [contract/realname.md](../contract/realname.md)「同意管理(consent,v1.1)」节(只加不改)。实现面(冻结清单 2–4 步)待开工拍板。
> 定性:本草案把「出海拍板后从零设计」变成「按草案评审冻结」;法域中立(阈值不进契约,归 Provider 配置),不绑死 COPPA / GDPR-K 单一形状。
> 调研依据:F31 行([features.md §5](./features.md):首启年龄筛查;低于数字同意年龄走可验证家长同意 VPC;同意记录单独留存)。

## 代决记录(2026-10-16,巡检 FW1016D)

- **决定**:`consent` 组按法域中立设计,草案先行;冻结与实现待出海立项。
- **理由**:F31 属「待拍板」卡点(出海需求触发再冻结)。契约先行仓的既定打法是设计风险前置——草案不改变 realname v1 冻结态、不产生实现维护面;出海立项后「草案 → 冻结评审 → 落地」是机械步骤。
- **推翻路径**:拍板人推翻即废本草案,恢复「触发再从零设计」原口径;本档从 docs 移除即可,无任何实现面牵连。

## 代决记录(2026-10-18,巡检 PATROL1018E)

- **决定**:契约面立项——按本草案把 `consent` 组冻结为 realname **v1.1**(realname.md 新增「同意管理」节,只加不改,零新增错误码)。
- **理由**:1018E 单授权「待拍板」项代决;冻结动作可逆(纯加法、git 可回滚、不触碰 v1 字段/端点/码)、在本仓范围内;FW1016D 已确立「设计风险前置」打法,本步是其机械延伸。**商业出海与否仍归用户**,本代决只解契约面。
- **明确不代决**:冻结清单 2–4 步(fixtures 再生 / warden+wardenalt 实现 / 六端 SDK+e2e)——无真实接入方开工会产生无人验收的实现维护面,仍标卡点,解锁钥匙=接入方确认接入或用户开工指令。
- **推翻路径**:拍板人一句话即废——realname.md「同意管理」节回退为 v1.1 预留态,本档改回 Draft,零实现牵连。

## 设计原则(继承 realname v1)

- **只加不改**:v1 现有端点 / 字段 / 码不动;`consent` 组全部为新增。
- **判定经 200 载荷**:沿用 2026-10-15 代决口径(realname.md 错误码节注记)——判定态进 `data`,不进 403 码;`DENIED` / `REVOKED` / `PENDING` 是 state,不是错误码。
- **客户端不得自行推断**:年龄判定、同意判定均服务端下发(v1 `isMinor` 同款原则)。
- **默认关闭**:未配置 consent 通道按能力关闭语义(501 `COMMON_CAPABILITY_DISABLED`),接入方隐藏年龄门 UI,不进报错路径。
- **数据红线对齐 v1**:出生日期是个人数据——只经申报接口传输一次、不缓存、不落盘、不进诊断采集(同 v1「字段脱敏与数据红线」);UI 展示仅年份。

## 状态机(consent 状态)

```text
UNDECLARED ──declare-age──► ADULT(成年,终态)
     │
     └─declare-age─► PENDING_PARENT(未成年,待家长同意)
                          │  parent-consent/request 发起,expiresAt 到点可重发
                          ├─provider 判定─► GRANTED(同意,可玩)
                          │        └─parent 撤回─► REVOKED(撤回,回门槛关闭)
                          └─provider 判定─► DENIED(拒绝)
```

| state | 语义 | 游戏侧处理 |
| --- | --- | --- |
| `UNDECLARED` | 未申报年龄 | 首启年龄门(申报前不放行受保护操作) |
| `ADULT` | 成年 | 终态,不再要求同意 |
| `PENDING_PARENT` | 未成年且同意未决 | 门槛关闭;可发起 / 重发 VPC 请求,按 `requestExpiresAt` 提示 |
| `GRANTED` | 家长同意成立 | 门槛放行 |
| `DENIED` | 家长拒绝 | 门槛关闭;是否允许再次请求归 Provider 策略 |
| `REVOKED` | 同意后被撤回 | 门槛关闭;撤回联动诊断停发(见「诊断联动」) |

- **防回拨**:重复申报年龄仅「趋严」生效(成年 → 未成年可,反向不行);纠错走 Provider 运维面,不进玩家契约。

## 端点组(v1.1 新增,挂载 `/v1/realname/consent/*`)

| 端点 | 认证 | 说明 |
| --- | --- | --- |
| `GET /v1/realname/consent/status` | Bearer | 同意状态查询 |
| `POST /v1/realname/consent/declare-age` | Bearer | 首启年龄申报 `{ birthDate: "YYYY-MM-DD" }` |
| `POST /v1/realname/consent/parent-consent/request` | Bearer | 发起 VPC 请求 `{}` → `{ requestId, requestExpiresAt }` |
| 推送事件 `realname.consent.updated` | SSE | 状态变化通知(复用 M3 推送通道,同 `config.updated` 形状) |

- 玩家侧端点认证与 scope 要求同 [auth.md](../contract/auth.md);账号维度(同 v1,非 player 维度)。
- 家长验证本身**不在本契约**(归 Provider:信用卡验证 / 签署表单 / 政府证件 / 邮件增强等法域机制);契约只表达「请求发起 + 结果状态 + 变化推送」。

## 数据模型

### status(同意状态)

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `state` | 枚举 | 上表六态 |
| `declaredAt` | timestamp? | 年龄申报时间 |
| `grantedAt` | timestamp? | 同意成立时间(`GRANTED` 下发) |
| `revokedAt` | timestamp? | 撤回时间(`REVOKED` 下发) |
| `requestExpiresAt` | timestamp? | 当前 VPC 请求到期点(`PENDING_PARENT` 下发) |

### declare-age / parent-consent-request

- 请求 / 响应走 primitives 信封;`declare-age` 响应 `data` = 上表 status(申报即得判定)。
- `parent-consent/request` 幂等:`PENDING_PARENT` 期间重复发起返回同一请求(未过期时);过期后重发换新 `requestId`。

## 法域中立:阈值与规则归 Provider 配置

| 配置 | 说明 | 契约关系 |
| --- | --- | --- |
| `minimumAge` | 最低游玩年龄(低于则始终 `DENIED` 语义,由 Provider 决定表达) | 不进契约 |
| `digitalConsentAge` | 数字同意年龄(GDPR-K 各成员国 13–16;COPPA 13) | 不进契约 |
| `vpc.mechanism / ttl` | 家长验证机制与请求有效期 | 不进契约 |

- 服务端判定输入 = `birthDate` + Provider 阈值配置;换法域 = 换配置,契约与六端 SDK 不动。

## Provider 接口扩展(加法,不动 v1 方法)

```text
RealNameProvider 追加:
  ConsentStatus(accountId) → ConsentState
  DeclareAge(accountId, birthDate) → ConsentState            // 防回拨:仅趋严生效
  RequestParentConsent(accountId) → ConsentRequest{ requestId, expiresAt }
```

- 热插拔 / 降级链 / 熔断沿用 v1 通用 Provider 治理规则;降级安全态 = `PENDING_PARENT`(不放行不拒绝,转人工)。

## 审计与留存(同意记录单独留存)

- 新增审计事件:`realname.consent.declared / parent_requested / granted / denied / revoked`——只含账号维度元数据,出生日期与家长身份输入**永不进事件**(同 v1 红线);经共享接缝 `gateway/audit.AuditSink`,两实现同形状。
- **同意记录与身份数据分存储、独立保留期**(合规要求可分别举证);保留天数归接入方合规口径(F33 同款边界)。

## SDK 面(L2/L4 投影)

```text
RealName.Consent
  .Status()                  → { state, declaredAt?, grantedAt?, revokedAt?, requestExpiresAt? }
  .DeclareAge(birthDate)     → status
  .RequestParentConsent()    → { requestId, requestExpiresAt }
OnConsentChanged 回调预留(消费 realname.consent.updated 推送)
```

- 501 = consent 未启用:查询返回「未启用」,UI 隐藏年龄门,不进报错路径(v1 同款)。
- `REVOKED` 到达 → SDK 内部触发诊断停发联动(下节),无需接入方逐事件接线。

## 诊断联动(隐私撤回)

- [diagnostics.md:117](../contract/diagnostics.md) 预留位:「同意状态联动(隐私撤回)——宿主传入 consent 状态 → 全类停发」。本草案落地该联动的服务端触发源:`REVOKED` 状态下发时各端 SDK 停发诊断(红线「关闭立即生效」已内建)。

## 错误码

- **零新增**:全部判定经 200 载荷表达(2026-10-15 代决口径)。若冻结评审时接入方游戏侧策略执行需要码,按「注册保留码」流程补(errors.md + `TestErrorCodesWiredOrReserved` 门禁),不预注册。

## 冻结清单与落地状态

1. ✅ 草案评审定稿 → realname.md 升 v1.1(2026-10-18 PATROL1018E 代决,新增「同意管理(consent,v1.1)」节,现有节不动)。
2. ⏳ fixtures 增补(如需)→ `go run ./tools/contractgen -write` 六端再生成。
3. ⏳ 网关:warden / wardenalt 两实现同形状(Provider 方法 + 审计事件 + 降级安全态)。
4. ⏳ 六端 SDK(unity / cocos / miniprogram / laybox / ue / godot)+ e2e;errors.md 仅在需要码时升版。

> 2–4 步=实现面,统一卡在「开工拍板」:等接入方确认真接入 consent 流程,或用户直接指示开工。
