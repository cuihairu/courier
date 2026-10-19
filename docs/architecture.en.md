
# Courier Architecture

> **Status**: Current — maintained alongside the implementation (2026-10-08 M0 constitutional freeze; M1–M5 have all landed). Layer-by-layer detail (SDK Contract / Core / Platform Adapter / Service Provider / Gateway) is in [layers.md](./layers.en.md), batch-by-batch delivery is in [todo.md](./todo.en.md), the contract constitution is in [contract/](./contract/index.en.md), and competitive positioning is in [research/competitive.md](./research/competitive.en.md).

## Design Principle: SDK Contract > Everything

Courier's core is the contract, not any particular backend:

```text
                 Courier
                    │
          ┌─────────▼─────────┐
          │   SDK Contract    │
          │  Auth / Session   │
          │  Player / App     │
          │  Announcement     │
          │  Support / Pay    │
          │  Message / …      │
          └─────────┬─────────┘
           ┌────────┼────────┐
           ▼        ▼        ▼
         Unity      UE      Cocos …
```

Gateway, Providers, and ecosystem backends are all **implementers of the contract / backend providers**. A new engine = a new Adapter; a new service = a new Provider; the contract itself stays stable.

**The delivery metaphor**: the name Courier (the delivery person) is itself the product shape — capabilities are split into small standalone "packages", each running independently, replaceable, refusable; the contract is the unified packaging spec, and the granularity rule for a package is "a size that works" — small enough to swap, runnable before delivery; the sorting system (Gateway) doesn't care what business is inside a package, it only reads the waybill (the interface boundary).

## Provider Principle: Provided by Default, Swappable and Disableable

Ecosystem projects can only exist in the role of providers (Provider):

1. **Every capability's backend = a pluggable Provider**, uniform interface, configuration-driven.
2. **Courier ships a set of out-of-the-box implementations by default**: announcements default to herald, support to croupier, push to chirp, real name to warden (self-hosted verification), account to the self-built courier-account (second implementation courier-account-alt, swappable under the same contract) — the default providers can be bundled, but **whether to connect them, which one to use, and whether to swap is decided by the integrator's configuration; the SDK does not choose on the user's behalf**.
3. **No hard binding, no forced dependencies**: any Provider can be replaced with a self-built implementation or a third-party service, or simply turned off; turned off = capability degradation (`COMMON_CAPABILITY_DISABLED`), not an error.
4. **Example**: the announcements capability defaults to the herald Provider; the integrator can configure it to point at their own HTTP endpoint, or disable announcements (the client safely hides the corresponding UI).
5. **Uniform interfaces ≠ data migration**: the Provider mechanism promises "uniform interfaces" and "replaceable implementations"; data migratability (moving account/order/profile history data to a new backend) **is not a default promise** and needs per-domain evaluation — before switching Providers, first answer whether user IDs, session validity, and in-flight orders are compatible.
6. **Two integration modes**: server-side Providers are the default (the client only ever sees the gateway; credentials and routing stay entirely server-side; sensitive capabilities such as account/payments/real name must go this way); direct client connections to external services are limited to public services explicitly allowed to be direct-connected, must not become the default switching mechanism, and server-side secrets never reach the client.
7. **At least two implementations per module**: a default implementation + a replaceable implementation (a real second implementation or a test double), selected by the integrator via configuration. **For a capability with only one implementation, "swappable" exists in name only** — as of today the account domain has delivered (courier-account + courier-account-alt, heterogeneous implementations of the same frozen contract + same-scenario e2e behavioral proof, batch 18), the real name domain has delivered (warden self-hosted verification + warden-alt hosted verification, heterogeneous verdict sources/persisted content + same-scenario e2e behavioral proof, batch 64), the player profile domain has delivered (archivist state table + archivist-alt event log, heterogeneous storage models + same-scenario e2e behavioral proof, batch 67), the push domain has delivered (chirp fan-out push + chirp-alt shared ring log/cursor pull, heterogeneous hub models + same-scenario e2e behavioral proof, batch 68), the announcements domain has delivered (herald read-time scan-and-sort + herald-alt write-time indexing, heterogeneous index models + same-scenario e2e behavioral proof, batch 69), the assistant domain has delivered (sage read-time normalization + sage-alt write-time precomputed normalization, heterogeneous preprocessing models + same-scenario e2e behavioral proof, batch 70), the config domain has delivered (scribe read-time per-entry condition AND + scribe-alt write-time dimension index/staged filtering, heterogeneous evaluation models + same-scenario e2e behavioral proof, batch 71), the payments domain has delivered (teller mutable state table + teller-alt additive-only event ledger/projection replay, heterogeneous state-representation models + same-scenario e2e behavioral proof, batch 72), and the support domain has delivered (croupier read-time scan-and-sort/offset cursor + croupier-alt write-time owner-ordered index/keyset cursor, heterogeneous list-organization models + same-scenario e2e behavioral proof, batch 73) — all nine capability domains have two implementations; stage C's "at least two implementations per domain" is closed out.

