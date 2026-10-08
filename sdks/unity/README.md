# Courier Unity SDK (C#)

> M1 目标端。UPM 包结构。

## 计划结构

```text
Runtime/
  CourierClient.cs        入口：Init(gameId, env, endpoint)
  Auth/                login / logout / refresh / bindDevice（M1）
  Announcements/       （M2）
  Support/             （M2）
  Assistant/           （M3）
  Payments/            （M4）
  Core/                DTO、token 安全存储、重试、错误模型
Editor/
Samples~/
package.json
```

## 约定

- DTO 以 `../../docs/api/` 为唯一事实源，与 UE/Cocos 端字段逐一对齐。
- 公共类型集中定义，禁止 `object`/`dynamic` 敷衍（同生态类型约定）。
- token 存储不使用明文 PlayerPrefs。
