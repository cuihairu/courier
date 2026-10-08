# 诊断契约(Diagnostics)——可选能力

> 状态:**Frozen v1**(2026-10-09,M3)· 可选包(不进核心包)· 默认全关。
> 哲学:**未配置即 no-op,零开销**——不初始化实例、不起后台线程、不建队列。
> 诊断数据直发接入方自选端点(Sentry / OTLP collector / 自有 HTTP),**不经 Courier 网关**;网关与默认供应商均不感知本域。

## 红线(内建,Provider 不得覆盖)

1. **默认关闭**:四类能力全部 off。
2. **显式开启**:每类独立开关(`diagnostics.crash.enabled` 等),开启必须同时配置上报端点;只开端点不开启关 = 关。
3. **开启即明示**:下文数据范围表即对外口径;接入方隐私政策须覆盖所选类别。
4. **关闭即零上报**:关闭后不留缓冲、不发缓存队列、不补报。v1 上报为直发不缓冲,该红线由构造保证(见「发送语义」)。
5. **敏感接口隔离**:实名等敏感接口的请求/响应体永不进入任何诊断采集(全局红线,非本模块可配)。
6. **永不反噬游戏**:诊断上报失败(网络错/非 2xx)静默丢弃,不重试、不缓存、不抛错、不阻塞调用线程。

## 四类能力与 Provider 生态

| 类别 | 内容 | Provider 生态 |
| --- | --- | --- |
| Crash / Error Tracking | 未捕获异常、错误上报、breadcrumb | Sentry 协议(Sentry SaaS 或自托管 GlitchTip / Sentry) |
| Trace | 分布式追踪 | 官方 OTel SDK,OTLP 导出,接任意 OTLP collector(自托管优先) |
| Performance | 启动耗时、卡顿帧、网络 RTT | OTel Metrics 或 Sentry 性能面,按 Provider 能力映射 |
| Event Analytics | 游戏埋点 | 自有 HTTP 端点(默认)或接入方指定 |

SDK 侧统一接口(每类一个 Provider 接口),实现可插拔;可只开其中一类。Courier 内置实现为**自有 HTTP 端点直发**(下文 wire schema);Sentry / OTLP 适配由接入方按同一接口接入生态 SDK,契约不绑定其私有 wire。

## 配置形状(每类同构)

```json
{
  "crash":       { "enabled": false, "endpoint": "https://diag.example.com/crash" },
  "trace":       { "enabled": false, "endpoint": "https://diag.example.com/trace" },
  "performance": { "enabled": false, "endpoint": "https://diag.example.com/metrics" },
  "analytics":   { "enabled": false, "endpoint": "https://diag.example.com/events" }
}
```

- `enabled=false`(缺省)→ 该类接口为 no-op:不初始化实例、不起线程、不建队列,调用零副作用。
- 运行时开关:关闭立即生效——后续调用变 no-op,未发出的数据丢弃(v1 无未发缓冲,天然满足)。
- 资源上下文(game_id/env/platform/app 版本/engine 版本/sessionId)由宿主在创建时注入,不采集则缺省。

## SDK 侧接口语义

| 类别 | 方法语义 |
| --- | --- |
| Crash | `Report(错误/异常, 堆栈)`:上报一次错误;`Breadcrumb(文本)`:为下一次错误报告附加上下文(内存环形,上限 20 条,关闭即清空) |
| Trace | `Span(name, durationMs, path)`:上报一个已结束的 span;URL 只记 path 不记 query |
| Performance | `Startup(ms)` / `Jank(ms)` / `Rtt(ms)`:三个注册指标,见指标注册表 |
| Analytics | `Track(eventName, props)`:一次埋点;校验见数据范围 |

各方法均为非阻塞 fire-and-forget:签名不返回上报结果,失败不影响调用方。

## 上报 Schema 与数据范围(自有端点 wire)

统一:POST `application/json`,单条一请求,body 为单对象;响应 2xx = 受理(响应体丢弃),其余丢弃该条。字段缺省即不下发。

**Crash / Error**

```json
{ "sessionId": "...", "platform": "ios", "appVersion": "2.0.0",
  "engineVersion": "2022.3", "osVersion": "17.4", "deviceModel": "iPhone15,3",
  "message": "...", "stack": "...", "breadcrumbs": ["..."], "traceId": "..." }
```

| 采集 | 不采集 |
| --- | --- |
| sessionId、app/engine 版本、platform、os 版本、设备型号、堆栈、breadcrumb(接入方自行添加)、traceId | 位置、通讯录、相册、剪贴板、实名等敏感接口报文 |

**Trace**

```json
{ "resource": { "service.name": "courier.sdk", "courier.game_id": "game_demo",
    "courier.env": "prod", "platform": "ios", "app.version": "2.0.0",
    "engine.version": "2022.3" },
  "traceId": "...", "name": "support.ticket.create", "durationMs": 42, "path": "/v1/support/tickets" }
```

resource 属性为固定集;span 内不采集任何请求体;URL 只记 path,不记 query。

**Performance**

```json
{ "metric": "startup_duration_ms", "value": 1820,
  "dims": { "platform": "ios", "appVersion": "2.0.0" } }
```

指标名注册表:`startup_duration_ms` / `frame_jank_ms` / `network_rtt_ms`;值 + 维度(platform、app.version),无设备标识维度。

**Event Analytics**

```json
{ "eventName": "level_complete", "sessionId": "...", "level": 7, "durationS": 95.5 }
```

props 平铺进对象,仅原始类型(string/number/bool);`eventName` 1-64 字符非空;单条总大小 ≤ 1KB——违例是使用方错误,SDK 拒绝该条(抛参数错),不是静默截断。事件名与 props 结构由接入方登记,Courier 不做服务端字段解释。

## 分包与验收

- 归 **Diagnostics 可选包**;核心包/服务包/UI 包不依赖它、不携带任何诊断代码路径(含 no-op 路径)。
- 开启即意味着接入方接受该类别的数据范围表;运行时关闭开关立即停止采集并丢弃未发缓冲。
- 验收:四类默认零上报(一次发送都不发生);逐类开启逐类生效(只该类上报,其余仍零);关闭立即归零。

## v1 边界

- 直发不缓冲:崩溃现场重报(上次会话崩溃下次启动补发)归 Provider 生态(Sentry SDK 等自带),契约不内置。
- 采样/脱敏配置不进 v1;接入方在自托管端点侧处理。
