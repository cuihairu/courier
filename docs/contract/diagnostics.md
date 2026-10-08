# 诊断契约(Diagnostics)——可选能力

> 状态:Draft v0 · 可选包(不进核心包)· 默认全关 · 里程碑 M3 同期或其后。
> 哲学:**未配置即 no-op,零开销**(与 atlas 同哲学)——不初始化实例、不起后台线程、不建队列。

## 红线(内建,Provider 不得覆盖)

1. **默认关闭**:四类能力全部 off。
2. **显式开启**:每类独立开关(`diagnostics.crash.enabled` 等),开启必须同时配置上报端点。
3. **开启即明示**:下文数据范围表即对外口径;接入方隐私政策须覆盖所选类别。
4. **关闭即零上报**:关闭后不留缓冲、不发缓存队列、不补报。
5. **敏感接口隔离**:实名等敏感接口的请求/响应体永不进入任何诊断采集(全局红线,非本模块可配)。

## 四类能力与 Provider

| 类别 | 内容 | 适配 |
| --- | --- | --- |
| Crash / Error Tracking | 未捕获异常、错误上报、breadcrumb | Sentry 协议(Sentry SaaS 或自托管 GlitchTip / Sentry) |
| Trace | 分布式追踪 | **官方 OTel SDK**,OTLP 导出,接任意 OTLP collector(自托管优先,云可接) |
| Performance | 启动耗时、卡顿帧、网络 RTT | OTel Metrics 或 Sentry 性能面,按 Provider 能力映射 |
| Event Analytics | 游戏埋点 | 自有 HTTP 端点(默认)或接入方指定 |

每类 = 一个 `DiagnosticsProvider` 接口;SDK 侧统一接口,实现可插拔(自托管优先,云服务可接);可只开其中一类。

## 上报 Schema 与数据范围

**Crash / Error**

| 采集 | 不采集 |
| --- | --- |
| sessionId、app/engine 版本、platform、os 版本、设备型号、堆栈、breadcrumb(接入方自行添加)、traceId | 位置、通讯录、相册、剪贴板、实名等敏感接口报文 |

**Trace**

resource 属性(固定集):`service.name=courier.sdk`、`courier.game_id`、`courier.env`、`platform`、`app.version`、`engine.version`。span 内不采集任何请求体;URL 只记 path,不记 query。

**Performance**

指标名注册表:`startup_duration_ms` / `frame_jank_ms` / `network_rtt_ms`;值 + 维度(platform、app.version),无设备标识维度。

**Event Analytics**

`{ eventName, props, sessionId }`;`props` 仅原始类型,总大小 ≤ 1KB;事件名与 props 结构由接入方登记,Courier 不做服务端字段解释。

## 分包

- 归 **Diagnostics 可选包**;核心包不携带任何诊断代码路径(含 no-op 路径)。
- 开启即意味着接入方接受该类别的数据范围表;运行时关闭开关立即停止采集并丢弃未发缓冲。
