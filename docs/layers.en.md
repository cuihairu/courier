
# Five-Layer Architecture: L1–L5

> **Status**: Current — maintained alongside the implementation (2026-10-08 M0 constitution frozen; M1–M5 all landed). Principle: **no business stacking — skeleton first**. The master outline lives in [architecture.md](./architecture.en.md) (design principles / Provider principles / capability tree / lifecycle); the contract constitution lives in [contract/index.md](./contract/index.en.md); batch cadence lives in [todo.md](./todo.en.md).

## Layer Overview

```text
 Game clients: Unity / Unreal / Cocos / Godot / Mini Programs / Layabox
 ───────────────────────── Client side ─────────────────────────
 L1 SDK Contract      Language-agnostic contract: DTOs, error model, scope, events, lifecycle (single source of truth)
 L2 Core              Platform-agnostic kernel: state machine, token storage interface, retry, serialization
 L3 Platform Adapter  Platform bindings: native network stack, secure storage, lifecycle, packaging shape (+ optional UI packages)
 ───────────────────────── Wire ───────────────────────────
                      HTTPS JSON (SSE push channel M2+; the channel is a Provider capability)
 ───────────────────────── Server side ───────────────────────────
 L5 Gateway           Single entry point: auth / session / scope / routing / aggregation / middleware
 L4 Service Provider  Provider interface + default implementation (default vendor swappable / disableable)
 ───────────────────────── Ecosystem ────────────────────────────
   Default vendors (existing only in the Provider role): courier-account (account; second implementation courier-account-alt, swappable) herald (announcements; second implementation herald-alt, write-time index, swappable) croupier (support; second implementation croupier-alt, write-time owner index + keyset cursor, swappable) chirp (messaging; second implementation chirp-alt, ring log + cursor, swappable) warden (real name; second implementation warden-alt, managed verification, swappable) scribe (config/branding/version; second implementation scribe-alt, write-time dimension index, swappable) archivist (player profile; second implementation archivist-alt, event log, swappable) sage (assistant; second implementation sage-alt, write-time precomputed normalization, swappable) teller (payments; second implementation teller-alt, event ledger projection, swappable)
```

L1–L3 sit on the client side, L4–L5 on the server side; L1 is depended on by every layer and depends on no layer itself.

## L1 SDK Contract (contract layer)

**Responsibilities**

- Cross-platform shared types and behavior conventions: DTOs, unified error model, scope rules, event envelopes, SDK lifecycle state machine, and the interface shape of each capability.
- The primitives contract is already chartered: [contract/](./contract/index.en.md) (primitives / errors / scope / versioning / events); domain contracts freeze along with milestones.

**Boundaries**

- Contains no implementation or platform details; **aware of no vendor** — the contract describes capabilities, not implementers.
- The red lines for collection/compliance capabilities (diagnostics, real name) are built into the contract: off by default, explicit opt-in, and enabling must state the data scope.
- Changes follow the change process in [contract/index.md](./contract/index.en.md); once frozen, additive-only — breaking changes bump the version.

**Interface shape**

- Primitives: primitives / errors / scope / versioning / events; domains: auth (Frozen v1.1), announcement/support/messages (Frozen v1.1), realname (Frozen v1.1; the consent group frozen 2026-10-18), config/app/branding/player/diagnostics (Frozen v1 · M3), assistant (Frozen v1 · M4), payment (Frozen v1.1 · M5), oauth (Frozen v1, batch 52).

**Repo mapping**: `docs/contract/` (this repo) + `docs/contract/fixtures/` + `tools/contractgen` (contract test skeleton: fixtures generate contract sources for all six platforms; any drift turns tests red).

**Next steps**: the five primitives, auth, the three M2 domains, realname, the five M3 domains (config/app/branding/player/diagnostics), assistant (M4), and payment (M5) are all frozen; new domains freeze before work starts under new milestones.

## L2 Core (platform-agnostic kernel)

