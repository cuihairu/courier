# Courier miniprogram SDK

> 状态：规划中，待实现。

## 目标平台

- 微信小程序

## 模块（能力树，见 docs/architecture.md）

- Identity / Session: 登录、登出、绑定、设备；token 轮换
- Player: 档案与游戏角色（M3+）
- App: 版本 / 维护 / 远程配置 / 品牌（M3+）
- Communication: 公告 / 消息 / 推送
- Support: 工单 / FAQ（面板 UI 可选）
- Payment: 仅契约面（M5+）
- RealName: 实名（可选，合规，默认关）
- Diagnostics: 诊断（可选包，默认全关）

## 路线图

- M1: Identity / Session + 生命周期状态机
- M2: Communication + Support（RealName 后段）
- M3: App + Player + Branding + Diagnostics
- M4: Assistant → M5: Payment

## 约定

- DTO 以 `../../docs/contract/` 为唯一事实源。
- token 存储走微信 storage；UI 可选，游戏可自绘。
