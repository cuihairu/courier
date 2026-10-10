# Courier godot SDK

> 状态：M1 core 已落位（生命周期状态机 + Transport/TokenStore 抽象 + ApiClient + Identity/Session + CourierClient 门面，Godot 4.3 headless 三套测试全绿）；域服务（Communication / Support 等）按路线图推进。

## 目标平台

- Godot Engine 4.3 (GDScript)

## 结构

```text
sdks/godot/
  project.godot     工程清单(class_name 全局类解析需 --import 生成类缓存)
  contract/         契约生成物(errors.gd / envelope.gd,tools/contractgen 生成,# 注释风格)
  src/core/         L2 core(平台无关):lifecycle / transport / tokenStore /
                    courierError / apiClient / identity / session / courierClient
  tests/            SceneTree 脚本测试(headless 跑,不依赖场景)
```

## 运行测试

首次先导入生成类缓存(`.godot/`,已 gitignore),之后三条测试:

```sh
godot --headless --path sdks/godot --import
godot --headless --path sdks/godot --script tests/test_contract.gd
godot --headless --path sdks/godot --script tests/test_lifecycle.gd
godot --headless --path sdks/godot --script tests/test_core.gd
```

本机无 Godot 二进制时,契约一致性由 tools/contractgen 兜底(fixture↔生成物漂移检查)。

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

- M1: Identity / Session + 生命周期状态机 —— **已完成**(core 全绿;HTTP 真传输与平台 TokenStore 待引擎集成面)
- M2: Communication + Support（RealName 后段）
- M3: App + Player + Branding + Diagnostics
- M4: Assistant → M5: Payment

## 约定

- DTO 以 `../../docs/contract/` 为唯一事实源；未知字段容忍。
- GDScript 无异常:错误一律 `{ok:false, error}` Dictionary;`await` 为协程必需。
- token 存储走 Godot 平台存储；UI 可选，游戏可自绘。
- 生成物注释一律 `#`（GDScript 无 `//`,Go/C 风格头部会被引擎判为语法错误）。
