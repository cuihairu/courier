<p align="center"><img src="docs/public/logo.svg" width="64" height="64" alt="logo" /></p>

<h1 align="center">Courier — Universal Game SDK</h1>

<p align="center">
  <img src="https://img.shields.io/badge/TypeScript-3178C6?logo=typescript&logoColor=white" alt="TypeScript" />
  <img src="https://img.shields.io/badge/Go-00ADD8?logo=go&logoColor=white" alt="Go" />
  <img src="https://img.shields.io/badge/Node.js-339933?logo=nodedotjs&logoColor=white" alt="Node.js" />
  <img src="https://img.shields.io/badge/license-Apache%202.0-blue.svg" alt="License: Apache-2.0" />
</p>

<p align="center">[English](README.md) | [中文](README.zh.md)</p>

> **A universal SDK for integrating common game services across engines and platforms.**
>
> Courier is a universal SDK for games. It exposes one set of interfaces for account, authentication, player, messaging, announcements, customer support, payments, real-name verification, remote configuration, branding, and diagnostics across game engines such as Unity, Unreal, Cocos, and Godot.

Courier means messenger: it is not any single business feature. It delivers service capabilities to the game client on the game's behalf.

```text
 Service capabilities (Providers): account · announcements · support · messaging ·
 payments · real-name · config · branding · diagnostics
                             │
                             ▼ pickup
                       Courier (the messenger)
                             │
                             ▼ delivery
                       Game Client
        Unity / Unreal / Cocos / Godot / Mini Program / Layabox
```

## Scenario

A game company runs multiple games (MMO, card, tower defense, ...). Each game used to build its own account, announcement, support, and payment systems. Players had to register again in every game, and the operations team switched between separate back offices.

Courier addresses this:

```text
Player's view:
  register one account → sign in to any game → payments per game → unified support

Operations' view:
  one contract covers the common capabilities of all games → each game opts in as
  needed → data flows across games
```

## Design Principle: SDK Contract > Everything

The contract is the constitution. The gateway, providers, and backend services are all its implementers. Adding an engine means adding an adapter, not redesigning the SDK.

**Provider principle: provided by default, replaceable, and disableable.** The backend of every capability is a pluggable provider. Courier ships a set of implementations that work out of the box, but whether to connect one, which vendor to use, and whether to swap it are decided by the integrator's configuration. No hard binding, no forced dependency: a capability that is not connected degrades gracefully instead of raising errors.

## Architecture: Five Layers

```text
SDK Contract      The common contract for every SDK (types, error codes, protocol);
                  consistent across clients; ratified at M0
Core             Engine-independent core logic (lifecycle state machine, session,
                  cache, retry, serialization)
Platform Adapter Platform adaptation (Unity/Unreal/Cocos/Godot/Mini Program:
                  lifecycle, storage, networking; no business logic allowed)
Service Provider Provider interfaces + default implementations (pluggable:
                  self-hosted / third-party / disabled)
Gateway          Player-facing API gateway (Go: auth, session, scope, routing,
                  aggregation, middleware, providers)
```

Layer-by-layer design: [docs/layers.md](docs/layers.md). Overall architecture, capability tree, SDK lifecycle, and provider governance: [docs/architecture.md](docs/architecture.md). **The contract constitution: [docs/contract/](docs/contract/index.md).** Batch-by-batch progress: [docs/todo.md](docs/todo.md).

## Positioning

Courier is not another standalone backend. It is a **player-side SDK plus a lightweight API gateway**; backend capabilities all connect through provider interfaces:

```text
Game Client (Unity / UE / Cocos / Mini Program / Layabox / Godot)
  -> Courier SDK (C# / C++ / GDScript / TypeScript / JavaScript)
  -> Courier Gateway (player API gateway: auth, session, scope, routing, aggregation)
  -> Backend Providers (provided by default, replaceable, disableable):
     - AccountProvider       default: Courier's built-in accounts/sessions
     - AnnouncementProvider  default: herald (event-driven notification delivery)
     - SupportProvider       default: croupier support / ticket / faq
     - MessageProvider       default: chirp (gateway + session)
     - RiskProvider          default: oddsmaker (top-up risk control, behavior analysis)
     - RealNameProvider      default: disabled (self-hosted / Alibaba Cloud /
                             Tencent Cloud Huiyan / Yidun / Webhook can be connected)
     - ConfigProvider        remote config, built-in by default
     - BrandingProvider      branding assets, built-in by default (same pipeline as Config)
     - DiagnosticsProvider   default: disabled (Sentry/GlitchTip/OTLP can be connected)
```

The default providers are an out-of-the-box convenience, not a lock-in. Any of them can be configured as a self-hosted HTTP endpoint, a third-party service, or switched off entirely (the client then safely hides the corresponding capability).

### Account Model

```text
One company-wide account
  ├── Game A: character 1 (player_id_A)
  ├── Game B: character 2 (player_id_B)
  └── Game C: character 3 (player_id_C)
```

- **One account signs in to every game**: a player registers once and enters any game under the company with the same credentials.
- **Characters stay per-game**: the same account has independent character data, inventory, and level in each game.
- **Payment shapes stay open**: the contract only defines Order/Purchase/Receipt. Account wallet, per-game wallet, direct purchase, or external payment is each game's choice; no unified-wallet assumption.
- **Unified support**: tickets and FAQ are isolated by game_id, while the same account's ticket history is visible across games.

