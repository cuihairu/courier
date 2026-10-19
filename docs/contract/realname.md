
# Real Name Contract (RealName)

> Status: **Frozen v1.1** · compliance capability · off by default · late in milestone M2 · v1 frozen 2026-10-08 / v1.1 frozen 2026-10-18 (delegated decision PATROL1018E: the `consent` group, additive-only).
> The domain prefix `REALNAME` is registered ([errors.md](./errors.md)); once frozen it is additive-only, and breaking changes bump the version (see [versioning.md](./versioning.md)).
> Red line: real-name data is the highest-sensitivity personal data. This capability is off by default; integrating it counts as the integrator collecting real-name information from players, and **the obligation to disclose the data scope lies with the integrator** — the data scope table in this document is the single source of truth for the external wording.

## Capabilities and Scope

- Capability domain `realname` (`providers.CapRealname`), routed at `/v1/realname/*`; **off by default** — with no Provider configured it shares the degradation signal of other capabilities (`501 COMMON_CAPABILITY_DISABLED`), and the integrator uses it to hide the real-name UI and stay out of the error path.
- Enabling requires both a Provider configuration and the disclosure wording; missing either is treated as off.
- This contract **does not cover**: the verification execution itself (belongs to the Provider: self-built library / Alibaba Cloud / Tencent Cloud Huiyan / NetEase Yidun / Webhook), or anti-addiction policy enforcement (the **policy and configuration** of time windows / spending limits belong to the integrator; this contract only provides **query and validation endpoints**).
- Research basis: the must-have chain F27–F30 (see [features.md §14](../research/features.md)) — real-name verification (F27), time-window query (F28), spending-limit validation (F29), S2S reporting (F30).

## Client Interface (L2/L4 projection)

```text
RealName
  .Submit({ name, idNumber })   submit verification → states below
  .Status()                     → { state, isMinor?, verifiedAt? }
  .Curfew()                     → { playable, nextWindowAt? }
  .ChargeCheck(amountCents)     → { allowed, singleLimitCents?, monthlyLimitCents?, monthlyUsedCents? }
```

- `state`: `UNVERIFIED | PENDING_REVIEW | VERIFIED | REJECTED`
- `isMinor`: the verdict is issued by the server only after `VERIFIED`; the client must not infer it on its own.
- The submit endpoint requires an authenticated session (attached at the account dimension, not the player dimension).
- The masking rule lands in the UI: keep the first character of the name and mask the rest with `*` (张三 → 张\*); for the ID number keep the first 3 and last 4, masking the middle with `*` (110101199001011234 → 110\*\*\*\*\*\*\*\*\*\*\*\*1234). The SDK provides the pure function `RealNameMask` (implemented in the service package, testable in dotnet; shared by the UI panel and integrators), and integrators must not invent their own masking rule.

## Endpoint Overview (mounted at `/v1/realname/*`)

| Endpoint | Auth | Description | Research basis |
| --- | --- | --- | --- |
| `POST /v1/realname/verify` | Bearer | Submit real-name verification | F27 |
| `GET /v1/realname/status` | Bearer | Query real-name status | F27 |
| `GET /v1/realname/curfew` | Bearer | Playable time-window query (minor play windows) | F28 |
| `POST /v1/realname/charge-check` | Bearer | Spending-limit validation | F29 |
| `POST /v1/realname/playtime-report` | **S2S** | Anti-addiction data reporting (server to server) | F30 |

- Player-side endpoint authentication and scope-header requirements are as in [auth.md](./auth.md); missing → the corresponding common code.
- `playtime-report` is an **S2S endpoint**: authentication uses the integrator's server credentials (not the player's Bearer); the SDK must not call it — client-side reporting would double-count, so reporting is consolidated and submitted by the integrator's server.
- Successful responses are HTTP 200; for the envelope see [primitives.md](./primitives.md).

## Data Model

### status (real-name status)