**Responsibilities**

- Unit-testable pure logic: DTO parsing and validation, **lifecycle state machine** (Uninitialized→…→PlayerReady, Suspended/Resuming), token storage interface, transport interface, retry and timeout, error normalization (error codes → per-platform enums).
- The `CourierClient` facade composes only the two entry points Identity / Session; the nine capability-tree domain entry points belong to the Service package's `CourierServices` facade (same shape on every platform).

**Boundaries**

- Imports no platform APIs (ITransport / ITokenStore / IClock injected); no UI; **no branding logic**; unaware of Provider implementations.

**Interface shape**

- `CourierClient.Init({ gameId, env, endpoint })` → per-domain submodules; domain interfaces correspond one-to-one with L1.

**Repo mapping**: `sdks/unity/packages/com.courier.core/Runtime` (Core/ state machine and API client, Identity/ Session/ auth domain, CourierClient.cs facade, Core/Contract/ generated artifacts); `CoreTests~` (65 Fact cases, xunit; runnable locally with dotnet test, also run by the engine Test Runner); `sdks/cocos src/core` (transport/apiClient/session/identity/sseStream/utf8, courierClient.ts facade) + `src/service` (nine-domain clients + SseStreamClient stream orchestration, `CourierServices` facade) + `src/ui` / `src/diagnostics` (optional packages), 200 node:test cases (contract 6 / core 35 / coreTransport 13 / lifecycle 35 / utf8 9 / service 18 / service2 16 incl. consent v1.1 / stream 20 / wechat 9 / wechatStream 8 / ui 17 / diagnostics 14); `sdks/ue/core` (pure ISO C++17 header-only: json parser, transport/apiClient/session/identity/lifecycle/courierClient/sse_stream) + `sdks/ue/service` (SSE parser + stream orchestration + nine-domain clients, `run_tests.sh` seven suites independently compiled with g++, all green); `sdks/godot src/core` (lifecycle/transport/tokenStore/courierError/apiClient/identity/session/courierClient) + `src/service` (SSE parser + nine-domain clients + `CourierServices` facade, eight headless SceneTree suites all green); the remaining platforms are the same shape.

**Next steps**: land the messages SSE stream client with the same shape on all six platforms (unity/godot wired to real stream reads; fetch stream source for the three TS platforms + UE SseSource seam, batch 38; WeChat enableChunked stream source + hand-written incremental UTF-8 decoding, batch 39; laya XHR stream source, batch 40); the one platform-specific stream source still outstanding is the UE SseSource engine wiring, which waits on the engine environment; real-device integration waits on the deployment side.

## L3 Platform Adapter (platform binding layer)

**Responsibilities**

- The adapter translates four things per platform: networking (native HTTP / SSE streaming reads), storage (ITokenStore secure-storage implementations), lifecycle (foreground/background and process signals → state machine events), threading (main-thread callback scheduling); plus packaging shape (UPM / npm landed; UE pluginization and Godot addon packaging wait on the engine integration side — in this repo UE is an engine-agnostic C++ header tree and Godot is in project form).
- **Optional UI packages hang on the platform side**: AnnouncementPanel / CustomerServicePanel / AssistantPanel / RealNamePanel, consuming the branding delivered from the server (see L2 boundaries: Core has no branding logic).

**Boundaries**

- **No business logic allowed**: the Adapter carries no business semantics; business logic either moves up into Core/Service or belongs in a UI package.
- One adapter per platform; platform-specific extensions build on Core interfaces as their base; cross-platform shared logic moves up.

**Interface shape**

- ITokenStore: Unity encrypted storage / WeChat storage / Layabox LocalStorage / Godot user:// persistent files have landed; UE platform credentials and Cocos localStorage are not yet in the repo (in-repo there is only MemoryTokenStore; platform persistence is provided by the integrator/engine side); ITransport: each platform's network stack.