## Overall Shape

```text
┌───────────────────────────────────────────────┐
│ Game Client                                   │
│   Courier SDK: Core / Service / UI (optional) │
└──────────────┬────────────────────────────────┘
               │ HTTPS JSON (+ SSE push channel, M2)
┌──────────────▼────────────────────────────────┐
│ Courier Gateway(Go):auth · session · scope    │
│   · routing · aggregation · middleware        │
│   · providers(Provider registry & governance) │
└──────┬─────────┬─────────┬─────────┬──────────┘
       ▼         ▼         ▼         ▼
  AccountProvider (self-hosted accounts) … all via the Provider interface
       ▼
  Default providers: herald (announcements) · croupier (support) · chirp (push) · warden (real name) · scribe (app) · archivist (player profile) · sage (assistant) · teller (payments)
  — all of the above can be replaced or disabled by the integrator
```

## Capability Tree

```text
Courier
├── Core          Lifecycle / Network / Error / Retry / Cache / Telemetry hooks
├── Identity      Login / Logout / Bind / Device / OAuth (optional, third-party) / RealName (optional, compliance)
├── Session       AccessToken / RefreshToken / Session
├── Player        Profile / Game Player
├── App           Version / Maintenance / Remote Config / Branding
├── Communication Announcement / Message / Push
├── Support       FAQ / Ticket (core); Panel (optional UI package)
├── Assistant     FAQ retrieval Q&A (optional, LLM provider swappable)
├── Payment       Order / Purchase / Receipt (contract + built-in sandbox channel, four shapes optional)
├── Diagnostics   Crash / OTel Trace / Perf / Analytics (optional package)
└── Platform      Unity / Unreal / Cocos / Godot / Layabox / MiniProgram
```

## SDK Lifecycle State Machine

The biggest difference between a game SDK and an ordinary REST SDK: lifecycle is a first-class citizen. The state machine is defined in the L1 contract (event surface in [contract/events.md](./contract/events.en.md)) and implemented in L2 Core:

```text
Uninitialized → Initializing → Ready → Authenticating → Authenticated → PlayerReady
                                   ▲                        │              │
                                   │        ┌───────────────┘              │
                                   │        ▼                              ▼
                                   └── SignedOut                Suspended ⇄ Resuming
                                                              (backgrounded / offline → resume)
```

Handled uniformly: game launch, backgrounding, resumption, token expiry (automatic refresh), network disconnection and recovery, account switching, and sign-out. Platform differences (foreground/background signals, process liveness) are injected by Adapters; the state machine itself is platform-agnostic.

## SDK Packaging: Core / Service / UI

| Package | Contents | Required? |
| --- | --- | --- |
| Core package | Kernel + Identity / Session (initialization, lifecycle, tokens) | Required |
| Service package | The service surface of each capability: `Support.CreateTicket()`, `Communication.Announcements.list()`, etc. — **the interfaces live in the core/service packages** | As needed |
| UI package | Components such as `AnnouncementPanel` / `CustomerServicePanel` / `AssistantPanel` / `RealNamePanel`, **consuming Branding config** | Optional |