| Field | Type | Description |
| --- | --- | --- |
| `state` | enum | `UNVERIFIED` / `PENDING_REVIEW` / `VERIFIED` / `REJECTED` |
| `isMinor` | bool? | Issued only after `VERIFIED`; omitted in the other states |
| `verifiedAt` | timestamp? | Verification pass time |

### curfew (playable time window, F28)

| Field | Type | Description |
| --- | --- | --- |
| `playable` | bool | Currently playable |
| `nextWindowAt` | timestamp? | Start of the next playable window (issued when not playable; omitted when playable) |

- Decision input = `status.isMinor` + the Provider's time-window configuration; adults always get `playable = true`.
- The policy configuration (which days, which windows) belongs to the Provider and is outside the contract — the contract only guarantees query semantics.

### charge-check (F29)

Request `{ "amountCents": int }` (the amount of this charge, in cents).

Response `data`:

| Field | Type | Description |
| --- | --- | --- |
| `allowed` | bool | Whether this charge is allowed through |
| `singleLimitCents` | int? | Per-transaction cap |
| `monthlyLimitCents` | int? | Monthly cap |
| `monthlyUsedCents` | int? | Monthly used |

- `allowed = false` → integrator guides the player; the payment domain (M5) calls this endpoint before order placement for pre-validation.

### playtime-report (F30, S2S)

Request `{ "accountId": "acc_...", "date": "YYYY-MM-DD", "playMinutes": int, "chargeAmountCents": int }` (bare object, outside the envelope); response `data: {}`.

- Server-side S2S reporting is preferred, to avoid duplicate client-side reporting; the idempotency key = `accountId + date` (same-day re-reports take the latest).

## Field Masking and the Data Red Line

| Stage | Rule |
| --- | --- |
| Transport | HTTPS only (TLS 1.2+); the SDK does not apply a second layer of encryption — envelope encryption belongs to the Provider implementation |
| SDK side | **Never persisted**: not into secure storage, not into logs, not into breadcrumb / trace / diagnostics collection |
| UI display | Always masked (name first character kept with the rest `*`, ID number first 3 / last 4; see the rule in the "Client Interface" section, unified by the UI-package function) |
| Server side | Encrypted storage; logs and traces forbid plaintext; retention follows the integrator's compliance requirement, taking the shortest necessary period |
| Diagnostics | The request/response bodies of real-name endpoints **never enter** any diagnostics collection (see the red line in [diagnostics.md](./diagnostics.md)) |

Collected field list (complete): `name`, `idNumber`, verification result, adult/minor verdict, time-window / spending-limit decision output. Nothing else is collected.

## Anti-Addiction Hooks (interface slots)

