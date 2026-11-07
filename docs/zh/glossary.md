# Courier 源码术语表

> 范围：本表只记录当前源码中真实存在的抽象，不按目录名或设想命名。配套分层叙述见[架构](./architecture.md#源码分层)；英文镜像为 [glossary](../glossary.md)。

| 术语 | 英文 | 定义 | 证据 |
| --- | --- | --- | --- |
| 能力域 | Capability | 路由前缀与路由表键（`/v1/{capability}/`）；它是刻意收窄的服务边界，不得使用后端实体名。 | gateway/providers/capability.go:3 |
| 供应商处理器 | Provider handler | 一个能力域下唯一命名的 HTTP 处理器；默认实现、自建实现和第三方实现经同一入口注册。 | gateway/providers/registry.go:29 |
| 供应商链 | Provider chain | 配置出的 `primary` 加保序 `fallbacks`；未注册名字被跳过，解析结果为空表示能力降级。 | gateway/providers/registry.go:72 |
| 入口治理链 | Entry governance chain | 冻结的横切顺序：trace、panic 结构化错误、限流、scope 校验，包装全部能力路由。 | gateway/middleware/chain.go:32 |
| 作用域 | Scope | 仅经 header 传输的 `game_id + env` 隔离对；网关校验它，签发会话绑定它。 | gateway/scope/scope.go:21 |
| 会话视图 | Session view | 供令牌验证使用的只读会话投影，包含账号、设备、scope、过期与吊销状态。 | gateway/session/session.go:15 |
| 可切换验证器 | Switchable verifier | 共享的运行期重定向接缝；身份源 primary 变化时，依赖域无需重建 Provider 即可跟随。 | gateway/auth/switchable.go:17 |
| 热路由 | Hot router | 原子替换根处理器：在途请求按旧路由表完成，新请求使用重载后的配置。 | gateway/routing/hot.go:11 |
| 响应信封 | Response envelope | 线上响应规则：成功只有 `data`，失败只有 `error` 加 `traceId`，两种形态互斥。 | gateway/aggregation/envelope.go:1 |
| 冻结错误码 | Frozen error code | 契约拥有且冻结 HTTP 状态与可重试性的错误码；未登记码保守按可重试 500。 | gateway/aggregation/envelope.go:71 |
| 生命周期状态机 | Lifecycle state machine | 平台无关的 SDK 转移表，覆盖初始化、认证、PlayerReady、挂起、恢复与登出。 | sdks/cocos/src/core/lifecycle.ts:85 |
| 传输接口 | Transport | SDK 执行 HTTP 请求的接缝；Core 保持平台无关，各端提供 fetch、引擎或小程序绑定。 | sdks/cocos/src/core/transport.ts:19 |
| 令牌存储 | Token store | 持久凭证的 load/save/clear 接缝；Core 默认内存实现，适配层提供引擎或平台安全存储。 | sdks/cocos/src/core/tokenStore.ts:5 |
| 事件提示 | Event hint | SSE 推送只是提示，列表/拉取端点才是权威；事件缺失、丢弃或缓冲满都由拉取补偿。 | docs/contract/events.md:23 |
| 域实现配对 | Domain implementation pair | 同一 wire 契约下的默认 Provider 与异构第二实现，由路由配置选择。 | gateway/cmd/gateway/main.go:62 |
| 运行期接缝 | Runtime follow target | 原子化的验证器、事件发布器或维护门；启动和重载时必须随对应域 primary 一起重指。 | gateway/cmd/gateway/reload.go:36 |