- `Support.CreateTicket()` belongs to the core/service packages; `CustomerServicePanel` belongs to the optional UI package — the SDK forces no UI on anyone.
- Each engine takes what it needs: Unity has a UPM UI package, UE goes through UMG (the UI layer belongs to the engine-integration surface, not in this repo), Cocos draws its own; a missing UI package does not affect the functional surface.
- Diagnostics is likewise an optional package, on the same level as the UI package.

## Gateway: Entry Governance, No Business Logic

The Gateway only does entry governance; business always arrives via Provider interfaces, and **no business directories are kept**:

```text
gateway/
├── cmd/gateway/   entry point
├── auth/          auth middleware (token verification)
├── session/       session management
├── scope/         game_id + env injection and validation
├── routing/       routing and the Provider registry
├── aggregation/   cross-Provider aggregation
├── audit/         realname audit event seam (AuditSink / LogSink / HTTPSink)
├── middleware/    rate limiting / trace / recovery / scope / maintenance gate / 1MiB body cap (outermost)
└── providers/     Provider interface + default implementations (capability domain → registered name)
    ├── account/        AccountProvider(identity)   default: courier-account (self-hosted
    │                   accounts/sessions)
    ├── accountalt/    AccountProvider(identity second implementation, same contract heterogeneous, swappable)
    ├── herald/         AnnouncementProvider(announcements)
    ├── heraldalt/      AnnouncementProvider(announcements second implementation, write-time indexing, same contract swappable)
    ├── croupier/       SupportProvider(support)
    ├── croupieralt/    SupportProvider(support second implementation, write-time owner index + keyset cursor, same contract swappable)
    ├── chirp/          MessageProvider(messages, SSE)
    ├── chirpalt/       MessageProvider(messages second implementation, ring log + cursor, same contract swappable)
    ├── warden/         RealNameProvider(realname)  default: self-hosted verification; Alibaba Cloud/Tencent Cloud
    │                   Huiyan/Yidun/Webhook adapted to the same shape as needed
    ├── wardenalt/      RealNameProvider(realname second implementation, hosted verification, same contract swappable)
    ├── scribe/         AppProvider(app: config/branding/version/maintenance, self-hosted pipeline,
    │                   publishable via croupier)
    ├── scribealt/      AppProvider(app second implementation, write-time dimension index, same contract swappable)
    ├── archivist/      PlayerProvider(player)
    ├── archivistalt/   PlayerProvider(player second implementation, event-log model, same contract swappable)
    ├── sage/           AssistantProvider(assistant, FAQ retrieval Q&A)
    ├── sagealt/       AssistantProvider(assistant second implementation, write-time precomputed normalization, same contract swappable)
    ├── teller/         PaymentsProvider(payments: order-callback-delivery; channel sandbox follows the deployment surface)
    └── telleralt/      PaymentsProvider(payments second implementation, event-ledger projection, same contract swappable)
```

> Diagnostics bypasses the gateway: the contract red line (diagnostics.md) requires diagnostics data to be sent directly to the integrator's own endpoints; the gateway has no DiagnosticsProvider.

- Unconfigured Providers: routes are still registered (all `/v1/<capability>/` prefixes mount); a request returns a 501 degradation envelope with `COMMON_CAPABILITY_DISABLED` (see [contract/errors.md](./contract/errors.en.md)) — clients probe capability switches this way instead of getting a 404.
- Names like `accounts` / `announcements` must never appear as first-class business directories in the gateway — they are the default implementations of Providers and live under `providers/`.

## Provider Governance: Hot-Swap / Degradation Chain / Circuit Breaker