- `Status().isMinor` and `Curfew().playable` are the only two decision outputs; policy execution is not in this contract (the integrator's game side, or a future standalone anti-addiction module).
- Reserved: the `OnRealNameVerified` client callback; the server-side Provider result keeps the `policyExtension` extension slot, and unknown fields must currently be ignored.
- Frozen in v1.1: the overseas age gate / parental consent `consent` group — see the "Consent Management (consent, v1.1)" section below (research F31; frozen 2026-10-18 by delegated decision PATROL1018E, additive-only; the design history is in [research/consent-v1.1-draft.md](../research/consent-v1.1-draft.md)).

## Consent Management (consent, v1.1)

> Added in v1.1, additive-only: the existing `verify` / `status` / `curfew` / `charge-check` / `playtime-report` endpoints and fields are untouched. Frozen 2026-10-18 by patrol PATROL1018E delegated decision (design draft: [research/consent-v1.1-draft.md](../research/consent-v1.1-draft.md)). **Frozen ≠ implemented**: the gateway and the six-SDK implementation surface awaits a go-ahead call (F31, registered in [todo.md](../todo.md)).

- **Off by default**: with no consent channel configured, the capability-disabled semantics apply (`501 COMMON_CAPABILITY_DISABLED`) — queries return "not enabled", the integrator hides the age-gate UI, and **no error path is entered** (same as v1).
- **Verdicts ride the 200 payload**: consent state goes in `data`, not in error codes; **zero new error codes**.
- **The client must not infer on its own**: both the age verdict and the consent verdict are server-issued (same principle as v1 `isMinor`).
- **Data red lines align with v1**: the birth date travels only once through the declare endpoint — never cached, never persisted, never sent to diagnostics collection; UI display shows the year only.

### State machine (consent state)

| state | Semantics | Game-side handling |
| --- | --- | --- |
| `UNDECLARED` | Age not declared | First-launch age gate (protected operations are not released until declared) |
| `ADULT` | Adult | Terminal state; no consent is ever required |
| `PENDING_PARENT` | Minor and consent undecided | Gate closed; VPC requests may be issued / reissued, prompt per `requestExpiresAt` |
| `GRANTED` | Parental consent established | Gate opens |
| `DENIED` | Parent refused | Gate closed; whether re-requesting is allowed is Provider policy |
| `REVOKED` | Consent later withdrawn | Gate closed; withdrawal stops diagnostics (see "Diagnostics linkage") |

- **No rollback**: repeated age declarations only ever tighten (adult → minor allowed, never the reverse); corrections go through the Provider ops surface, not the player contract.

### Endpoints (mounted at `/v1/realname/consent/*`)

| Endpoint | Auth | Notes |
| --- | --- | --- |
| `GET /v1/realname/consent/status` | Bearer | Consent status query |
| `POST /v1/realname/consent/declare-age` | Bearer | First-launch age declaration `{ birthDate: "YYYY-MM-DD" }`; response `data` = the status table below (declaration immediately yields the verdict) |
| `POST /v1/realname/consent/parent-consent/request` | Bearer | Initiate a VPC request `{}` → `{ requestId, requestExpiresAt }`; idempotent while `PENDING_PARENT` (same request returned before expiry, new `requestId` after) |
| Push event `realname.consent.updated` | SSE | State-change notification (reuses the M3 push channel, same shape as `config.updated`) |

- Player-side endpoint auth and scope requirements follow [auth.md](./auth.md); account-dimension (same as v1, not player-dimension).
- Parental verification itself is **not in this contract** (Provider territory: credit-card verification / signed forms / government ID / email step-up and other jurisdictional mechanisms); this contract only expresses "request initiation + resulting state + change push".

### Data model: status (consent status)

| Field | Type | Notes |
| --- | --- | --- |
| `state` | enum | The six states above |
| `declaredAt` | timestamp? | Age declaration time |
| `grantedAt` | timestamp? | Consent establishment time (sent under `GRANTED`) |
| `revokedAt` | timestamp? | Withdrawal time (sent under `REVOKED`) |
| `requestExpiresAt` | timestamp? | Expiry of the current VPC request (sent under `PENDING_PARENT`) |

Requests / responses use the primitives envelope ([primitives.md](./primitives.md)).

### Jurisdiction-neutral: thresholds and rules belong to Provider config

| Config | Notes | Contract relation |
| --- | --- | --- |
| `minimumAge` | Minimum play age (below it, always `DENIED` semantics; expression decided by the Provider) | Not in the contract |
| `digitalConsentAge` | Digital consent age (GDPR-K member states 13–16; COPPA 13) | Not in the contract |
| `vpc.mechanism / ttl` | Parental-verification mechanism and request validity period | Not in the contract |

- The verdict input = `birthDate` + Provider threshold config; switching jurisdictions = switching config, with the contract and the six SDKs untouched.

### Provider interface extension (additive; v1 methods untouched)

```text
RealNameProvider adds:
  ConsentStatus(accountId) → ConsentState
  DeclareAge(accountId, birthDate) → ConsentState        // no rollback: only tightening takes effect
  RequestParentConsent(accountId) → ConsentRequest{ requestId, expiresAt }
```

- Hot-swap / fallback chain / circuit breaking follow the v1 general Provider governance rules; the degraded safe state = `PENDING_PARENT` (neither releases nor refuses; escalate to a human).
- Audit: new events `realname.consent.declared / parent_requested / granted / denied / revoked` carry account-dimension metadata only; birth dates and parental identity inputs **never enter any event** (same red line as v1); they go through the shared seam `gateway/audit.AuditSink`, same shape for both implementations.
- Consent records are **stored separately from identity data, with an independent retention period** (compliance can evidence each separately); retention days belong to the integrator's compliance stance (same boundary as F33).

### SDK surface (L2/L4 projection)

```text
RealName.Consent
  .Status()                  → { state, declaredAt?, grantedAt?, revokedAt?, requestExpiresAt? }
  .DeclareAge(birthDate)     → status
  .RequestParentConsent()    → { requestId, requestExpiresAt }
OnConsentChanged callback reserved (consumes the realname.consent.updated push)
```

- On `REVOKED` → the SDK internally triggers the diagnostics stop-sending linkage ([diagnostics.md](./diagnostics.md) reserved slot), no per-event wiring by the integrator; the "off takes effect immediately" red line is built in.

### Error codes

**Zero additions** — all verdicts ride the 200 payload (the 2026-10-15 delegated stance). If, during implementation review, an integrator's game-side policy execution genuinely needs codes, add them via the "registered reserved codes" process ([errors.md](./errors.md) + the `TestErrorCodesWiredOrReserved` gate); none are pre-registered.

## Provider Hot-Swap (server side; first complete application of the governance)

Unified interface `RealNameProvider`:

```text
Verify(accountId, identity) → Result      Query(accountId) → Status
Curfew(accountId, now) → Curfew           ChargeCheck(accountId, amountCents) → ChargeDecision
HealthCheck() → bool
```

**Built-in adapters (implemented as needed):**

| Provider | Description |
| --- | --- |
| Self-built verification | Local library comparison; data never leaves the self-built environment (**first implementation**) |
| Alibaba Cloud real-person verification | Cloud API adapter (as needed) |
| Tencent Cloud Huiyan | Cloud API adapter (as needed) |
| NetEase Yidun | Cloud API adapter (as needed) |
| Custom Webhook | Generic HTTP adapter for the integrator's own verification service |

**Hot-switching:**

- Configuration-driven routing table: `primary` + `fallbacks[]`; runtime configuration reload takes effect immediately, **without a restart**.
- The routing table is replaced atomically; in-flight requests complete against the old table, and new requests go to the new table.

**Degradation chain and circuit breaker:**

- Primary Provider failure / timeout → fallbacks are tried in order; if all fail → return `PENDING_REVIEW` (safe state: neither pass nor reject for release; route to manual review / delayed re-verification).
- Circuit breaker: N consecutive failures (default 5) trip it open, and after opening, **30 calls** get half-open probe passage (the implementation is call-count driven, with no timer); while the breaker is open, traffic goes straight to the fallback.
- A business-level "no match" (verification result `REJECTED`) is a **valid result** and does not count as a circuit-breaker failure; the breaker only counts call failures (network / timeout / 5xx).
- HealthCheck lets the routing table drop unhealthy instances.
- Audit trail: key actions (verify / curfew / charge_check / playtime_report) already have a structured audit-event surface, with the sink as an integrator injection point (default = no output; built-in log half and HTTP direct-write half, see provider-guide §4); events contain only account-dimension metadata — identity inputs never enter events; the retention policy and query surface belong to the integrator's storage implementation (the contract surface is unchanged). The seam lives in the shared package `gateway/audit`, and both implementations (warden / warden-alt) inject the same shape — switching Providers does not change the audit surface.

The hot-swap / degradation / circuit-breaker rules above are the general Provider governance rules (see "Provider Governance" in [architecture.md](../architecture.md)); RealName is their first complete application.

## Error Codes (domain table)

| code | HTTP | retryable | Description |
| --- | --- | --- | --- |
| `REALNAME_REQUIRED` | 403 | false | Real name required (not yet submitted) |
| `REALNAME_REJECTED` | 403 | false | Real-name verification not passed |
| `REALNAME_PENDING_REVIEW` | 403 | false | Pending re-verification (the safe state when the whole degradation chain is down) |
| `REALNAME_CURFEW_BLOCKED` | 403 | false | In a non-playable time window (F28; `data` may carry `nextWindowAt`) |
| `REALNAME_CHARGE_BLOCKED` | 403 | false | Spending limit exceeded (F29; `data` may carry the limit fields) |

> **The codes in this table are currently registered reserved codes, and the gateway does not emit them** (2026-10-15 patrol delegated decision; the decision payload is the single source of truth):
> Real-name determinations travel in the 200 decision-state payload — `status.state` (`UNVERIFIED`/`PENDING_REVIEW`/
> `VERIFIED`/`REJECTED`) + `isMinor` + `curfew.playable` + `charge-check.allowed`.
> `verify` / `curfew` / `charge-check` are decision endpoints; returning 403 from them would turn "check whether I can charge"
> into an error path; the payload can express all five states at once, which a single 403 code cannot. The codes above
> are retained, not deleted, and integrators may still use them when enforcing anti-addiction policy on the game side
> (see "Policy execution is not in this contract" below); the whole table is covered by the "registered reserved codes"
> annotation in [errors.md](./errors.md), checked by the `TestErrorCodesWiredOrReserved` gate.

Everything else (authentication, scope, rate limiting, capability disabled) uses common codes; see [errors.md](./errors.md).

## Client Requirements (SDKs on each platform)

- `501 COMMON_CAPABILITY_DISABLED` = real name not enabled: the query endpoint returns "not enabled", the UI hides the entry, and it **stays out of the error path**.
- `PENDING_REVIEW` is a safe state: no retry guidance, no release of protected operations; show "under review" — whether to route to manual handling is the integrator's decision.
- `REALNAME_REQUIRED` → guide the player into the real-name flow; `REALNAME_CURFEW_BLOCKED` / `REALNAME_CHARGE_BLOCKED` → display per the `data` fields, without inventing your own copy wording.
  (The gateway currently emits none of these three codes — the same determination lives in the 200 payload: `state=UNVERIFIED` → guide into real name;
  `curfew.playable=false` / `charge-check.allowed=false` → display per the payload's `nextWindowAt` and
  limit fields. When the integrator enforces policy on the game side and switches to error codes, the client mapping is already in place — handle it accordingly.)
- The name and ID number are **transmitted only once, via `Submit`**: no caching, no replay, no printing; UI display always goes through `RealNameMask`.

## Extension Reservations (addendum, 2026-10-11)

| Possible extension | Compatible handling |
| --- | --- |
| Overseas age gate / parental consent (F31) | The `consent` endpoint group is frozen in v1.1 (2026-10-18 delegated decision PATROL1018E; see the "Consent Management (consent, v1.1)" section); existing fields and endpoints are untouched, the implementation surface awaits a go-ahead call |
| Provider result extension | The `policyExtension` extension slot is reserved; unknown fields must be ignored; new policy fields = additive compatibility |
| Verification-passed callback | The `OnRealNameVerified` client callback is reserved; refining the trigger timing = an add-parameter proposal |
| Third-party verification ecosystem | The `RealNameProvider` interface is pluggable + routing-table hot-switching is frozen; onboarding a new vendor = a new adapter, the contract is unchanged |
| Time-window / limit rule adjustments | Management-plane configuration (curfew windows, age-tiered limits); the contract does not freeze concrete values, and the red line (off by default, explicit enable) is not loosened by extensions |
