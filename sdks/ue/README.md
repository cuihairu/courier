# Courier Unreal Engine SDK (C++)

> M2 跟进端（Unity 稳定后对齐）。UE 插件结构。

## 计划结构

```text
Courier/
  Source/
    CourierRuntime/
      Public/  CourierClient、Auth、Announcements、Support、Assistant、Payments
      Private/
    CourierEditor/
  Courier.uplugin
```

## 约定

- 与 Unity/Cocos 端共享 `../../docs/api/` 契约，DTO 字段逐一对应。
- HTTP 走 UE Http 模块，WebSocket（M2）走引擎 WebSockets 插件。
- token 走平台安全存储，不落明文配置文件。