- **Configuration-driven**: each Provider is declared by configuration as `primary` + `fallbacks[]`; a runtime config reload takes effect without a restart (the routing table is swapped atomically; in-flight requests complete against the old table). Current state: whole-table hot reload across all domains landed via `routing.Hot` (batch 99, F78 phase 1) — `SIGHUP` re-reads `COURIER_GATEWAY_CONFIG`, and a failure keeps the old table (fail-safe); the primary-following seams (identity verifier / push source / maintenance gate) re-point in both directions on reload; the realname domain additionally has fine-grained in-domain governance at `providers/router.Reload` (degradation chain / circuit breaker, batch 7). The admin-plane reload endpoint is not implemented (phase 2, prerequisite = a management-plane auth contract).
- **Degradation chain**: primary Provider failure/timeout → fallbacks in order; if all fail, the domain-defined safe state applies (e.g. RealName → `PENDING_REVIEW`).
- **Circuit breaker** (already landed in the real name domain): driven by call counts — N consecutive failures open the circuit; once open, 30 calls act as half-open probes to recover (no timers); during the open period traffic goes straight to fallback.
- **HealthCheck**: per-request probe; on failure the chain falls through, and a fully unhealthy chain returns 503 — the registry does no instance eviction (reserved seam).
- RealName ([contract/realname.md](./contract/realname.en.md)) is the first domain to apply all of this in full; the rules are universal for all Providers.

### Independent Switching Strategy per Sensitive Domain (Account / Real Name / Payments)

Replaceable interfaces ≠ migratable data; each of the three sensitive domains defines its own switching surface and what does not migrate:

| Domain | Switching surface (switch by config alone) | Not migrated / conservative surface | Evidence |
| --- | --- | --- | --- |
| Account | Verifier and primary store share one source: switching the identity primary in the routing table to the second implementation re-points atomically via `auth.Switchable` (fallbacks do not participate; the verifier does not switch per-request) | Token semantics do not cross implementations: account (opaque random tokens + session table) and accountalt (HMAC self-verifying tokens) do not recognize each other; after a switch old tokens fail verification → re-login | Both implementations speaking the same wire is proven by the conformance e2e (`e2e/identity_conformance_test.go`, batch 18); under concurrent source switching, in-flight verifications only ever return the old/new target identity, no intermediate state (`auth` package `-race` unit tests) |
| Real name | Off by default, enabled explicitly in the routing table; `router.Router` atomic routing-table swap + circuit breaker + all-chain-failure safe state (PENDING_REVIEW / not playable / not admitted) | Identity inputs live only on the call stack (no logs/traces/diagnostics; only fingerprints may be stored) — a switch must not be an occasion to persist plaintext | `router` package unit tests (degradation order / breaker half-open / safe state / in-flight requests on the old table) + `e2e/realname_test.go` (default 501 / fallback chain / hot reload with service continuity) |
| Payments | Channel switch = swap the callback signing-secret config (per-channel HMAC-SHA256) + the `Deliver` fulfillment hook; empty secret = callbacks always 403 | Order state and the ledger do not follow the channel (the ledger belongs to the deployment surface / integrator storage); callback idempotency and broken-order reconciliation semantics in the contract are unchanged | `payment.md` order-delivery core flow + callback idempotency red line; `teller.Options.ChannelSecret` / `Deliver` |

## New Capability Design

### Remote Config (M3)

- SDK: `Courier.Config.Get("login.enable_guest")`.
- Matching dimensions: `game_id / environment / platform / version / region` — configs match by condition.
- Chain: published by the integrator or a default provider (e.g. croupier) → ConfigProvider → Gateway cache → SDK (local cache + `config.updated` push for hot effect).

### Courier.App (M3)

- `CheckUpdate / CheckMaintenance / GetEnvironment` (Unity facade `CheckUpdateAsync` / `CheckMaintenanceAsync` / `GetEnvironmentAsync`, mapped to the `GET /v1/app/version|maintenance|environment` endpoints; no separate GetVersion facade).
- Typical startup sequence: Game Start → Initialize → CheckMaintenance → CheckVersion → Auth → Player → Game.

### Branding (M3)

Brand customization: company name/logo/icon/theme color/about page/support entry, delivered per `game_id+env`, consumed by the optional UI package; Core contains no branding logic. See [contract/branding.md](./contract/branding.en.md).

