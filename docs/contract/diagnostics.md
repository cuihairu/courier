
# Diagnostics Contract (Diagnostics) — Optional Capability

> Status: **Frozen v1** (2026-10-09, M3) · optional package (not in the core package) · off by default.
> Philosophy: **unconfigured = no-op with zero overhead** — no instance is initialized, no background thread is started, no queue is built.
> Diagnostic data goes straight to an integrator-chosen endpoint (Sentry / OTLP collector / their own HTTP), **never through the Courier gateway**; neither the gateway nor the default providers are aware of this domain.

## Red Lines (built in, Providers must not override)

1. **Off by default**: all four capability types are off.
2. **Explicit opt-in**: each type has its own switch (`diagnostics.crash.enabled` etc.); enabling requires configuring the reporting endpoint at the same time; configuring the endpoint without flipping the switch = still off.
3. **Enabling means disclosure**: the data-scope table below is the external wording; the integrator's privacy policy must cover the selected categories.
4. **Disabled means zero reporting**: once off, no buffer remains, no cached queue is sent, no catch-up reports. v1 reporting is direct-send with no buffering, so this red line is guaranteed by construction (see the send semantics below).
5. **Sensitive-interface isolation**: request/response bodies of sensitive interfaces such as real-name verification never enter any diagnostic collection (a global red line, not configurable per module).
6. **Never hurts the game**: a failed diagnostic report (network error / non-2xx) is silently dropped — no retry, no caching, no throwing, no blocking of the calling thread.

## The Four Capability Types and the Provider Ecosystem

| Category | Content | Provider Ecosystem |
| --- | --- | --- |
| Crash / Error Tracking | Uncaught exceptions, error reporting, breadcrumbs | Sentry protocol (Sentry SaaS or self-hosted GlitchTip / Sentry) |
| Trace | Distributed tracing | Official OTel SDK, OTLP export, connects to any OTLP collector (self-hosted preferred) |
| Performance | Startup duration, jank frames, network RTT | OTel Metrics or the Sentry performance surface, mapped per Provider capability |
| Event Analytics | Game instrumentation events | Own HTTP endpoint (default) or as specified by the integrator |

The SDK side has one unified interface (one Provider interface per type); implementations are pluggable; a single type may be enabled on its own. The implementation built into Courier is **direct send to its own HTTP endpoint** (the wire schema below); the Sentry / OTLP adapters are wired to ecosystem SDKs by integrators via the same interface, and the contract does not bind their private wire.

## Configuration Shape (isomorphic for every type)

```json
{
  "crash":       { "enabled": false, "endpoint": "https://diag.example.com/crash" },
  "trace":       { "enabled": false, "endpoint": "https://diag.example.com/trace" },
  "performance": { "enabled": false, "endpoint": "https://diag.example.com/metrics" },
  "analytics":   { "enabled": false, "endpoint": "https://diag.example.com/events" }
}
```

- `enabled=false` (default) → that type's interface is a no-op: no instance is initialized, no thread started, no queue built, and calls have zero side effects.
- Runtime switch: turning off takes effect immediately — subsequent calls become no-ops and unsent data is dropped (v1 has no unsent buffer, so this holds naturally).
- The resource context (game_id/env/platform/app version/engine version/sessionId) is injected by the host at creation time; defaults apply when not collected.

## SDK-side Interface Semantics

| Category | Method Semantics |
| --- | --- |
| Crash | `Report(error/exception, stack)`: report one error; `Breadcrumb(text)`: attach context to the next error report (in-memory ring, capped at 20 entries, cleared when off) |
| Trace | `Span(name, durationMs, path)`: report a finished span; the URL records only the path, not the query |
| Performance | `Startup(ms)` / `Jank(ms)` / `Rtt(ms)`: the three registered metrics, see the metric registry |
| Analytics | `Track(eventName, props)`: one instrumentation event; validation see Data Scope |

Every method is non-blocking fire-and-forget: the signature returns no reporting result, and a failure never affects the caller.

## Reporting Schema and Data Scope (own-endpoint wire)

Uniform: POST `application/json`, one request per record, the body is a single object; a 2xx response = accepted (the response body is discarded), anything else drops the record. Absent fields are simply not sent.

**Crash / Error**

```json
{ "sessionId": "...", "platform": "ios", "appVersion": "2.0.0",
  "engineVersion": "2022.3", "osVersion": "17.4", "deviceModel": "iPhone15,3",
  "message": "...", "stack": "...", "breadcrumbs": ["..."], "traceId": "..." }
```

| Collected | Not Collected |
| --- | --- |
| sessionId, app/engine version, platform, OS version, device model, stack, breadcrumbs (added by the integrator), traceId | Location, contacts, photo library, clipboard, and payloads of sensitive interfaces such as real-name verification |

**Trace**

```json
{ "resource": { "service.name": "courier.sdk", "courier.game_id": "game_demo",
    "courier.env": "prod", "platform": "ios", "app.version": "2.0.0",
    "engine.version": "2022.3" },
  "traceId": "...", "name": "support.ticket.create", "durationMs": 42, "path": "/v1/support/tickets" }
```

The resource attributes are a fixed set; no request body is collected inside a span; the URL records only the path, not the query.

**Performance**

```json
{ "metric": "startup_duration_ms", "value": 1820,
  "dims": { "platform": "ios", "appVersion": "2.0.0" } }
```

Metric-name registry: `startup_duration_ms` / `frame_jank_ms` / `network_rtt_ms`; value + dimensions (platform, app.version), with no device-identifier dimension.

**Event Analytics**

```json
{ "eventName": "level_complete", "sessionId": "...", "level": 7, "durationS": 95.5 }
```

props are flattened into the object, primitive types only (string/number/bool); `eventName` is 1–64 characters and non-empty; a single record is at most 1KB in total — a violation is a caller error: the SDK rejects the record (throws an argument error), it does not silently truncate. Event names and the props structure are registered by the integrator; Courier does no server-side field interpretation.

## Packaging and Acceptance

- Belongs to the **Diagnostics optional package**; the core / service / UI packages do not depend on it and carry no diagnostic code path at all (including the no-op path).
- Enabling means the integrator accepts that category's data-scope table; turning the runtime switch off immediately stops collection and drops the unsent buffer.
- Acceptance: all four types report zero by default (not a single send happens); enabling them category by category takes effect category by category (only that category reports, the rest stay at zero); turning off returns to zero immediately.

## v1 Boundaries

- Direct send with no buffering: crash-scene re-reporting (re-sending a last-session crash at the next startup) belongs to the Provider ecosystem (the Sentry SDK etc. carry it); the contract does not build it in.
- Sampling/redaction configuration is not in v1; integrators handle it on their self-hosted endpoint side.

---

## Extension Reservations (Note Added 2026-10-11)

| Possible Extension | Compatible Handling |
| --- | --- |
| New diagnostic categories (e.g. ANR / battery / network quality) | Adding a Provider interface = compatible; the optional package's off-by-default red line is unchanged — **new categories are also off by default** |
| Report payload extension | analytics `props` is already an open set (primitive values, ≤1KB); the other categories add optional fields with unknown-field tolerance |
| Sampling / retention / redaction policies | Handled by the integrator on the self-hosted endpoint side (this contract's wording); the contract side reserves an optional `sampling` configuration slot (v1.1+) |
| Consent-state linkage (privacy withdrawal) | Reserved for v1.1+: the host passes in a consent state → all types stop reporting; the red line "turning off takes effect immediately" is already built in |
| Reporting through the gateway | **Never** — direct send bypassing the gateway is a contract red line and does not loosen with extensions |
