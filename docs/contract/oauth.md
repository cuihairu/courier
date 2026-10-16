[English](./oauth.en.md) | [中文](./oauth.md)

# 第三方身份提供方契约(OAuth / SSO)

> 状态:**Frozen v1**(2026-10-12,批次 52 冻结;原 Draft v0.1 提案 2026-10-11 批次 49,按原文冻结不改语义)· 能力域 `oauth`(路由 `/v1/identity/oauth/*`)。
> 域前缀 `OAUTH` 随本域冻结入 [errors.md](./errors.md) v6(不动现有冻结行)。

## 为什么是独立域

- [auth.md](./auth.md) v1 冻结口径(第 10 行)明示「第三方 OAuth / 手机号」不覆盖,留给后续域契约扩展;F03 提案的扩展表亦约定「OAuth 域契约扩展时 `type` 加枚举值即覆盖」。
- 第三方提供方(Google / Apple / 微信 / Facebook / Line / Discord / Steam …)本质是**供应商**:验证机制(JWKS / introspection / code 换 token)、client secret、app 配置全在管理面,玩家契约只见「拿第三方凭证换 Courier 会话」。契约必须对「有哪些供应商」保持无感。
- 与 [auth.md](./auth.md) 的边界:本域**不重新设计账号、会话、token 模型**——`accounts` / `sessions` / `devices` 与 token 轮换、重放检测、scope 绑定全部复用 auth 域;本域只加「第三方身份 ↔ 账号」的映射与换绑/解绑端点。

## 能力与范围

- 能力域 `oauth`(`providers.CapOAuth`),默认供应商 **courier-account**(可换可关,同 auth 域供应商面)。
- 提供三件事:**用第三方身份换 Courier 会话**、**把第三方身份绑到已登录账号**、**解绑与查询绑定**。
- 本契约**不覆盖**:第三方 SDK 集成(接入方自行集成并取凭证)、手机号验证码登录(同构域,另提)、第三方账号资料(头像/昵称归 [player.md](./player.md) 或 Provider)、第三方账号注销(归 auth.md F03 提案的 `OnAccountDeleted` 钩子链)。

## 端点总览(挂载 `/v1/identity/oauth/*`)

| 端点 | 认证 | 说明 |
| --- | --- | --- |
| `POST /v1/identity/oauth/exchange` | 匿名 | 第三方凭证换 Courier 会话(无匹配即创建账号) |
| `POST /v1/identity/oauth/bind` | Bearer | 当前账号绑定第三方身份 |
| `DELETE /v1/identity/oauth/bindings/{provider}` | Bearer | 解绑当前账号的指定提供方 |
| `GET /v1/identity/oauth/bindings` | Bearer | 当前账号已绑定列表 |

- scope header、信封、分页、限流口径同 [auth.md](./auth.md) / [primitives.md](./primitives.md);成功响应 HTTP 200。
- 敏感端点(`exchange` / `bind`)并入 auth 域 IP 限流桶。

## 数据模型

沿用 auth 域 `accounts` / `sessions` / `devices`,本域只加一张映射表:

### account_identities(第三方身份映射,account 维度)

| 字段 | 说明 |
| --- | --- |
| `accountId` | → accounts.id |
| `provider` | 提供方标识(小写字母数字与连字符,如 `google` / `apple` / `wechat`);**管理面注册集合,契约不枚举** |
| `subject` | 第三方用户唯一标识(经第三方验证后取得的归一标识) |
| `createdAt` | 绑定时间 |

- `(provider, subject)` 全局唯一;`(accountId, provider)` 唯一。
- `accounts.type` 增枚举值 `OAUTH`(加法,随本域冻结一并落 auth.md):纯第三方登录创建的账号为 `OAUTH` 类型,无凭证行,行为对齐 `GUEST` 的最小凭证形态。

### 数据红线(内建,不采集即不承诺)

- **不存第三方凭证原文**:只存 `provider + subject` 映射;每次登录重新向第三方在线验证,不缓存 id_token / access_token。
- **不向第三方回传 Courier 数据**:验证是无请求侧交互(拉取式校验),不把 accountId / email / 设备信息推给任何第三方。
- `subject` 必须来自第三方验证结果,**客户端不得自报**;验证失败一律 `OAUTH_IDENTITY_INVALID`,不区分「无效」与「过期」(防枚举)。

## 端点契约

### POST /v1/identity/oauth/exchange — `{ provider, token, tokenType?, deviceId?, platform? }`

