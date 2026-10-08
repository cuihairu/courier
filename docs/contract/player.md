# 玩家档案契约(Player Profile)

> 状态:**Frozen v1** · 里程碑 M3 · 冻结日期 2026-10-09。
> 本文档是 M3 服务端(默认供应商 **archivist**,自建轻量)与各端 SDK 的一致性依据,冻结后只加不改,破坏性变更升版本(见 [versioning.md](./versioning.md))。
> 定位:账号↔角色映射与账号档案;**角色数据归各游戏**(等级/背包/进度不经 Courier)。

## 能力与范围

- 能力域 `player`(`providers.CapPlayer`),路由 `/v1/player/*`;全部要求 Bearer。
- 账号档案 = 跨游戏的展示名(登录账号的门面);角色映射 = 本 scope(`game_id + env`)下的 `playerId` 绑定记录,游戏侧用 `playerId` 回自己的游戏服取数据。
- 本契约**不覆盖**(红线「字段最小化」):角色数据与进度(归各游戏服)、好友/社交关系、档案头像资源托管(只存 URL 字符串,素材归接入方对象存储)。
- 不采集:邮箱/手机号不在本域回显(auth 域已有);无任何设备或行为字段。

## 端点总览(挂载 `/v1/player/*`)

| 端点 | 认证 | 说明 |
| --- | --- | --- |
| `GET /v1/player/profile` | Bearer | 账号档案 |
| `PATCH /v1/player/profile` | Bearer | 修改展示名 |
| `GET /v1/player/characters` | Bearer | 本游戏已绑定角色映射 |
| `POST /v1/player/characters` | Bearer | 绑定角色映射(幂等) |

- scope header 要求同 [auth.md](./auth.md);成功 HTTP 200,信封见 [primitives.md](./primitives.md)。

## 数据模型

profile:

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `displayName` | string | 展示名;1–30 字符(修剪首尾空白后) |
| `avatarUrl` | string? | 头像地址(可选;只存 URL,素材归接入方) |
| `createdAt` / `updatedAt` | timestamp | 档案创建/最近修改 |

character(角色映射):

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `playerId` | string | 游戏侧角色 ID;1–64 字符,由各游戏定义与解释 |
| `boundAt` | timestamp | 绑定时间 |

- 绑定上限:每账号每 scope ≤50 条;超出 → `COMMON_INVALID_ARGUMENT`(接入方按「先解绑再绑定」的产品流程处理;v1 不做解绑端点,预留 v1.1)。
- `POST /v1/player/characters` **幂等**:重复绑定同 `playerId` 返回既有记录(200),不是错误。

## 错误码(域表)

本域**无新增错误码**:参数非法(展示名长度、playerId 长度、超绑定上限)一律 `COMMON_INVALID_ARGUMENT`,认证/限流/能力关闭一律通用码([errors.md](./errors.md))。

## 客户端要求(各端 SDK)

- `PATCH` 成功后以响应体为准(服务端修剪后的 displayName);
- 绑定幂等:重复调用不报错,以返回的既有记录为准;
- `501 COMMON_CAPABILITY_DISABLED` = 能力未接:SDK 返回「未启用」态(null),接入方隐藏档案入口,不进报错路径。
