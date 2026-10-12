# Courier Unity SDK (C#)

> 目标端。UPM 包结构,四包分离:Core / Service / UI(可选)/ Diagnostics(可选)。

## 快速开始

纯逻辑层零引擎依赖,dotnet 直跑实测(2026-10-13;完整说明见 `examples/README.md`):

```sh
cd gateway && go build -o /tmp/courier-gateway ./cmd/gateway && /tmp/courier-gateway  # 起本地网关(默认 :8080)
dotnet run --project examples/unity -- http://127.0.0.1:18099                          # 另开终端
```

输出:

```text
login: type=GUEST id=acc_...
announcements: 0
ticket: id=tkt_... status=OPEN
detail: messages=1
example: done
```

`announcements: 0` 属正常(本地网关无发布数据)。进引擎只换两件:传输换 `UnityWebRequest`(实现 `ITransport`)、存储换平台安全存储(实现 `ITokenStore`,红线:禁明文 PlayerPrefs),其余代码原样。

## 结构

```text
packages/
  com.courier.core/        L2 平台无关内核(CourierClient 门面 + Core/Identity/Session/Adapter)
  com.courier.service/     L2 域服务(九域 + DTO + messages SSE 流客户端,CourierServices 门面)
  com.courier.ui/          可选 UI 包(brandingCatalog + 公告/客服/助手/实名面板)
  com.courier.diagnostics/ 可选诊断包(crash/trace/performance/analytics,默认全关)
tests/
  ContractTests~/          契约测试(8 例,xunit)
  CoreTests~/              内核测试(48 例,xunit)
  ServiceTests~/           域服务测试(77 例,xunit)
  DiagnosticsTests~/       诊断包测试(11 例,xunit)
```

## 运行测试

各 `Tests~/` 下为纯逻辑 dotnet 工程(不依赖引擎运行时),本地直接验证:

```bash
cd sdks/unity/ContractTests~ && dotnet test
cd sdks/unity/CoreTests~ && dotnet test
cd sdks/unity/ServiceTests~ && dotnet test
cd sdks/unity/DiagnosticsTests~ && dotnet test
```

Unity Test Runner(需引擎本体;Window → General → Test Runner → EditMode/PlayMode)亦可跑同一批用例。
CI 门禁含上述四个纯逻辑 dotnet 工程(`.github/workflows/tests.yml` unity-dotnet job);引擎内 Test Runner 用例不进 CI(需引擎本体)。

## 约定

- DTO 以 `../../docs/contract/` 为唯一事实源,与其他端字段逐一对齐;禁止 `object`/`dynamic` 敷衍。
- 生命周期状态机(Uninitialized→…→PlayerReady、Suspended/Resuming)在 Core 实现,平台信号由 Adapter 注入。
- Adapter 禁带业务:只做网络(UnityWebRequest)、存储(加密存储,不用明文 PlayerPrefs)、生命周期、主线程调度。
- messages SSE 流:编排(退避/重连/解析)在 Core+Service 纯逻辑(dotnet 单测),Adapter 只接线(`UnityWebRequestSseSource`)。约束:`SendWebRequest` 须主线程调用;服务端必须 chunked 分框(Go net/http 网关缺省即满足);取消经 `CancellationToken` 净停(Abort 归一 `TransportException`)。
- UI / Diagnostics 包为独立可选 UPM 包;不装不影响功能面。诊断默认全关,开启必须同时配置端点,失败静默不反噬。
- 分支判断只认 wire code(`ErrorCode` 枚举);未登记码落兜底(容忍未知)。
