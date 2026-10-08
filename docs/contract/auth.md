# 认证契约(Auth / Identity)

> 状态:**Frozen v1** · 里程碑 M1 · 冻结日期 2026-10-08。
> 域前缀 `AUTH` 已注册([errors.md](./errors.md));本文档是 M1 服务端(AccountProvider 自建实现)与各端 SDK 的一致性依据,冻结后只加不改,破坏性变更升版本(见 [versioning.md](./versioning.md))。

## 能力与范围

- 认证方式:**邮箱+密码**、**游客(设备)**。
- 账号(`account`)是**公司级实体**,跨游戏存在;会话(session)签发时绑定 scope `game_id + env`([scope.md](./scope.md))——同一账号在不同游戏各自登录,token 不可跨游戏使用。
- 本契约**不覆盖**:player 档案(M3)、实名(realname.md)、第三方 OAuth / 手机号(后续域契约扩展)。

## 端点总览(挂载 `/v1/identity/*`)

| 端点 | 认证 | 说明 |
| --- | --- | --- |
| `POST /v1/identity/register` | 匿名 | 邮箱+密码注册并登录 |
| `POST /v1/identity/login` | 匿名 | 邮箱+密码登录 |
| `POST /v1/identity/guest` | 匿名 | 游客登录(按设备幂等:同设备回到同一游客账号) |
| `POST /v1/identity/bind` | Bearer | 游客账号绑定邮箱(转正),M1 链路「绑定邮箱」 |
| `POST /v1/identity/refresh` | 匿名 | refresh token 轮换 |
| `POST /v1/identity/logout` | Bearer | 吊销当前会话 |
| `GET /v1/identity/session` | Bearer | 当前会话信息 |
| `GET /v1/identity/devices` | Bearer | 已绑定设备列表 |
| `DELETE /v1/identity/devices/{deviceId}` | Bearer | 解绑设备并吊销其全部会话 |

- 认证列 `Bearer` = 请求头 `Authorization: Bearer <accessToken>`;缺失/格式错 → `COMMON_UNAUTHENTICATED`(401)。
- 所有请求必带 scope header(`X-Courier-Game-Id` / `X-Courier-Env`,见 [scope.md](./scope.md));缺失 → `COMMON_INVALID_ARGUMENT`。
- 成功响应一律 HTTP 200,信封见 [primitives.md](./primitives.md)。

## 数据模型

四张表,字段为契约口径(实现自建存储,列名可异,语义不得漂移);DDL 附录见文末。

### accounts(账号,公司级)

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | id | 服务端生成,UUIDv7,前缀 `acc_` |
| `type` | 枚举 | `EMAIL` / `GUEST` |
| `email` | email? | `EMAIL` 账号必填、全局唯一;`GUEST` 为空 |
| `status` | 枚举 | `ACTIVE` / `DISABLED`;`DISABLED` 登录与已签发会话均拒(`AUTH_ACCOUNT_DISABLED`) |
| `createdAt` / `updatedAt` | timestamp | RFC 3339 毫秒 UTC |

### account_credentials(凭证,1:1 accounts)

| 字段 | 说明 |
| --- | --- |
| `accountId` | → accounts.id |
| `passwordHash` / `salt` | PBKDF2-HMAC-SHA256 存储;**禁止明文、禁止可逆** |
| `algo` / `iterations` | 算法标识与迭代数(如 `PBKDF2-SHA256` / `210000`) |
| `updatedAt` | 密码变更时间 |

`GUEST` 账号无凭证行;`bind` 成功后写入。

### sessions(会话,scope 维度)

| 字段 | 说明 |
| --- | --- |
| `id` | UUIDv7,前缀 `ses_` |
| `accountId` | → accounts.id |
| `gameId` / `env` | 签发时绑定的 scope(唯一事实源,下游不得从 path/payload 读) |
| `deviceId`? | 绑定设备(见 devices) |
| `accessTokenHash` / `refreshTokenHash` | 只存 SHA-256,不存原文 |
| `previousRefreshTokenHash` | 上一代 refresh 哈希,**重放检测**用 |
| `accessExpiresAt` / `refreshExpiresAt` | access 默认 15 分钟、refresh 默认 30 天(接入方可配,契约不冻结具体值) |
| `revokedAt`? | 吊销时间;非空即拒(`AUTH_TOKEN_REVOKED`) |

