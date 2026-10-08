# Courier Cocos Creator SDK (TypeScript)

> M2 起步（Identity/Session + Communication），与 Unity 端同构。

## 计划结构

```text
src/
  CourierClient.ts        入口：init({ gameId, env, endpoint })
  core/                生命周期状态机、DTO、token 存储、重试、错误枚举
  identity/            （M2 起步）
  session/             （M2 起步）
  communication/       announcements / push（M2）
  support/             （M2 后期）
  app/                 version / maintenance / config / branding（M3）
  player/              （M3）
  realname/            可选，合规（M2 后段）
  payment/             （M5，仅契约面）
  diagnostics/         可选包（默认全关，no-op）
ui/                    可选 UI 包：公告栏 / 客服页（消费 Branding，可自绘替代）
```

## 约定

- DTO 以 `../../docs/contract/` 为唯一事实源；禁止 `any`/未收窄 `unknown`，公共类型集中在 `core/types.ts`。
- token 存储按平台隔离（native localStorage / 微信小游戏 storage）。
- UI 包可选，不装不影响功能面。
