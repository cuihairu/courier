[English](./sdk-selection.en.md) | [中文](./sdk-selection.md)

# Selective Installation Checklist (Measured in Bytes)

> Purpose: answers "which parts can be skipped, what each one you include saves, and what you get in return". The measurement methodology is given in the last section.

## The Four UPM Packages (Unity)

Packages are split into four layers by dependency direction: core is the base being depended on, service depends on core, and ui/diagnostics depend on service. **Install only what you actually use.**

| Package | Measured source bytes | What you save by skipping it | What you gain by installing it |
| --- | --- | --- | --- |
| `com.courier.core` | 77,851 B(~76 KiB) | — the foundation, can't be omitted | scope header / Bearer attachment / envelope parsing, lifecycle state machine, ApiClient backoff retry and `AUTH_TOKEN_EXPIRED` refresh-and-replay, Identity/Session orchestration (refresh single-flight), transport and token-storage abstractions |
| `com.courier.service` | 58,102 B(~57 KiB) | writing the nine domains' REST calls yourself | `CourierServices` facade: announcements / support (including attachment direct upload) / app config / branding / player / assistant / payments / real name + SSE parsing |
| `com.courier.ui` | 22,673 B(~22 KiB) | writing the panel rendering yourself | announcements panel + marquee (`MarqueeRendered` event), support panel; pure logic C#, not bound to any UI framework, rendering happens through callbacks/events |
| `com.courier.diagnostics` | 20,495 B(~20 KiB) | no telemetry reporting | the three streams: trace correlation / performance events / crash reporting (diagnostics.md) |

Minimal usable set `core + service` = 135,953 B (~133 KiB); all four packages = 179,121 B (~175 KiB).
**Common trimming**: if the game has no live-ops announcements, drop ui; if you already have your own telemetry pipeline, drop diagnostics; with a fully self-built backend where you only need login/session, install core alone.

## The Three TS Platforms (cocos / miniprogram / laybox)

A single isomorphic tree, not splittable by directory (one `src/`), but dead code elimination works by whether you use ui/diagnostics/platform:

| Subtree | Measured bytes | Notes |
| --- | --- | --- |
| `core/` | 44,448 B | the foundation (includes `FetchTransport`; on the miniprogram side `createWechatTransport(wx)` is used) |
| `service/` | 38,793 B | the nine-domain facade |
| `ui/` | 13,323 B | announcements panel + marquee (`onMarqueeRendered`) |
| `diagnostics/` | 16,088 B | three-stream reporting |
| `platform/` | 12,179 B | keep only the adapter for the platform you target; the rest can be deleted |
| `contract/` | 7,659 B | generated code; do not delete |

Minimal set `core + service` ≈ 83,241 B (~81 KiB); full tree 132,490 B ≈ 129 KiB.

## UE (header-only) and Godot (single tree)

| Target | Measured bytes | Notes |
| --- | --- | --- |
| UE `contract + core + service` | 241,095 B (~235 KiB) | header-only tree; unused service headers are simply never included and never make it into the target file (no UI package, no diagnostics package) |
| Godot `src/` | 61,434 B(~60 KiB) | includes the platform pieces `platform/` (HTTPClient/FileTokenStore); UI awaits engine integration |

## The Provider Dimension (gateway side, not package size)

Not wiring up a Provider for a capability domain = that domain returns 501 `COMMON_CAPABILITY_DISABLED` (not a fault); the SDK then safely hides the corresponding UI per the contract. Default routing table (`cmd/gateway/main.go`):

| Capability domain | Default Provider | Consequence of not wiring it |
| --- | --- | --- |
| `identity` | `courier-account` (can be swapped for `courier-account-alt`) | no login/session |
| `announcements` | `herald` | announcements hidden |
| `support` | `croupier` | support hidden (including attachment direct upload) |
| `messages` | `chirp` | no push channel (SSE stream unavailable) |
| `app` | `scribe` | no config/branding/version (the maintenance gate goes dead with it) |
| `player` | `archivist` | no player profile |
| `assistant` | `sage` | no assistant |
| `payments` | `teller` | no payments |
| `realname` | no default (you must wire one up yourself) | real name / anti-addiction surface hidden |

Replacement and degradation semantics: see the routing package (`gateway/routing`) and [layers.en.md](./layers.en.md): primary + `fallbacks[]`; health checks are per-request probes (no caching); the circuit breaker has only landed in the realname domain (batch 7); a disabled Provider or a fully unhealthy chain returns 501 / 503 `COMMON_UNAVAILABLE` respectively. The complete onboarding steps for a self-built or replacement backend are in [provider-guide.en.md](./provider-guide.en.md).

## Measurement Notes

- Bytes = **source code** bytes measured with `du -sb` on 2026-10-13 (batch 63 re-measurement: core includes the refresh single-flight implementation; UE includes test cpp; the TS three platforms had zero changes and were not re-listed); UPM uniformly takes `packages/*/Runtime/` (the package root additionally contains README/package.json, which is not source code) — batch 55's original measure was mixed (the last two packages took the package root), batch 60 unified it: UPM packages compile inside the engine and produce no standalone build artifacts, and the TS three platforms have no bundler and produce no build artifacts — what is given here is the "integration footprint", not download size or in-engine footprint.
- The three TS repos' code is generated in sync (`tools/tsync`); the bytes of `cocos/src/` stand for the common measure across the three platforms: the `service/core/contract/ui/diagnostics` subtrees are **byte-identical** across all three platforms (verified by diff); the difference lies only in the `platform/` adaptation layer — miniprogram +24 B, laybox +1,473 B (relative to cocos).
- Real engine-side artifacts (Unity IL2CPP stripping, UE GC segments, Godot bytecode) vary with the project's dependency graph; this checklist does not estimate them.