- `provider` 必填;未在管理面注册或未开启 → `OAUTH_PROVIDER_UNKNOWN`(400)。
- `token` 必填:第三方颁发的凭证;`tokenType` 可选枚举 `ID_TOKEN` / `ACCESS_TOKEN` / `CODE`,缺省 `ID_TOKEN`——具体校验方式由提供方实现决定(JWKS 验签 / introspection / code 换 token),**契约不描述校验细节**。
- 校验流程:服务端向第三方在线验证 → 取得 `subject` → 查 `account_identities` → 命中则登录该账号,未命中则创建 `type=OAUTH` 账号并写入映射 → 签发绑定 scope 的会话。
- 响应 `data` 与 [auth.md](./auth.md) `POST /login` 同构(`tokens` + `account` + `deviceId?`),`account.type = "OAUTH"`。
- 第三方凭证无效/过期/伪造 → `OAUTH_IDENTITY_INVALID`(401);能力域关闭 → `COMMON_CAPABILITY_DISABLED`(501,域级降级统一语义,见 [errors.md](./errors.md));已达设备上限 → `AUTH_DEVICE_LIMIT`(403)。

### POST /v1/identity/oauth/bind — `{ provider, token }`(Bearer)

- 当前账号绑定该第三方身份,供下次一键登录(同 provider 多次绑定 = 覆盖前先解绑,重复绑定同 provider 返回 `COMMON_INVALID_ARGUMENT`)。
- 该 `subject` 已绑定其他账号 → `OAUTH_IDENTITY_TAKEN`(409,提示已绑定其他账号);验证失败 → `OAUTH_IDENTITY_INVALID`(401)。

### DELETE /v1/identity/oauth/bindings/{provider}(Bearer)

- 解绑并删除映射行;幂等(不存在再删仍 200,返回 `data: {}`)。
- 解绑不影响账号本身——`type=OAUTH` 账号解绑后仍存在(下次可重新绑定);是否回收空账号归管理面策略。

### GET /v1/identity/oauth/bindings(Bearer)

- `data.items[]`:`{ provider, createdAt }`;按 `createdAt` 升序;不返回 `subject`(避免客户端侧泄露第三方标识)。

## 错误码(拟新增,随冻结入 errors.md)

| code | HTTP | retryable | 说明 |
| --- | --- | --- | --- |
| `OAUTH_PROVIDER_UNKNOWN` | 400 | false | 提供方未注册或未开启 |
| `OAUTH_IDENTITY_INVALID` | 401 | false | 第三方凭证验证失败/过期/伪造 |
| `OAUTH_IDENTITY_TAKEN` | 409 | false | 该第三方身份已绑定其他账号 |

域前缀 `OAUTH` 登记于 [errors.md](./errors.md) 注册表;其余(认证、scope、限流、能力关闭)一律通用码。

## 客户端要求(各端 SDK)

- SDK **不内置任何第三方 SDK**:接入方自行集成 Google / Apple / 微信等 SDK 取得凭证,调 `exchange`;Courier 只见一个 `{ provider, token }` 形状。
- 第三方凭证**只发一次**给本域端点,不得本地长期缓存;`OAUTH_IDENTITY_INVALID` → 引导重新授权,不自动重试。
- `OAUTH_IDENTITY_TAKEN` → 提示「已绑定其他账号」,可引导改用该账号登录;`OAUTH_PROVIDER_UNKNOWN` → 提示该登录方式不可用(可能未配置)。
- `COMMON_CAPABILITY_DISABLED`(oauth 域)→ 不展示对应登录入口。
- token 存储、刷新、重放检测全部复用 auth 域口径([auth.md](./auth.md) 客户端要求节),本域签发的会话行为与普通会话无差异。

## 扩展与演进

| 可能的扩展 | 兼容处理方式 |
| --- | --- |
| 新提供方(微信 / Line / Discord / Steam / Apple 中国区 …) | 管理面注册 + 提供方实现;`provider` 是开集字符串,**契约不动不升版** |
| 手机号验证码登录 | 同构域:另一认证域提案(短信链路有独立验证语义与滥用防护,不塞进本域) |
| OAuth 账号转正(绑邮箱) | 复用 auth 域 `bind` 语义扩展(写入 `account_credentials`),`type` 由 `OAUTH` 转 `EMAIL`;加字段不改表 |
| 账号级联删除 | 复用 auth.md F03 提案的 `OnAccountDeleted` 钩子链;`account_identities` 行随账号匿名化清理,`accounts.id` 保留 |
| 跨游戏身份贯通 | `account_identities` 挂公司级 account,天然跨游戏;但 token 跨游戏永不原地放开([scope.md](./scope.md) 红线) |
| 同一 provider 多标识(微信 openid vs unionid) | Provider 配置层面归一到单一 `subject`,契约只见归一后的标识 |
| 第三方资料展示(头像 / 昵称) | 归 [player.md](./player.md) 或 Provider,不进本域(字段最小化) |
| 第三方凭证轮换 / 长期免登 | **不存第三方凭证**是红线,不因扩展松动;每次登录重新验证 |
| 注销申诉 / 处罚拦截 | 归 support 域 F45 提案;本域签发的会话同样受其约束 |
