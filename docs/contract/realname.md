# 实名认证契约(RealName)

> 状态:Draft v0 · 合规能力 · 默认关闭 · 里程碑 M2 后段。
> 红线:实名数据是最高敏感级个人数据。本能力默认关闭;接入即视为接入方向玩家收集实名信息,**数据范围明示义务在接入方**,本文档的数据范围表即对外口径的事实源。

## 能力开关

- SDK 初始化配置 `realname.enabled = false`(默认)。开启须同时给出 Provider 配置;二者缺一按关闭处理。
- 未开启时:`RealName` 接口查询返回「未启用」,服务端对受实名保护的操作返回 `REALNAME_REQUIRED` 由接入方决定如何引导。

## 客户端接口

```text
RealName
  .Submit({ name, idNumber })   提交核验 → 状态见下
  .Status()                     → { state, isMinor?, verifiedAt? }
```

- `state`:`UNVERIFIED | PENDING_REVIEW | VERIFIED | REJECTED`
- `isMinor`:仅 `VERIFIED` 后由服务端下发判定结果;客户端不得自行推断。
- 提交接口要求会话已认证(挂在 account 维度,非 player 维度)。

## 字段脱敏与数据红线

| 环节 | 口径 |
| --- | --- |
| 传输 | 仅 HTTPS(TLS 1.2+);SDK 不做二次加密,信封加密归 Provider 实现 |
| SDK 侧 | **不落盘**:不进安全存储、不进日志、不进 breadcrumb / trace / 诊断采集 |
| UI 展示 | 一律脱敏(姓*、证件号前 3 后 4) |
| 服务端 | 加密存储;日志与 trace 禁明文;保留期按接入方合规要求,取最短必要 |
| 诊断 | 实名接口的请求/响应体**永不进入**任何诊断采集(见 [diagnostics.md](./diagnostics.md) 红线) |

采集字段清单(全部):`name`、`idNumber`、核验结果、成年/未成年判定。其余一律不采。

## 防沉迷钩子(接口位,本契约不实现)

- `Status().isMinor` 是唯一的判定输出;「未成年 → 时段/时长限制」的策略与执行不在本契约范围(接入方游戏侧实现,或未来独立防沉迷模块)。
- 预留:`OnRealNameVerified` 客户端回调;服务端 Provider 结果中保留 `policyExtension` 扩展位,供后续防沉迷契约使用,当前必须忽略未知字段。

## Provider 热插拔(服务端)

统一接口 `RealNameProvider`:

```text
Verify(accountId, identity) → Result      Query(accountId) → Status      HealthCheck() → bool
```

**内置适配(按需实现):**

| Provider | 说明 |
| --- | --- |
| 自建核验 | 本地库比对,数据不出自建环境 |
| 阿里云实人认证 | 云 API 适配 |
| 腾讯云慧眼 | 云 API 适配 |
| 网易易盾 | 云 API 适配 |
| 自定义 Webhook | 接入方自有核验服务的通用 HTTP 适配 |

**热切换:**

- 配置驱动路由表:`primary` + `fallbacks[]`;运行时配置重载即生效,**不重启**。
- 路由表原子替换;在途请求按旧表完成,新请求走新表。

**降级链与熔断:**

- 主 Provider 失败/超时 → 依序尝试 fallback;全部失败 → 返回 `PENDING_REVIEW`(安全态:不通过也不拒绝放量,转人工/延迟复核)。
- 熔断:连续 N 次(默认 5)失败开路 T 秒(默认 30s),半开放行探测;熔断期间直接走 fallback。
- HealthCheck 供路由表剔除不健康实例。

以上热插拔/降级/熔断规则为通用 Provider 治理规则,RealName 是第一个完整适用方(见 architecture.md「Provider 治理」)。
