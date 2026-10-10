# Courier godot SDK

> 状态：契约层已落位（errors.gd / envelope.gd 由 tools/contractgen 生成，Godot 4.3 headless 契约测试全绿）；域实现（Identity / Session / 各域服务）规划中。

## 目标平台

- Godot Engine 4.3 (GDScript)

## 结构

```text
sdks/godot/
  contract/         契约生成物(errors.gd / envelope.gd,tools/contractgen 生成,# 注释风格)
  tests/            契约测试(tests/test_contract.gd,SceneTree 脚本,headless 跑)
```

## 运行测试

```sh
godot --headless --path sdks/godot --script tests/test_contract.gd
```

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

- DTO 以 `../../docs/contract/` 为唯一事实源；未知字段容忍。
- token 存储走 Godot 平台存储；UI 可选，游戏可自绘。
- 生成物注释一律 `#`（GDScript 无 `//`,Go/C 风格头部会被引擎判为语法错误）。