**Repo mapping**: Unity — `sdks/unity/packages/com.courier.core/Runtime/Adapter` (UnityWebRequest transport / SecureTokenStore encrypted storage / ApplicationLifecycleMonitor foreground-background + the engine-agnostic LifecycleSignalRelay signal normalization (split out in batch 81; dedup / paired resume / rejection keeps pending / unbound no-op, all pure-dotnet testable) / UnityWebRequestSseSource messages SSE stream reads, landed in batch 37), `tools/packcheck` structure acceptance, four UPM packages (core/service/ui/diagnostics) in place; the three TS platforms' adapters — `platform/wechat` for cocos and miniprogram, `platform/laya` for laybox (wx.request / wx storage / LayaAir visibility and onLine signals; WeChat-side sseSource.ts — wx.request enableChunked chunked stream reads + incremental UTF-8 decoding, landed in batch 39; laybox-side sseSource.ts — XHR stream reads with onprogress increments + clean close-and-reconnect on onload, landed in batch 40); UE C++ header-only landed in batch 14 (engine integration via HttpModule waits on the engine environment); Godot `src/platform` (httpTransport — real HTTPClient poll transport, fileTokenStore — user:// persistent token storage, sseStreamTransport — messages SSE stream reads with chunked framing + backoff reconnect, lifecycleMonitor — foreground/background signals → state machine events, landed in batches 34/35/36, eight headless suites all green).

**Next steps**: UE engine integration and real-device joint debugging wait on the deployment side; Godot TLS-path real-device verification is pending wiring.

## L4 Service Provider (Provider interface + default implementation, server side)

**Responsibilities**

- Defines the Provider interface for each capability and ships a default implementation; the data facts stay on the implementation side — Courier only does projection and orchestration.

| Provider | Interface responsibility | Default implementation (swappable / disableable) |
| --- | --- | --- |
| AccountProvider | Registration / login / credential verification | courier-account (in-house; second implementation courier-account-alt, same contract, swappable) |
| AnnouncementProvider | Player-facing announcements projection | herald (read-time scan; second implementation herald-alt with write-time indexing, swappable) |
| SupportProvider | Ticket / FAQ projection | croupier (read-time scan; second implementation croupier-alt with write-time owner index + keyset cursor, swappable) |
| MessageProvider | Push channel / session reuse | chirp (fan-out; second implementation chirp-alt with ring log + cursor, swappable) |
| RealNameProvider | Real-name verification (hot-swap / degradation chain / circuit breaker) | warden (in-house verification; second implementation warden-alt with managed verification, verdicts via an injected CloudVerifier; Alibaba Cloud / Tencent Cloud Huiyan / Yidun / Webhook all attach with the same shape) |
| ConfigProvider | Remote config delivery | scribe (conditional projection + gray-release bucketing; config/branding/app are all covered by the single scribe.AppProvider interface; second implementation scribe-alt with write-time dimension indexing, same contract, swappable) |
| BrandingProvider | Branding asset delivery | scribe (same pipeline as config; second implementation scribe-alt, same pipeline, swappable) |
| PlayerProvider | Account profile + account↔character mapping | archivist (scope isolation; second implementation archivist-alt with event log, swappable) |
| AssistantProvider | FAQ retrieval Q&A (hits return verbatim snapshots, nothing generated) | sage (in-house; second implementation sage-alt with write-time precomputed normalization, swappable; handoff to human agents reuses the support domain; LLM reserved, not a dependency by default) |
| PaymentsProvider | Order-callback payments (server-side pricing; the channel signature is the authentication) | teller (sandbox channels; real channels attach via the same callback signature surface; second implementation teller-alt with event ledger projection, same contract, swappable) |
| DiagnosticsProvider | Diagnostics reporting | Off by default; the client-side optional package ships directly to self-hosted endpoints (Sentry/GlitchTip/OTLP ecosystem), bypassing the gateway |

**Boundaries**