Scope model: `game_id + env` isolates globally. It is specified once at initialization and must not appear in URLs or payloads (see [contract scope.md](docs/contract/scope.md)).

## Capability Tree

```text
Courier
├── Core          Lifecycle / Network / Error / Retry / Cache / Telemetry hooks
├── Identity      Login / Logout / Bind / Device / RealName (optional, compliance)
├── Session       AccessToken / RefreshToken / Session
├── Player        Profile / Game Player
├── App           Version / Maintenance / Remote Config / Branding
├── Communication Announcement / Message / Push
├── Support       FAQ / Ticket (core) + Panel (optional UI package)
├── Payment       Order / Purchase / Receipt (contract only; four shapes optional)
├── Diagnostics   Crash / Error Tracking / OTel Trace / Perf / Analytics (optional package)
└── Platform      Unity / Unreal / Cocos / Godot / Layabox / MiniProgram
```

SDK packages: **Core (required) / Service (as needed) / UI (optional)**. `CreateTicket()` lives in the core; `CourierSupportPanel` lives in the optional UI package. Diagnostics is likewise an optional package, disabled by default and a zero-cost no-op when unconfigured.

| Capability | Description | Default Provider (replaceable/disableable) | Milestone |
| --- | --- | --- | --- |
| Identity / Session | Register/sign in (email+password, guest device), token rotation, device binding; one account for all games | Built-in accounts/sessions | M1 |
| Announcements | Fetch/subscribe to announcements, events, maintenance notices (isolated per game) | herald + croupier message | M2 |
| Support | Ticket submission/query, FAQ search (account-scoped, visible across games) | croupier support/ticket/faq | M2 |
| Real-name (optional) | Real-name verification + anti-addiction hook interface; hot-swappable provider (degradation chain + circuit breaker); disabled by default, explicit data-scope notice when enabled | Self-hosted / Alibaba Cloud / Tencent Cloud Huiyan / Yidun / Webhook | late M2 |
| Remote Config | Condition matching on game_id/env/platform/version/region, hot effect | Built-in (publishable via croupier) | M3 |
| Player Profile | Account↔character mapping and profile interfaces; character data belongs to each game | Built-in (lightweight) + each game server | M3 |
| App | GetVersion / CheckUpdate / CheckMaintenance / GetEnvironment | Built-in | M3 |
| Branding | Company name/logo/theme color/about page/support entry, delivered per game_id+env; consumed by the UI package, Core has no branding logic, Courier-branded fallback by default | Built-in (same pipeline as Config) | M3 |
| Diagnostics (optional) | Crash/error tracking, OTel Trace, performance metrics, analytics events; all off by default | Sentry/GlitchTip/self-hosted OTLP | M3+ |
| Assistant | FAQ bot, in-game guide | Can connect to croupier faq | M4 |
| Payment | Contract only: Order/Purchase/Receipt; the four shapes are each game's choice | Channel abstraction + RiskProvider pre-check | M5 |

## Repository Layout

```text
gateway/    Courier API gateway (Go): auth, session, scope, routing, aggregation,
            middleware, providers
sdks/
  unity/    Unity SDK (C#, UPM packages)
  ue/       Unreal Engine SDK (C++ plugin)
  cocos/    Cocos Creator SDK (TypeScript)
  miniprogram/  WeChat Mini Program SDK (JavaScript)
  laybox/   Layabox SDK (TypeScript/JavaScript)
  godot/    Godot SDK (GDScript/C#)
docs/       Contracts (contract/), architecture, five-layer design, competitive
            research (research/), roadmap, TODO
```

## Design Boundaries

1. **The SDK serves players only**: no operations/admin capabilities; those are out of scope for this SDK (default provider Croupier, or self-hosted).
2. **The gateway is the single entry point**: clients never talk to backend providers directly; the gateway handles authentication, rate limiting, aggregation, and risk-control pre-checks.
3. **Providers live only in providers/**: the gateway contains no business directories; business logic always connects through provider interfaces. A capability that is not connected degrades (`COMMON_CAPABILITY_DISABLED`) instead of erroring.
4. **One account, many games**: the account is the top-level entity across games; characters (players) are per-game and created independently by each game.
5. **Payments define contract only**: the four shapes stay open, and the unified-wallet assumption stays out of the contract; the sandbox channel runs the full chain first, and callbacks trust channel signatures only.
6. **Collection/compliance off by default**: the red lines of Diagnostics and Real-name are built into the contract — disabled by default, enabled explicitly, with the data scope stated when enabled.
7. **UI is always optional**: Core/Service/UI packages are separate; Core has no UI and no branding logic.
8. **No any/unknown hand-waving**: public types in every SDK align with the contract; inventing fields per client is forbidden.
9. **No real-time multiplayer, no channel aggregation**: real-time matchmaking (Nakama/Agones territory) and channel-package publishing (MSDK/QuickSDK territory) are out of scope; see the [competitive research](docs/research/competitive.md).

## Documentation Index

- Contract constitution (M0): [docs/contract/](docs/contract/index.md) — primitives/errors/scope/versioning/events + real-name/branding/diagnostics domain contracts
- Architecture: [docs/architecture.md](docs/architecture.md) · Five layers: [docs/layers.md](docs/layers.md)
- Competitive research: [docs/research/competitive.md](docs/research/competitive.md)
- Roadmap: [docs/roadmap.md](docs/roadmap.md) · Batch TODO: [docs/todo.md](docs/todo.md)

## License

Apache-2.0