### devices(设备绑定,account 维度)

| 字段 | 说明 |
| --- | --- |
| `id` | UUIDv7,前缀 `dev_`(服务端行 ID,区别于客户端 `deviceId`) |
| `accountId` / `deviceId` | `(accountId, deviceId)` 唯一;`deviceId` 为客户端提供的不透明标识(1–128 字符) |
| `platform`? | 客户端自报(`ios` / `android` / `windows` / …,不校验枚举) |
| `createdAt` / `lastSeenAt` | 绑定与最近活跃 |

- 设备数上限默认 **5**/账号(接入方可配);已达上限再绑新设备 → `AUTH_DEVICE_LIMIT`(403)。
- 解绑(`DELETE /devices/{deviceId}`)删除绑定行并**吊销该设备全部会话**;解绑后同设备可重新绑定(生成新行)。
- 游客账号按 `deviceId` 幂等:存在 `type=GUEST` 且绑定该设备的账号即直接登录,否则创建新游客账号并绑定。

## Token 模型

- **不透明随机 token**(64 位 hex,服务端 `crypto/rand` 生成),无 JWT;网关查 `sessions` 表验证。JWT 与签名验签归后续域契约(若需要),本契约不引入。
- `accessToken`:短时效(默认 15 min);`refreshToken`:长时效(默认 30 天)。
- **轮换**:每次 `refresh` 成功即签发全新 access+refresh,旧 access **立即失效**,旧 refresh 记入 `previousRefreshTokenHash`。
- **重放检测**:已被轮换的 refresh 再次出现(`== previousRefreshTokenHash`)视为令牌泄露 → `AUTH_REFRESH_REUSED`(401)并**吊销整个会话**。
- **scope 绑定**:token 验证时,session 的 `gameId+env` 与请求 scope 不一致 → `SCOPE_MISMATCH`(400,见 [scope.md](./scope.md))。

## 端点契约

### POST /register — `{ email, password, deviceId?, platform? }`

- 校验:email 格式、password 8–128 字符、deviceId 1–128 字符 → 违反 `COMMON_INVALID_ARGUMENT`(400)。
- email 已注册 → `AUTH_EMAIL_TAKEN`(409)。
- 成功:创建 `EMAIL` 账号 + 凭证 + (可选)设备绑定 + 会话,签发 tokens。

### POST /login — `{ email, password, deviceId?, platform? }`

- 账号不存在或密码不符 → `AUTH_INVALID_CREDENTIALS`(401);**两种情况返回完全一致**(防账号枚举)。
- 账号 `DISABLED` → `AUTH_ACCOUNT_DISABLED`(403)。
- 新设备且已达上限 → `AUTH_DEVICE_LIMIT`(403)。
- 成功:签发会话(绑定 deviceId 时自动绑定)。

### POST /guest — `{ deviceId, platform? }`

- `deviceId` 必填。
- 成功:按设备幂等签发会话(`GUEST` 账号);响应 `account.type = "GUEST"`。

### POST /bind — `{ email, password }`(Bearer)

- 当前账号须为 `GUEST`,已是 `EMAIL` → `COMMON_INVALID_ARGUMENT`(400)。
- email 已被其他账号注册 → `AUTH_EMAIL_TAKEN`(409)。
- 成功:账号转 `EMAIL`、写入凭证;**已绑定设备与在途会话保留**,token 不换发。

### POST /refresh — `{ refreshToken }`

| 情形 | 结果 |
| --- | --- |
| `refreshToken` 匹配当前代且未过期 | 轮换:新 access+refresh,旧代记入 previous |
| 匹配当前代但 refresh 已过期 | `AUTH_TOKEN_EXPIRED`(401,客户端应重登) |
| 匹配上一代(已轮换) | `AUTH_REFRESH_REUSED`(401)+ 吊销整个会话 |
| 均不匹配 / 会话已吊销 | `AUTH_INVALID_CREDENTIALS` / `AUTH_TOKEN_REVOKED`(401) |

### POST /logout(Bearer)

吊销当前会话;幂等(已吊销再吊销仍 200)。成功 `data: {}`。

### GET /session(Bearer)

