# Courier Cocos Creator SDK (TypeScript)

> M2 起步（Auth + Announcements），与 Unity 端同构。

## 计划结构

```text
src/
  CourierClient.ts        入口：init({ gameId, env, endpoint })
  auth/                （M2 起步）
  announcements/       （M2）
  support/             （M2 后期）
  assistant/           （M3）
  payments/            （M4）
  core/                DTO、token 存储、重试、错误模型
```

## 约定

- DTO 以 `../../docs/api/` 为唯一事实源。
- 禁止 `any`/未收窄 `unknown`；公共类型集中在 `core/types.ts`（同生态类型约定）。
- token 存储按平台隔离（native localStorage / 微信小游戏 storage）。