### RealName (late M2, compliance)

Interface slots for real name verification and anti-addiction hooks; hot-swappable Providers (self-hosted / Alibaba Cloud / Tencent Cloud Huiyan / Yidun / Webhook), off by default. See [contract/realname.md](./contract/realname.en.md).

### Diagnostics (alongside M3 or later, optional)

Crash/error tracking (Sentry/GlitchTip, self-hostable), OTel Trace (official OTel SDK → any OTLP collector), performance metrics, event analytics; everything off by default, and an unconfigured setup is a no-op with zero overhead. See [contract/diagnostics.md](./contract/diagnostics.en.md).

### Payment: Contract Only (M5)

Payment shapes differ from game to game; the Contract sets no "unified wallet" assumption and allows:

```text
Payment
├── Account Wallet     account-scoped wallet (balance shared across games)
├── Game Wallet        game-scoped wallet
├── Direct Purchase    direct purchase
└── External Payment   external payment (channel IAP, etc.)
```

The contract freezes only `Order / Purchase / Receipt` and the security boundary (callbacks trust only channel signatures); which shape to pick is decided by each game's configuration. The interface shape (`Order / Channel / Callback / Reconcile`) is still frozen up front to avoid rework.

## Key Decisions

1. **The contract is the constitution.** Constituted and frozen at M0 ([contract/](./contract/index.en.md)); the precondition for M1 Auth work. SDK Contract > Everything.
2. **The gateway is the only entry point.** Clients talk only to the Courier Gateway; Providers and backend topology are invisible to the client.
3. **Protocol: HTTPS JSON first.** Push goes over SSE from M2 on (the frozen selection in messages.md: fully implementable with the standard library, HTTP semantics reused, Unity UnityWebRequest can stream reads; WebSocket is left as a later extension and needs a version bump to write), reusing the session channel of the default push Provider (chirp), and the channel itself is replaceable.
4. **One scope only.** `game_id + env` is specified once at initialization and injected as a header; see [contract/scope.md](./contract/scope.en.md).
5. **The Gateway has no business directories.** Business always arrives via Provider interfaces; `providers/` holds the interfaces and the default implementations.
6. **Provided by default, swappable and disableable.** Ecosystem projects are just default providers; not connected = capability degradation, not an error.
7. **Collection/compliance off by default.** The red lines of Diagnostics and RealName are built into the contract.
8. **Payment defines the contract only.** All four shapes stay open; wallet assumptions do not enter the contract.
9. **UI is always optional.** Core/Service/UI are three separate packages; Core has no UI and no branding logic.
10. **Account ≠ player.** account is company-level, player is game-level, never mixed.

## SDK Structure (Isomorphic Across Platforms)

```text
CourierClient (facade)
  .Identity        login / logout / bind / device / oauth (optional, third-party)
  .Session         token state and rotation
  .Assistant       faq retrieval Q&A (optional, LLM provider swappable)
  .Player          profile / gamePlayer
  .App             version / checkUpdate / checkMaintenance / config / branding
  .Communication   announcements / messages / push
  .Support         createTicket / listTickets / getFaq        ← core/service packages
  .Payment         createOrder / queryOrder (M5, contract + built-in sandbox channel)
  .RealName        submit / status (optional, compliance)
  .Diagnostics     optional package access point (no-op by default)
  UI package       AnnouncementPanel / CustomerServicePanel / AssistantPanel (optional, consumes Branding)
```

- Each platform = a platform-agnostic Core (DTOs, lifecycle state machine, token storage, retry) + a platform binding (Unity UPM / UE plugin / Cocos TS / WeChat Mini Program / Layabox / Godot — project shapes are in place; plugin-packaged builds follow the engine-integration surface).
- **Adapters must carry no business logic**: an Adapter only translates "network, storage, lifecycle, threading" for the platform; any business semantics appearing there is an architecture violation (move it up into Core or Service).
- Public DTOs take [contract/](./contract/index.en.md) as the single source of truth, generated or hand-aligned per platform; inventing fields per platform is forbidden.
- Token storage keeps a unified interface (`ITokenStore` / `TokenStore`) landed per platform: Unity encrypted storage, WeChat storage (miniprogram `platform/wechat`), Layabox LocalStorage, and Godot `user://` persistent files are implemented; UE platform credentials and Cocos localStorage are not in-repo yet — the in-repo default is `MemoryTokenStore` (lost on process exit), with persistent adapters following the engine/integration surface (see [layers.md](./layers.en.md)).