- No entry governance (authentication / rate limiting belong to L5); no UI; an unconfigured Provider = capability degraded (`COMMON_CAPABILITY_DISABLED`), raising no error and dragging down no other capability.
- Governance rules: config-driven routing table (`primary`+`fallbacks[]`), degradation chain, HealthCheck common to all Providers; whole-table hot reload across all domains landed via `routing.Hot` (SIGHUP re-reads COURIER_GATEWAY_CONFIG, batch 99 / F78 phase 1); in-domain fine-grained circuit breaking remains realname-specific (`providers/router`), the other domains are reserved seams (see "Provider governance" in architecture.md).

**Interface shape**

- One Provider interface per domain; after implementing `providers.Handler`, registration goes through the assembler's `reg.Register(h)` (interface and implementation are separate; the default implementation can be replaced wholesale).

**Repo mapping**: `gateway/providers/` (interface shape frozen in batch 2; `account/` AccountProvider batch 3; `herald/` `croupier/` `chirp/` default implementations for the three domains batch 6, with the Notify hook decoupled at assembly — publish/reply events enter the chirp Hub through the injected `Publish`, Providers never import each other; `router/` governance engine + `warden/` in-house real-name verification batch 7 — the first complete landing of degradation chain / circuit breaker / hot-reload, reused by later domains; batch 102 adds the consent group v1.1 (six-state machine / anti-rollback / VPC TTL idempotency / S2S verdict surface / main.go env assembly); `scribe/` config/app/branding on one pipeline + `archivist/` player profile batch 8 — M3 hot-apply / maintenance gate / branding hot-swap e2e acceptance; `sage/` assistant batch 9 — M4 retrieval Q&A / hit-rate stats / handoff-to-human e2e acceptance; `teller/` payments batch 10 — M5 server-side pricing / callback HMAC signature surface / broken-order reconciliation e2e acceptance; `accountalt/` second identity implementation (heterogeneous under the same contract, HMAC self-verifying tokens) batch 18 — reads COURIER_GATEWAY_CONFIG at assembly time and re-points with the identity primary; `wardenalt/` second realname implementation (managed verification, verdicts via an injected CloudVerifier, the gateway side stores only the verdict state) batch 64 (the consent group landed same-shape in batch 102) — same-wire scenario consistency e2e (same shape as the batch 18 precedent; consent scenarios batch 102); `archivistalt/` second player implementation (event log model: changes append events, reads = projection replay) batch 67 — same-wire scenario consistency e2e (same shape as the batch 18/64 precedents); `chirpalt/` second messages implementation (shared ring log + per-connection cursor pulls, each publish appended exactly once) batch 68 — same-wire scenario consistency e2e (same shape as the batch 18/64/67 precedents); `heraldalt/` second announcements implementation (write-time indexing: O(1) map detail lookups + a publishedAt ascending slice maintained at publish time) batch 69 — same-wire scenario consistency e2e (same shape as the batch 18/64/67/68 precedents); `sagealt/` second assistant implementation (write-time precomputed normalized text: normalized once at AddEntry, zero recomputation at query time) batch 70 — same-wire scenario consistency e2e (same shape as the batch 18/64/67/68/69 precedents); `scribealt/` second app implementation (write-time dimension indexing: at Publish, conditional slices become dimension→allowed-value indexes, projection filters in stages by the requested dimensions) batch 71 — same-wire scenario consistency e2e (same shape as the batch 18/64/67/68/69/70 precedents); maintenance-gate callbacks follow the app-domain primary re-pointing (same precedent as the push source); `telleralt/` second payments implementation (append-only event ledger: state transitions append immutable events, status/timestamps = projection replay, history immutable) batch 72 — same-wire scenario consistency e2e (same shape as the batch 18/64/67/68/69/70/71 precedents); `croupieralt/` second support implementation (write-time owner-ordered index: ticket creation / append / reply / close / attachment registration re-sort immediately, cursor = `<milliseconds>|<ticketID>` keyset anchor, zero scans and zero sorting at read time, no drift under concurrent inserts) batch 73 — same-wire scenario consistency e2e + explicit proof of cross-implementation pagination equivalence (same shape as the batch 18/64/67/68/69/70/71/72 precedents)).

