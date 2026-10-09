# Courier Unity SDK (C#)

> 目标端。UPM 包结构,四包分离:Core / Service / UI(可选)/ Diagnostics(可选)。

## 结构

```text
packages/
  com.courier.core/        L2 平台无关内核(CourierClient 门面 + Core/Identity/Session/Adapter)
  com.courier.service/     L2 域服务(九域 + DTO,CourierServices 门面)
  com.courier.ui/          可选 UI 包(brandingCatalog + 公告/客服/助手/实名面板)
  com.courier.diagnostics/ 可选诊断包(crash/trace/performance/analytics,默认全关)
tests/
  ContractTests~/          契约测试(8 例,xunit)
  CoreTests~/              内核测试(29 例,xunit)
  ServiceTests~/           域服务测试(60 例,xunit)
  DiagnosticsTests~/       诊断包测试(11 例,xunit)
```

## 运行测试

Unity Test Runner(需引擎本体;Window → General → Test Runner → EditMode/PlayMode)。
CI 不覆盖 Unity(标准 runner 无法 headless 跑引擎测试),本地跑。

## 约定

- DTO 以 `../../docs/contract/` 为唯一事实源,与其他端字段逐一对齐;禁止 `object`/`dynamic` 敷衍。
- 生命周期状态机(Uninitialized→…→PlayerReady、Suspended/Resuming)在 Core 实现,平台信号由 Adapter 注入。
- Adapter 禁带业务:只做网络(UnityWebRequest)、存储(加密存储,不用明文 PlayerPrefs)、生命周期、主线程调度。
- UI / Diagnostics 包为独立可选 UPM 包;不装不影响功能面。诊断默认全关,开启必须同时配置端点,失败静默不反噬。
- 分支判断只认 wire code(`ErrorCode` 枚举);未登记码落兜底(容忍未知)。
