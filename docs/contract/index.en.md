
# Courier SDK Contract (M0 Constitutional)

> **Status**: the five primitives are **Frozen v1** (2026-10-08, frozen at the M0 cross-platform review; the error code table is now **v8** — v2 added M2 domain codes, v3 added REALNAME time-window/quota codes, v4 added M5 PAYMENT domain codes, v5 added AUTH deregistration codes, v6 added OAUTH domain codes, v7 added SUPPORT attachment codes (2026-10-12), v8 added SUPPORT report/appeal codes (2026-10-13, 42 codes); **v1.1** on 2026-10-10 added the two sections on cancellation and concurrency discipline); the auth domain contract is **Frozen v1.1** (M1; v1.1 on 2026-10-12 added account deregistration and data deletion F03, and the OAUTH sign-in method); the oauth domain contract is **Frozen v1** (batch 52, 2026-10-12, F01); the announcement / support domain contracts are **Frozen v1.1** (M2; v1.1 added announcement targeting filters F35 and multilingual content F76, ticket context F43, ticket attachments F44, reports and penalty appeals F45); the messages domain contract is **Frozen v1.1** (M2, v1.1 added the hard requirement on transport framing); the realname domain contract is **Frozen v1** (late M2); the config / app / branding / player domain contracts are **Frozen v1** (M3, 2026-10-09); the diagnostics domain contract is **Frozen v1** (M3, optional package default fully off); the assistant domain contract is **Frozen v1** (M4, 2026-10-09); the payment domain contract is **Frozen v1** (M5, 2026-10-09; v1.1 added the payment.paid/delivered push events, 2026-10-09). After freezing, changes enter change-process management.
> **Principle**: **SDK Contract > Everything** — the contract is Courier's constitution. The Gateway, Provider, Core, Adapter, and ecosystem backends (herald / croupier / chirp / warden / self-hosted accounts) are all merely **implementers** of the contract; adding an engine = adding an Adapter, not redesigning the SDK.

## Why a Constitution

Courier's value lies not in any particular feature, but in "one set of capability interfaces, consistent behavior across engines and platforms." That consistency can only be guaranteed by the contract:

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
      Unity      UE      Cocos  …
