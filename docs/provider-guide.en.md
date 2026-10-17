
# Custom Provider Integration Guide

> For integrators who want to **replace or add** a gateway capability-domain backend (Provider). The default vendors can be swapped or turned off;
> the client SDK needs **zero changes** (the wire is frozen by `docs/contract/`, see [sdk-selection.en.md](./sdk-selection.en.md)
> for the default wiring of the nine domains and the 501 degradation consequences). For governance principles see "Provider Governance" in [architecture.en.md](./architecture.en.md):
> a unified interface ≠ a data migration; there are two integration modes (server-side Provider by default / client-side direct connection as the exception).

## 1. Minimal Integration: Four Methods + Registration + Routing Table

A Provider = one `providers.Handler` (`gateway/providers/registry.go`), served under the `/v1/{capability}/` prefix:

```go
type Handler interface {
    Name() string                                  // vendor identifier (e.g. "herald"), unique within the registry
    Capability() providers.Capability              // capability domain; multiple may register for the same capability (degradation chain candidates)
    HealthCheck(ctx context.Context) error         // single availability probe
    ServeHTTP(w http.ResponseWriter, r *http.Request)  // Path retains the capability-domain prefix
}
```

Three steps to integrate (assembly precedent: `gateway/cmd/gateway/main.go`):

```go
reg := providers.NewRegistry()
_ = reg.Register(myProvider) // duplicate registration of the same name errors; multiple registrations for the same capability become degradation chain candidates
```

```json
// COURIER_GATEWAY_CONFIG (JSON routing table, overrides the built-in defaults):
{"identity": {"primary": "courier-account-alt"},
 "announcements": {"primary": "herald", "fallbacks": ["my-herald"]},
 "payments": {"disabled": true}}
```

Scope note: this is a replacement **within the same gateway process** (compile-time import + configuration-time routing); the standalone-process / HTTP
sidecar form leaves the contract untouched and belongs to deployment-surface evolution.

## 2. Routing and Degradation Semantics

- One routing table row = `CapabilityConfig{primary, fallbacks[], disabled}`
  (`gateway/providers/config.go`); an empty `primary` = not configured, `disabled: true` = explicitly off,
  and both likewise return `501 COMMON_CAPABILITY_DISABLED`.
- Request dispatch walks the degradation chain running `HealthCheck` on each in turn (a per-request liveness probe, no health cache), falling through to the next on failure; circuit breaking has only landed in the real-name domain (`providers/router`, batch 7), while the generic degradation chain is a per-request liveness probe.
- `fallbacks` entries that reference an unregistered name are **silently skipped** (not crippled); a misspelled configuration key (an unknown capability domain) errors directly in
  `LoadConfig` rather than failing silently.
- Management semantics: a capability domain = a routing prefix, and a business name must not become a capability domain; a new capability first registers its domain prefix in
  [errors.md](./contract/errors.en.md), then enters `providers/capability.go` and the routing table.

## 3. The Full Set of Capability Domains and Default Vendors

| Capability domain | Prefix | Default Provider | Milestone | Notes |
| --- | --- | --- | --- | --- |
| identity | /v1/identity/ | `courier-account` | M1 | second implementation `courier-account-alt` (batch 18) |
| announcements | /v1/announcements/ | `herald` | M2 | second implementation `herald-alt` (write-time index, batch 69) |
| support | /v1/support/ | `croupier` | M2 | most injection points (see §4); second implementation `croupier-alt` (write-time owner index + keyset cursor, batch 73) |
| messages | /v1/messages/ | `chirp` | M2 | SSE push channel, the other domains broadcast through its Hub; second implementation `chirp-alt` (ring log + cursor, batch 68) |
| realname | /v1/realname/ | `warden` | late M2 | **off by default**: only enabled when explicitly configured in the routing table (contract red line); second implementation `warden-alt` (hosted verification, batch 64) |
| app | /v1/app/ | `scribe` | M3 | config / branding / app share one pipeline; second implementation `scribe-alt` (write-time dimension index, batch 71) |
| player | /v1/player/ | `archivist` | M3 | player profile + account↔character mapping; second implementation `archivist-alt` (event log, batch 67) |
| assistant | /v1/assistant/ | `sage` | M4 | FAQ retrieval; LLM vendors are swapped through the same interface and are not depended on by default; second implementation `sage-alt` (write-time precomputed normalization, batch 70) |
| payments | /v1/payments/ | `teller` | M5 | self-built sandbox channel; real channels integrate through the same callback signature surface; second implementation `teller-alt` (event ledger projection, batch 72) |

## 4. The Options Assembly Surface: Per-Domain Injection Points

Each Provider's `Options` struct is the sole assembly surface; **nil / not configured = fail-closed, neither a panic nor running wide open**:

| Domain | Injection point | Default behavior |
| --- | --- | --- |
| identity | `account.Options` / `accountalt.Options`; the session validator is shared with M2+ domains via `auth.NewSwitchable`, and switching the identity source = `SetTarget` following the primary | both implementations share one wire (see §6) |
| identity·oauth | `OAuthDisabled: true` turns the oauth domain off (501); the third-party credential validator is assembled via providers/oauth, and not registered = `OAUTH_PROVIDER_UNKNOWN` | fail-closed |
| support | `croupier.Options.AttachmentSigner` (F44 direct attachment upload), `PenaltyStore` (F45 penalty projection source: List/Get/Appeal) | nil = the attachment / penalty capabilities return 501 (no baking a fake store or fake risk control into production); `croupieralt.Options` has the same-named fields with the same semantics |
| support | `Notify` (agent reply → chirp broadcast), `TicketRatePerMinute/Burst` (default 5/minute) | nil = no push |
| payments | `teller.Options.ChannelSecret` (callback HMAC-SHA256), `Deliver` fulfillment hook (idempotent per order, the integrator persists to its own store) | empty secret = callbacks uniformly 403 `PAYMENT_INVALID_SIGNATURE` |
| realname | `warden.Options.S2SToken`, `AuditSink` (F33 audit event surface: verify/curfew/charge_check/playtime_report, events carrying only account-dimension metadata; the seam is in the shared package `gateway/audit`, with the built-in `LogSink` log half and `HTTPSink` direct-write half, and the SIEM ingestion side / self-built persistence proxy injected per deployment surface; both implementations have the same shape, as does `wardenalt.Options.AuditSink`); `wardenalt.Options.Cloud` (hosted verification: the verification service is injected via the `CloudVerifier` interface, with `WebhookVerifier` adapting the contract's "custom Webhook" form; the address goes through `COURIER_REALNAME_VERIFY_WEBHOOK`) | S2S default = fail-closed 401; sink default = no output — `COURIER_REALNAME_AUDIT_WEBHOOK` (+ `COURIER_REALNAME_AUDIT_TOKEN`) enables the direct-write half (best-effort delivery: timeouts / network failures / non-2xx only log and do not block real-name requests; retention is the integrator's storage policy), and `COURIER_REALNAME_AUDIT=log` enables the log half, with both implementations injected from the same source in main.go; warden-alt Cloud default = HealthCheck not ready → 503 (fail-closed, a missing configuration ≠ a capability turned off with 501) |
| cascade | `hooks.Register(hooks.AccountDeleted, fn)` (registered-style per contract auth.md v1.1: cleanup of deactivation data is self-defined by the downstream Provider) | the gateway knows nothing about any downstream storage |

## 5. Credentials and Configuration Injection

Provider secrets are always **injected via environment variables and fail-closed in-process** (`COURIER_PAYMENTS_CHANNEL_SECRET`,
`COURIER_REALNAME_S2S_TOKEN`, etc., see main.go); they never land in configuration files or enter the contract.
The gateway side has its own assembly env vars: `COURIER_GATEWAY_ADDR` (listen address, default `:8080`),
`COURIER_OAUTH_DISABLED=1` (turn the oauth domain off, 501 degradation), `COURIER_GATEWAY_CONFIG` (path to the JSON
routing table, overriding the built-in defaults), `COURIER_REALNAME_AUDIT_WEBHOOK` (+ `COURIER_REALNAME_AUDIT_TOKEN`,
the audit direct-write-half endpoint and credentials), `COURIER_REALNAME_AUDIT=log` (the log-half switch).
Persistence (database writes / reconciliation / audit / object storage) follows the deployment surface — trace propagation from gateway→Provider is built in
([layers.en.md](./layers.en.md) L5), and the in-memory implementation is full functionality for single-machine and test scenarios.

## 6. Replacement Acceptance: The Consistency Test Precedent

The behavioral proof that "a Provider is swappable" = **running the same wire scenario against both implementations with the same assertions**
(`gateway/e2e/identity_conformance_test.go`, batch 18; the isomorphic precedent for the real-name domain is
`gateway/e2e/realname_conformance_test.go`, batch 64 — warden and warden-alt
each run the same chain for adult/minor):

- Assembly is isomorphic with main.go (registry + routing table + frozen middleware chain: trace → structured errors → rate limiting → scope);
- Scenario coverage includes cross-domain validation (after switching the identity source, herald still authenticates new tokens as usual);
- New Provider integrations are advised to ship an isomorphic consistency test; all-green e2e + gateway `go vet` + all-green `go test`
  are the commit gate for this repo (CI uses the same commands, see tests.yml; locally, add `-count=1` to defeat the cache).
