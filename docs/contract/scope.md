# Scope 契约:game_id + env

> 状态:Draft v0。与生态(Croupier / Oddsmaker)同一隔离模型。

## 规则

- 隔离单元是二元组 `game_id + env`,全局唯一。
- SDK 初始化时指定**一次**(`CourierClient.Init({ gameId, env, endpoint })`),之后对调用方不可见。
- **URL path 与 payload 一律不携带 scope 字段**;scope 只走 header。

## 传输

| header | 说明 |
| --- | --- |
| `X-Courier-Game-Id` | 游戏标识,SDK 每请求自动携带 |
| `X-Courier-Env` | 环境枚举:`dev` / `staging` / `prod`(注册表制,可扩展) |

## 网关校验

1. 两个 header 缺失或不在注册表 → `COMMON_INVALID_ARGUMENT`。
2. 网关以 header 为唯一事实源注入下游;下游实现不得从 path/payload 读取 scope。
3. token 签发时绑定 scope;用 A 游戏 scope 的 token 访问 B 游戏 scope 的资源 → `SCOPE_MISMATCH`(账号是公司级实体,跨游戏要重新走登录或未来的 token exchange,不共用 player 级 token)。

## 与账号模型的关系

- `account` 是公司级实体,跨游戏存在;`player` 是 game 维度实体。
- 因此同一账号在 M1 中按 `game_id + env` 各自登录,产出各自 scope 的会话;「一个账号登录所有游戏」指账号可复用,不是 token 可跨游戏。