**Next steps**: Diagnostics is a client-side optional package (bypassing the gateway; batch 8 landed it at `sdks/unity/packages/com.courier.diagnostics`); payments landed in batch 10 (teller).

## L5 Gateway (entry governance layer, server side)

**Responsibilities**

- Single entry point: auth / session / scope / routing / aggregation / middleware (rate limiting, trace, recovery, scope, maintenance gate, 1MiB request-body cap); auditing relies on trace running end to end — the real-name domain has landed a structured audit event surface (`gateway/audit` shared seam: both realname implementations inject the same `AuditSink`, nil = emits nothing; `COURIER_REALNAME_AUDIT=log` turns on the logging half), with persistence waiting on the deployment side.
- Provider registry management: routes for all known capability domains are mounted in full; routes for unconfigured capabilities return `COMMON_CAPABILITY_DISABLED` (501) at dispatch time — not a missing route.

**Boundaries**

- No business directories (names like `accounts/`, `announcements/` are not allowed at the gateway's first level); middleware handles only cross-cutting concerns and knows no business fields; no admin interface.

**Interface shape**

```text
gateway/
├── cmd/gateway/
├── auth/  ├── session/  ├── scope/
├── routing/  ├── aggregation/  ├── middleware/
└── providers/
```

Go `http.ServeMux` + middleware chain (trace → structured errors → rate limiting → scope, frozen order; per capability, authentication mounts Bearer validation inside the route); `/healthz`; `/v1/{domain}/*`.

**Repo mapping**: `gateway/{cmd,routing,middleware,scope,aggregation}` (batch 2 skeleton) + `auth`/`session` (implemented in batch 3, Bearer → identity injection / session validation) + `e2e` (batch 6, full M2 chain; batch 8 adds M3 hot-apply / maintenance / branding, batch 9 adds M4 retrieval Q&A / handoff-to-human, batch 10 adds M5 order placement / callback / reconciliation e2e); capability-driven routes `/v1/{capability}/` mount automatically as Providers register, unconfigured = 501 degradation; the maintenance gate `middleware.Maintenance` wraps the entry point (batch 8, app.md whitelist built in: when maintenance is on, everything under `/v1/*` returns 503 except `/v1/app/*` and `/healthz`); degradation semantics and the middleware chain are covered by tests.

**Next steps**: persistent storage and multi-instance rate-limit coordination.

## Dependency Rules

1. **Dependencies point downward only**: L3 → L2 → L1; L5 → L4 → L1; reverse dependencies are forbidden.
2. **The contract is vendor-agnostic**: L1 offers capabilities without naming implementers; default vendors appear only in L4's implementations and configuration.
3. **The kernel never touches the platform**: L2 programs against injected interfaces; L4 obtains identity only through L5 middleware.
4. **Contract first**: every public type in L2–L5 must have its definition findable in L1; no platform may invent fields of its own.
5. **Each difference stays in its place**: platform differences live only in L3; vendor differences only in L4; L1/L2 are identical across platforms.
6. **Red lines are built in**: off by default and explicit data scope on enable are L1 contract properties that L3–L5 must not bypass.
7. **UI is optional**: the UI and Diagnostics packages stay out of the Core package; Core has no UI and no branding logic.

## Mapping to the Current Repo Structure

| Layer | Location in repo | Status |
| --- | --- | --- |
| L1 SDK Contract | `docs/contract/` + `tools/contractgen` | five primitives Frozen v1 + auth v1.1 + three M2 domains v1.1 + realname v1.1 (consent group landed batch 102) + five M3 domains (config/app/branding/player/diagnostics) v1 + assistant (M4) v1 + payment (M5) v1.1 + oauth v1 (batch 52); error codes v8 |
| L2 Core | `sdks/unity/packages/com.courier.core/Runtime/{Core,Identity,Session}`;`sdks/cocos/src/core`;`sdks/ue/core` | Unity landed in batch 4 (state machine + auth domain, 48 cases; batch 37 SseStream seam + backoff); cocos TS landed in batches 12-13 (core + service, all domains); batch 38 landed the messages stream client (111 node:test cases across the three TS platforms, e2e against a real chunked server); batch 39 landed the incremental UTF-8 decoder (multi-byte sequences never torn across chunks, 123 node:test cases); UE C++ landed in batch 14 (core + service, all domains, pure ISO C++17 header-only); batch 38 landed stream orchestration (seven g++ suites all green) |
| L3 Platform Adapter | `sdks/unity/packages/com.courier.core/Runtime/Adapter` (+UPM packages; `tools/packcheck`); the three TS platforms' adapters (`platform/wechat` in `sdks/cocos` and `sdks/miniprogram`, `platform/laya` in `sdks/laybox`) | Unity landed in batch 5 (transport / secure storage / foreground-background); batch 6 service domain services + SSE parsing, UI panels; batch 8 App/Config/Branding/Player domain services + the diagnostics optional package (all off by default, no-op); batch 9 Assistant domain service + assistant panel (handoff to human); batch 10 Payment domain service (order placement / polling); batch 37 SSE stream-read wiring (`UnityWebRequestSseSource`, with backoff / reconnect / parsing orchestration as pure logic in Core+Service, covered by 77 dotnet [Fact] cases — only 16 of them SSE-specific; actual runs in the engine Test Runner wait on the deployment side); the three TS platforms landed in batches 12-17 (wechat/laya bindings; ui package batch 17, diagnostics package batch 20); batch 38 landed FetchSseSource for the three TS platforms (default fetch stream-read implementation) + the UE SseSource seam; batch 39 landed the WeChat enableChunked stream source (cocos/miniprogram, headers-then-backfill connection + hand-written UTF-8 decoding); batch 40 landed the laya XHR stream source (onprogress increments + clean close-and-reconnect on onload; UE engine wiring waits on the engine environment); the ui package (batch 17) and diagnostics package (batch 20) landed (cocos first, batch 41 aligned all three platforms — 123 node:test cases each on the TS platforms); Godot landed in batches 34/35/36 (HTTPClient transport + user:// storage + messages SSE stream reads + foreground/background signals, verified over real sockets) |
| L4 Service Provider | `gateway/providers/*` | interface shape frozen in batch 2; account batch 3; herald/croupier/chirp batch 6 (three M2 domains, e2e acceptance); router governance engine + warden real name batch 7 (consent group v1.1 batch 102); scribe + archivist batch 8 (M3, e2e acceptance); sage batch 9 (M4, e2e acceptance); teller batch 10 (M5, e2e acceptance); accountalt second identity implementation (heterogeneous under the same contract) batch 18; wardenalt second realname implementation (managed verification) batch 64 (consent same shape batch 102); archivistalt second player implementation (event log) batch 67; chirpalt second messages implementation (ring log + cursor) batch 68; heraldalt second announcements implementation (write-time indexing) batch 69; sagealt second assistant implementation (write-time precomputed normalization) batch 70; scribealt second app implementation (write-time dimension indexing) batch 71; telleralt second payments implementation (event ledger projection) batch 72; croupieralt second support implementation (write-time owner index + keyset cursor) batch 73 |
| L5 Gateway | `gateway/{cmd,routing,middleware,scope,aggregation,auth,session,e2e}` | batch 2 skeleton; auth/session batch 3; capability-driven routing + SSE channel + e2e batch 6; maintenance gate batch 8 |

## Evolution Principles

- Each layer first stands up "interface shape + one minimal implementation" before spreading horizontally; no half-features.
- For batch cadence and acceptance see [todo](./todo.en.md); for business milestones see [roadmap](./roadmap.en.md).