## Security Boundaries

- The client holds no Provider credentials, internal URLs, or admin interfaces.
- Rate limiting for sensitive endpoints is built into the account Provider (stricter thresholds for sensitive endpoints such as register/login/guest/bind/refresh); the gateway middleware provides only basic global rate limiting; the real name domain has no rate limiting. During the payments sandbox period, pre-transaction risk controls follow the deployment surface (roadmap M5).
- Payment callbacks trust only channel signatures, never client-reported amounts/status.
- Real name: field minimization, encrypted transport, no SDK disk writes, plaintext banned from logs/traces ([contract/realname.md](./contract/realname.en.md)).
- Diagnostics: off by default, enabled explicitly, with the data scope stated explicitly when enabled ([contract/diagnostics.md](./contract/diagnostics.en.md)).
- Write operations are auditable: traces span gateway → Provider; the real name domain's critical actions (verify/curfew/charge_check/playtime_report) already land on a structured audit event surface (the `gateway/audit` shared seam, both realname implementations inject the same `AuditSink`, nil = no output; `COURIER_REALNAME_AUDIT=log` turns on the logging half), events contain only account-level metadata (identity inputs never enter events, the realname.md red line), and persistence follows the deployment surface.

## Boundary with Backend Providers

| Capability | Courier owns | Provider side owns | Default provider (swappable/disableable) |
| --- | --- | --- | --- |
| Account/session | Client surface and gateway governance | Account storage and verification | courier-account (self-built; second implementation courier-account-alt, swappable under the same contract) |
| Announcements | Player-side fetch/subscribe APIs, SDK components | Delivery orchestration, ops publishing | herald + croupier message; second implementation herald-alt (write-time indexing, swappable under the same contract) |
| Support | Player-side ticket submission/query APIs | Ticket workflow, agents | croupier support/ticket/faq; second implementation croupier-alt (write-time owner index + keyset cursor, swappable under the same contract) |
| Push | Event contract and pull fallback | Long-lived connection channel | chirp (fan-out; second implementation chirp-alt ring log + cursor, swappable under the same contract) |
| Real name | Contract and Provider governance | Verification execution | warden (self-hosted verification; Alibaba Cloud/Tencent Cloud Huiyan/Yidun/Webhook attachable in the same shape) |
| Config/branding/version | Contract, conditional projection, hot effect | Content publishing pipeline | scribe (self-built; second implementation scribe-alt write-time dimension index, swappable under the same contract) |
| Player profile | Contract and governance | Profile storage, character binding | archivist (self-built; second implementation archivist-alt event log, swappable under the same contract) |
| Assistant | Contract and retrieval surface | FAQ knowledge base, agents | sage (self-built FAQ; second implementation sage-alt write-time precomputed normalization, swappable under the same contract) |
| Payments | Contract and order surface | Channel callbacks, fulfillment, reconciliation | teller (self-built; second implementation teller-alt event-ledger projection, swappable under the same contract) |
| Diagnostics | Contract and red lines | Reporting storage and analysis | Bypasses the gateway, sent directly to the integrator's endpoints; Sentry/GlitchTip/OTLP |

## Competitive Positioning

Open source has no "service-oriented aggregate SDK" category (Nakama is a server, XtraLife is stalled, AWS is cloud templates); commercial aggregators (MSDK/QuickSDK/XDSDK) prove the demand exists but are closed-source and channel-locked. For a full inventory of Courier's positioning and boundaries (no realtime multiplayer/matching, no channel aggregation), see [research/competitive.md](./research/competitive.en.md).
