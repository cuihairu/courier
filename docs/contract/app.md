# 应用状态契约(App)

> 状态:**Frozen v1** · 里程碑 M3 · 冻结日期 2026-10-09。
> 域前缀 `APP` 已注册([errors.md](./errors.md));本文档是 M3 服务端(默认供应商 **scribe**,与 config/branding 同管道)与各端 SDK 的一致性依据,冻结后只加不改,破坏性变更升版本(见 [versioning.md](./versioning.md))。

## 能力与范围

- 能力域 `app`(`providers.CapApp`),路由 `/v1/app/*`;版本开关、维护开关由接入方管理面设置,本契约是玩家侧只读投影。
- 本契约**不覆盖**:更新包分发与差分(接入方自建/商店渠道)、强制更新的合规弹窗文案(归 Branding)。

## 端点总览(挂载 `/v1/app/*`)

| 端点 | 认证 | 说明 |
| --- | --- | --- |
| `GET /v1/app/version` | 匿名可 | 版本检查(更新门槛判定) |
| `GET /v1/app/maintenance` | 匿名可 | 维护状态查询 |
| `GET /v1/app/environment` | 匿名可 | 环境回显(初始化校验用) |

- 三个端点**匿名可达**(维护时客户端仍需能查询状态);带有效 Bearer 亦可。
- scope header 要求同 [auth.md](./auth.md)(`X-Courier-Game-Id` / `X-Courier-Env`);缺失 → 对应通用码。
- 成功响应 HTTP 200,信封见 [primitives.md](./primitives.md)。

## 端点契约

### `GET /v1/app/version`

查询参数:`platform`(可选;`ios`/`android`/`windows`/...,大小写敏感)。

响应 `data`:

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `latestVersion` | string | 最新版本 |
| `minVersion` | string | 最低可用版本;低于即强制更新 |
| `updateUrl` | string? | 更新跳转地址(payload 可选) |
| `forceUpdate` | bool | 服务端判定:客户端版本 < minVersion 时为 true |

- 客户端版本经 `appVersion` 查询参数上报(缺省 = 不判定,`forceUpdate` 为 false)。
- `forceUpdate = true` 时客户端引导更新;受保护操作由服务端以 `426 APP_VERSION_UNSUPPORTED` 拦截(payload 可带 `updateUrl`,见 [errors.md](./errors.md))。

### `GET /v1/app/maintenance`

响应 `data`:

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `inMaintenance` | bool | 维护开关 |
| `estimatedRecoveryAt` | timestamp? | 预计恢复时间(可选) |
| `message` | string? | 维护公告文案(可选,单语) |

- 维护模式开启时,网关对**新会话**的认证/业务请求返回 `503 APP_MAINTENANCE`(payload 可带 `estimatedRecoveryAt`);已建立的会话不强制踢出。`/v1/app/*` 本域端点与 `/healthz` 不受维护拦截(否则客户端无从得知恢复)。

### `GET /v1/app/environment`

响应 `data`:

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `gameId` | string | 回显 scope 中的 game_id |
| `env` | string | 回显 scope 中的 env |

- 用途:SDK 初始化后校验 scope 配置与网关判定一致(排查接错环境)。

## 错误码(域表)

| code | HTTP | retryable | 说明 |
| --- | --- | --- | --- |
| `APP_MAINTENANCE` | 503 | false | 维护中(payload 带预计恢复时间,可选) |
| `APP_VERSION_UNSUPPORTED` | 426 | false | 版本过旧(payload 带下载地址,可选) |

其余(认证、scope、限流、能力关闭)一律通用码,见 [errors.md](./errors.md)。

## 客户端要求(各端 SDK)

- 启动流程:拉 `/v1/app/version` → `forceUpdate` 引导更新;拉 `/v1/app/maintenance` → 维护页;两者通过后进入登录。
- 收到 `503 APP_MAINTENANCE`:展示维护页并按 `estimatedRecoveryAt` 轮询本端点,不做其他请求。
- 收到 `426 APP_VERSION_UNSUPPORTED`:按 `updateUrl` 引导;SDK 不自动跳转商店。
- `501 COMMON_CAPABILITY_DISABLED` = App 能力未接:跳过版本/维护检查直接登录(能力安静地不存在)。
