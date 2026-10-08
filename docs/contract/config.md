# 远程配置契约(Remote Config)

> 状态:**Frozen v1** · 里程碑 M3 · 冻结日期 2026-10-09。
> 域前缀 `CONFIG` 已注册([errors.md](./errors.md));本文档是 M3 服务端(默认供应商 **scribe**,与 branding/app 同管道)与各端 SDK 的一致性依据,冻结后只加不改,破坏性变更升版本(见 [versioning.md](./versioning.md))。
> 调研落点:必备链路 F47(远程配置/Feature Flag)、F48(分群与条件定向)、F50(版本历史与回滚),见 [features.md §14](../research/features.md)。

## 能力与范围

- 能力域 `app`(`providers.CapApp`),路由 `/v1/app/*`;本契约是**玩家侧只读投影**——键值编辑、条件规则、发布与回滚全部走 Provider 管理面(接入方后台或 croupier 发布),不经网关玩家侧,与公告同模式。
- 键值模型:扁平 `key → value`,`value` 为任意 JSON 值(字符串/数字/布尔/对象;复杂结构客户端自行解析)。键名 1–128 字符,`[a-zA-Z0-9_.-]`;单份配置集 ≤200 键,单值 ≤32 KiB。
- 版本语义(F50):每次发布产生新 `configVersion`(全局单调递增);**回滚 = 以历史内容发布新版本号**,玩家侧只见 version 递增,不存在 version 回退。
- 条件与灰度(F48)是**管理面语义**:条件维度、受众分群、放量百分比的 DSL 不进玩家契约。玩家侧投影规则:
  - 请求参数(可选):`platform` / `appVersion` / `region`;缺省维度不参与过滤。
  - 同一请求参数与账号下,同一 `configVersion` 内容**稳定**(灰度期间同账号不抖动)。
  - 未命中任何键的客户端拿到空 `items`,不是错误。
- 实时热生效经 [messages.md](./messages.md) 通道 `config.updated` 事件(见下);通道关闭时以启动/进前台拉取兜底。
- 本契约**不覆盖**:A/B 实验与指标显著性(F49,Provider 扩展位)、配置审计与改动人记录(F50,管理面职责,玩家侧不暴露)。

## 端点总览(挂载 `/v1/app/*`)

| 端点 | 认证 | 说明 |
| --- | --- | --- |
| `GET /v1/app/config` | Bearer | 命中当前条件的键值全量 + `configVersion` |

- 认证与 scope header 要求同 [auth.md](./auth.md);缺失 → 对应通用码。
- 空配置集返回 `200 {"configVersion": n, "items": {}}`,不是 404;`CONFIG_NOT_FOUND` 保留给 v1.1 单键查询,本版不使用。

## 数据模型

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `configVersion` | int | 单调递增;热生效判据(与本地缓存比对,相同则无操作) |
| `items` | object | `key → JSON value` 映射;键与值上限见「能力与范围」 |

响应示例:

```json
{
  "configVersion": 7,
  "items": {
    "shop_switch": true,
    "pvp_ratio": 1.5,
    "banner": { "img": "summer.png", "jump": "activity_01" }
  }
}
```

## 推送事件(经 messages.md 通道)

| event type | data | 语义 |
| --- | --- | --- |
| `config.updated` | `{"configVersion": 7}` | 管理面发布新版本;客户端据此重拉 |

- 事件**广播给所有已连接客户端**,服务端不感知各端条件命中与否:客户端收到后重拉 `/v1/app/config`,比对本端缓存 `configVersion`——相同则无操作,不同则以新拉取结果热生效(命中集没变的客户端 version 不变,自然零开销)。
- data 结构只带 version,不带键值;禁止另造字段(与 announcement.published 同模式)。

## 错误码(域表)

本域玩家侧端点**无新增错误码**:参数/认证/限流/能力关闭一律通用码([errors.md](./errors.md));`CONFIG_NOT_FOUND`(404)为 v1.1 单键查询预留。

## 客户端要求(各端 SDK)

- 启动与进前台时拉取;`config.updated` 事件到达后重拉比对 version,相同即忽略(不重渲染)。
- `501 COMMON_CAPABILITY_DISABLED` = 配置能力未接:SDK 返回空结果/「未启用」态,接入方据此使用内建默认值,**不进报错路径**。
- 值解析失败(接入方结构变更)是使用方错误:SDK 按原始 JSON 值返回,不做二次校验。
- 客户端可缓存上次 `configVersion` 与内容;重拉失败时沿用缓存(配置是加速器,不是依赖)。