```

The Gateway, Provider, herald, Chirp, Croupier, and Oddsmaker are all just "implementers of the contract / backend providers", not the core of Courier.

## Constitutional Principles

1. **Contract first**: for any cross-platform capability, change the contract and get it reviewed and frozen before touching the implementation. Before M1 Auth started, the M0 contract had to be frozen.
2. **Single source of truth**: this directory is the single source of truth for all cross-platform types, error codes, events, and semantics; no platform SDK may invent its own fields (the landing point of the design boundary "no fobbing off with any/unknown").
3. **Cross-platform consistency**: the same semantics have different naming styles in C# / C++ / TypeScript / GDScript but behave identically.
4. **Vendor-agnostic**: the contract describes "capabilities", not "which implementation" — the announcement contract does not know herald exists. The default provider is configuration, not contract.
5. **Off by default (red line)**: data-collection and compliance capabilities (Diagnostics / RealName) have a built-in red line in the contract — off by default, working only when explicitly enabled, and enabling must state the data scope.
6. **Field minimization**: don't collect sensitive fields if you can avoid it; every contract involving personal data must also state "what is not collected".
7. **Backward compatibility**: frozen contracts are additive-only; breaking changes must bump the version (see [versioning.md](./versioning.en.md)).

## Contract Layers

| Layer | Content | Freeze point |
| --- | --- | --- |
| Primitive contracts | [primitives](./primitives.en.md) / [errors](./errors.en.md) / [scope](./scope.en.md) / [versioning](./versioning.en.md) / [events](./events.en.md) | **M0 frozen v1** |
| Domain contracts | [auth](./auth.en.md) (**Frozen v1.1 · M1**); [announcement](./announcement.en.md) / [support](./support.en.md) (**Frozen v1.1 · M2**; announcement v1.1 includes targeting filters F35 and multilingual content F76, support v1.1 includes ticket context F43, ticket attachments F44, and reports/appeals F45) / [messages](./messages.en.md) (**Frozen v1.1 · M2**, v1.1 hard requirement on transport framing); [realname](./realname.en.md) (**Frozen v1 · late M2**); [config](./config.en.md) / [app](./app.en.md) / [branding](./branding.en.md) / [player](./player.en.md) (**Frozen v1 · M3**); [diagnostics](./diagnostics.en.md) (**Frozen v1 · M3 · optional package default fully off**); [assistant](./assistant.en.md) (**Frozen v1 · M4**); [payment](./payment.en.md) (**Frozen v1.1 · M5**) | All domain contracts frozen; additive-only from here on, breaking changes bump the version |
| Domain contracts (later) | [oauth](./oauth.en.md) (third-party identity providers, **Frozen v1 · batch 52**, 2026-10-12) | All domain contracts frozen; additive-only from here on, `provider` is an open-set string |

## Change Process

1. Proposal: modify the corresponding file in this directory and mark it as a version draft (Draft). **Every proposal must contain an "Extensions and Evolution" section** — listing the anticipated extension points and how compatibility is handled (add optional fields / open-set map + tolerance rules / management-plane evolution / Provider hook registration); **it is forbidden to nail down semantics that may change as a closed enum**; nailing down interface semantics = the next extension can only be a major version bump.
2. Review: cross-platform leads go through it (at least Unity + one non-C# platform).
3. Freeze: remove the Draft marker and record it in the version number.
4. Sync: each platform SDK aligns to the contract; syncing is complete only when the contract tests pass.
5. Breaking changes go through the version-bump process in [versioning.md](./versioning.en.md), never an in-place rewrite.

## M0 Freeze Review Record (2026-10-08)

Review perspectives (the rule requires ≥ Unity + one non-C# platform; in practice four platforms were read through): Unity (C#), Cocos/mini-program (TypeScript), UE (C++), Godot (GDScript).

| # | Finding | Disposition |
| --- | --- | --- |
| 1 | primitives' "every response body must carry traceId" contradicts "successful responses have only data", and is inconsistent with the gateway implementation (success has no body traceId, `X-Request-Id` is echoed back in a header) | Unified as: traceId appears only in the failure response body; `X-Request-Id` handles correlation for successful requests and is not mixed with traceId |
| 2 | The Timestamp field suffix example used snake_case (`created_at`), inconsistent with the already-frozen auth contract and the gateway's camelCase wire format | Corrected to `At`/`Ms` suffixes, camelCase wire keys, with per-platform property names following language convention |
| 3 | The events `type` rule said "lowercase dot-separated", but the examples contained camelCase segments (`tokenExpired`/`signedOut`/`accountSwitched`/`ticketReplied`/`maintenanceChanged`/`forceUpdate`) | The rule was clarified to two lowercase segments + underscores for multi-word names, and all examples were normalized |
| 4 | primitives' "enums are always UPPER_SNAKE_CASE" conflicted with scope's lowercase `env` values (dev/staging/prod) | primitives marks scope.md as the sole exception |
| 5 | The versioning compatibility matrix's "SDK 0.x binds Draft v0" contradicted the fact that M0 froze before M1 started | The matrix was changed so SDK 0.x binds Contract v1.x |
| 6 | The index and the layer table did not include the already-frozen auth.md | The index and contract layer table were put in place |

Conclusion: the five primitives were promoted to **Frozen v1**; realname / branding / diagnostics remained first drafts (to be frozen before implementation); auth had already been frozen in M1. After the review, the five primitives entered change-process management, additive-only.

## Index

- [primitives.md](./primitives.en.md) — Request / Response / Pagination / Timestamp / ID / TraceID
- [auth.md](./auth.en.md) — Authentication and sessions (Frozen v1.1 · M1)
- [announcement.md](./announcement.en.md) — Announcements (player-side read-only projection; Frozen v1.1 · M2)
- [support.md](./support.en.md) — Support tickets and FAQ (Frozen v1.1 · M2)
- [messages.md](./messages.en.md) — Push channel (SSE, session reuse; Frozen v1.1 · M2)
- [errors.md](./errors.en.md) — Error body, error code registry, HTTP mapping, degradation signal
- [scope.md](./scope.en.md) — game_id + env isolation
- [versioning.md](./versioning.en.md) — Three-layer versioning and compatibility rules
- [events.md](./events.en.md) — Event envelope, lifecycle events, push channel
- [realname.md](./realname.en.md) — Real-name verification (off by default, Provider hot-swap; Frozen v1 · late M2)
- [config.md](./config.en.md) — Remote config (key-value + version + conditional rollout projection; Frozen v1 · M3)
- [app.md](./app.en.md) — App status (version/maintenance/environment; Frozen v1 · M3)
- [branding.md](./branding.en.md) — Branding customization (no branding logic in Core; Frozen v1 · M3)
- [player.md](./player.en.md) — Player profile (account↔character mapping; Frozen v1 · M3)
- [diagnostics.md](./diagnostics.en.md) — Diagnostics (off by default, reporting Schema for four capability types; Frozen v1 · M3)
- [assistant.md](./assistant.en.md) — Assistant (FAQ retrieval Q&A + handoff to human; Frozen v1 · M4)
- [payment.md](./payment.en.md) — Payments (order-to-delivery core flow, four forms open; Frozen v1.1 · M5, v1.1 added push events)
- [oauth.md](./oauth.en.md) — Third-party identity providers (credential-to-session exchange + binding; Frozen v1 · batch 52)
