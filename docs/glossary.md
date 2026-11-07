# Courier Source Glossary

> Scope: this table records the abstractions that the current source actually exposes, not aspirational module names. The companion layer narrative is [architecture](./architecture.md#source-layers); the Chinese mirror is [glossary.zh](./zh/glossary.md).

| Term | English | Definition | Evidence |
| --- | --- | --- | --- |
| 能力域 | Capability | A route prefix and routing-table key (`/v1/{capability}/`); an intentionally narrow service boundary, never a backend entity name. | gateway/providers/capability.go:3 |
| 供应商处理器 | Provider handler | A uniquely named HTTP handler for one capability, registered through the same gateway seam as default, self-hosted, and third-party implementations. | gateway/providers/registry.go:29 |
| 供应商链 | Provider chain | The configured `primary` plus ordered `fallbacks`; unknown names are skipped, and an empty resolved chain means the capability is degraded. | gateway/providers/registry.go:72 |
| 入口治理链 | Entry governance chain | The frozen cross-cutting order trace, structured panic errors, rate limiting, and scope validation, wrapped around every capability route. | gateway/middleware/chain.go:32 |
| 作用域 | Scope | The `game_id + env` isolation pair sent only in headers, checked by the gateway, and bound to issued sessions. | gateway/scope/scope.go:21 |
| 会话视图 | Session view | A read-only session projection carrying account, device, scope, expiry, and revocation state for token verification. | gateway/session/session.go:15 |
| 可切换验证器 | Switchable verifier | The shared runtime indirection that lets dependent domains follow an identity-provider primary change without rebuilding providers. | gateway/auth/switchable.go:17 |
| 热路由 | Hot router | An atomic root-handler replacement: in-flight requests finish on the old route table while new requests use the reloaded configuration. | gateway/routing/hot.go:11 |
| 响应信封 | Response envelope | The wire rule that success has only `data` and failure has only `error` plus `traceId`; these branches are mutually exclusive. | gateway/aggregation/envelope.go:1 |
| 冻结错误码 | Frozen error code | A contract-owned code with a frozen HTTP status and retryability mapping; unregistered codes fail conservatively as retryable 500. | gateway/aggregation/envelope.go:71 |
| 生命周期状态机 | Lifecycle state machine | The platform-neutral SDK transition table from initialization through authentication, player readiness, suspension, resume, and sign-out. | sdks/cocos/src/core/lifecycle.ts:85 |
| 传输接口 | Transport | The SDK seam that executes an HTTP request so Core can remain platform-neutral while platforms provide fetch, engine, or mini-program bindings. | sdks/cocos/src/core/transport.ts:19 |
| 令牌存储 | Token store | A load/save/clear seam for persisted credentials; Core defaults to memory, and adapters provide engine- or platform-specific secure storage. | sdks/cocos/src/core/tokenStore.ts:5 |
| 事件提示 | Event hint | An SSE push whose list/fetch endpoint remains authoritative; a missing, dropped, or full-buffer event is compensated by pulling. | docs/contract/events.md:23 |
| 域实现配对 | Domain implementation pair | A default Provider and a heterogeneous second implementation exposed under the same wire contract and selected by routing configuration. | gateway/cmd/gateway/main.go:62 |
| 运行期接缝 | Runtime follow target | An atomic verifier, event publisher, or maintenance gate that must be redirected together with its domain's primary during startup and reload. | gateway/cmd/gateway/reload.go:36 |

