# Courier Unreal Engine SDK (C++)

> M2 跟进端（Unity 稳定后对齐）。UE 插件结构。

## 计划结构

```text
Courier/
  Source/
    CourierRuntime/
      Public/  CourierClient、Identity、Session、Player、App、Communication、Support、Payment、RealName
      Private/
    CourierEditor/
  UI/      可选：UMG 组件（登录/公告/客服面板，消费 Branding）
  Courier.uplugin
```

## 约定

- 与其他端共享 `../../docs/contract/` 契约，DTO 字段逐一对应。
- HTTP 走 UE Http 模块，WebSocket（M2）走引擎 WebSockets 插件；生命周期信号（前后台）注入状态机。
- token 走平台安全存储，不落明文配置文件。
- UI 组件（UMG）属可选部分，消费 Branding 下发，游戏也可用 UMG 自绘。
