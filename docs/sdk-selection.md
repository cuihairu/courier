
# Selective Installation Checklist (Measured in Bytes)

> Purpose: answers "which parts can be skipped, what each one you include saves, and what you get in return". The measurement methodology is given in the last section.

## The Four UPM Packages (Unity)

Packages are split into four layers by dependency direction: core is the base being depended on, service depends on core, and ui/diagnostics depend on service. **Install only what you actually use.**

| Package | Measured source bytes | What you save by skipping it | What you gain by installing it |
| --- | --- | --- | --- |
| `com.courier.core` | 79,759 B(~78 KiB) | — the foundation, can't be omitted | scope header / Bearer attachment / envelope parsing, lifecycle state machine, ApiClient backoff retry and `AUTH_TOKEN_EXPIRED` refresh-and-replay, Identity/Session orchestration (refresh single-flight), transport and token-storage abstractions |
| `com.courier.service` | 62,786 B(~61 KiB) | writing the nine domains' REST calls yourself | `CourierServices` facade: announcements / support (including attachment direct upload) / app config / branding / player / assistant / payments / real name + the consent age gate (v1.1) + SSE parsing |
| `com.courier.ui` | 22,673 B(~22 KiB) | writing the panel rendering yourself | announcements panel + marquee (`MarqueeRendered` event), support panel; pure logic C#, not bound to any UI framework, rendering happens through callbacks/events |
| `com.courier.diagnostics` | 20,495 B(~20 KiB) | no telemetry reporting | the three streams: trace correlation / performance events / crash reporting (diagnostics.md) |

Minimal usable set `core + service` = 142,545 B (~139 KiB); all four packages = 185,713 B (~181 KiB).
**Common trimming**: if the game has no live-ops announcements, drop ui; if you already have your own telemetry pipeline, drop diagnostics; with a fully self-built backend where you only need login/session, install core alone.

## The Three TS Platforms (cocos / miniprogram / laybox)

A single isomorphic tree, not splittable by directory (one `src/`), but dead code elimination works by whether you use ui/diagnostics/platform:

| Subtree | Measured bytes | Notes |
| --- | --- | --- |
| `core/` | 44,570 B | the foundation (includes `FetchTransport`; on the miniprogram side `createWechatTransport(wx)` is used) |
| `service/` | 41,575 B | the nine-domain facade + the consent age gate (v1.1) |
| `ui/` | 13,323 B | announcements panel + marquee (`onMarqueeRendered`) |
| `diagnostics/` | 16,088 B | three-stream reporting |
| `platform/` | 12,179 B | keep only the adapter for the platform you target; the rest can be deleted |
| `contract/` | 7,659 B | generated code; do not delete |

Minimal set `core + service` ≈ 86,145 B (~84 KiB); full tree 135,394 B ≈ 132 KiB.

## UE (header-only) and Godot (single tree)

| Target | Measured bytes | Notes |
| --- | --- | --- |
| UE `contract + core + service` | 288,453 B (~281 KiB) | header-only tree; unused service headers are simply never included and never make it into the target file (no UI package, no diagnostics package) |
| Godot `src/` | 63,914 B(~62 KiB) | includes the platform pieces `platform/` (HTTPClient/FileTokenStore); UI awaits engine integration |

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
| `realname` | no default (you must wire one up yourself) | real name / anti-addiction surface hidden; the consent group (v1.1) additionally needs `COURIER_REALNAME_CONSENT*` to enable, otherwise the age-gate UI stays hidden (501 → null) |

Replacement and degradation semantics: see the routing package (`gateway/routing`) and [layers.md](./layers.md): primary + `fallbacks[]`; health checks are per-request probes (no caching); the circuit breaker has only landed in the realname domain (batch 7); a disabled Provider or a fully unhealthy chain returns 501 / 503 `COMMON_UNAVAILABLE` respectively. The complete onboarding steps for a self-built or replacement backend are in [provider-guide.md](./provider-guide.md).

## Measurement Notes

- Bytes = **source code** bytes measured with `du -sb` on 2026-10-19 (batch 102 re-measurement: unity service / TS service / UE service / Godot src include the consent-group v1.1 growth; batch 97: unity core includes the lifecycle signal relay and refresh single-flight, TS core includes the lookup-key fix, UE includes the test-closeout growth; historical batches 63/60/55 in git); UPM uniformly takes `packages/*/Runtime/` (the package root additionally contains README/package.json, which is not source code) — batch 55's original measure was mixed (the last two packages took the package root), batch 60 unified it: UPM packages compile inside the engine and produce no standalone build artifacts, and the TS three platforms have no bundler and produce no build artifacts — what is given here is the "integration footprint", not download size or in-engine footprint.
- The three TS repos' code is generated in sync (`tools/tsync`); the bytes of `cocos/src/` stand for the common measure across the three platforms: the `service/core/contract/ui/diagnostics` subtrees are **byte-identical** across all three platforms (verified by diff); the difference lies only in the `platform/` adaptation layer — miniprogram +24 B, laybox +1,473 B (relative to cocos).
- Real engine-side artifacts (Unity IL2CPP stripping, UE GC segments, Godot bytecode) vary with the project's dependency graph; this checklist does not estimate them.