```json
{ "data": { "account": { }, "sessionId": "ses_…", "deviceId": "…", "accessExpiresAt": "…" } }
```

### GET /devices(Bearer)

`data.items[]`:`{ id, deviceId, platform?, createdAt }`;无 `nextCursor` 即末页([primitives.md](./primitives.md))。

### DELETE /devices/{deviceId}(Bearer)

解绑并吊销该设备全部会话(含当前会话,若当前会话正使用该设备);设备不存在 → `COMMON_NOT_FOUND`(404)。

## 错误码(域表)

引用 [errors.md](./errors.md) 已冻结码:`COMMON_INVALID_ARGUMENT` / `COMMON_UNAUTHENTICATED` / `COMMON_NOT_FOUND` / `RATE_LIMITED` / `SCOPE_MISMATCH` / `AUTH_INVALID_CREDENTIALS` / `AUTH_TOKEN_EXPIRED` / `AUTH_TOKEN_REVOKED` / `AUTH_REFRESH_REUSED` / `AUTH_ACCOUNT_DISABLED` / `AUTH_DEVICE_LIMIT`。

本契约冻结新增:

| code | HTTP | retryable | 说明 |
| --- | --- | --- | --- |
| `AUTH_EMAIL_TAKEN` | 409 | false | 邮箱已被注册 |

## 基础限流

- 敏感端点(`register` / `login` / `guest` / `bind` / `refresh`)按**客户端 IP** 独立限流:默认 **10 次/分钟、桶容量 10**(接入方可配);超出 → `RATE_LIMITED`(429)+ `Retry-After`。
- 其余端点走网关全局限流;多实例协同限流随 M1 后续工程。

## 客户端要求(各端 SDK)

- token 存储走 Adapter 层安全存储(ITokenStore),**禁止明文落盘**(见 layers.md L3)。
- 收到 `AUTH_TOKEN_EXPIRED` → 自动 refresh 一次后重放原请求;`AUTH_REFRESH_REUSED` / `AUTH_TOKEN_REVOKED` / `AUTH_INVALID_CREDENTIALS` → 清除本地会话并回到未认证态。
- `COMMON_CAPABILITY_DISABLED`(identity 域)→ SDK 报告「账号能力未开启」,不进入报错路径。

## 附录:参考 DDL(SQLite 方言,实现可自选存储)

```sql
CREATE TABLE accounts (
  id          TEXT PRIMARY KEY,            -- acc_<uuidv7>
  type        TEXT NOT NULL,               -- EMAIL | GUEST
  email       TEXT UNIQUE,                 -- EMAIL 必填全局唯一;GUEST 为 NULL
  status      TEXT NOT NULL DEFAULT 'ACTIVE',
  created_at  TEXT NOT NULL,               -- RFC3339 ms UTC
  updated_at  TEXT NOT NULL
);

CREATE TABLE account_credentials (
  account_id  TEXT PRIMARY KEY REFERENCES accounts(id),
  password_hash BLOB NOT NULL,             -- PBKDF2-HMAC-SHA256
  salt        BLOB NOT NULL,
  algo        TEXT NOT NULL,
  iterations  INTEGER NOT NULL,
  updated_at  TEXT NOT NULL
);

CREATE TABLE sessions (
  id            TEXT PRIMARY KEY,          -- ses_<uuidv7>
  account_id    TEXT NOT NULL REFERENCES accounts(id),
  game_id       TEXT NOT NULL,
  env           TEXT NOT NULL,
  device_id     TEXT,                      -- → devices.device_id(可空)
  access_token_hash  BLOB NOT NULL,
  refresh_token_hash BLOB NOT NULL,
  previous_refresh_token_hash BLOB,        -- 重放检测
  access_expires_at  TEXT NOT NULL,
  refresh_expires_at TEXT NOT NULL,
  created_at    TEXT NOT NULL,
  revoked_at    TEXT
);

CREATE TABLE devices (
  id          TEXT PRIMARY KEY,            -- dev_<uuidv7>
  account_id  TEXT NOT NULL REFERENCES accounts(id),
  device_id   TEXT NOT NULL,
  platform    TEXT,
  created_at  TEXT NOT NULL,
  last_seen_at TEXT,
  UNIQUE (account_id, device_id)
);
```
