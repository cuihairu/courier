
# F31 Overseas Age Gate and Parental Consent: the `consent` Group v1.1 Design Draft

> Status: **Draft (not frozen)** · design started under the delegated decision of the 2026-10-16 patrol FW1016D · freezing still awaits the overseas expansion proposal ([realname.md:100](../contract/realname.en.md) position unchanged).
> Characterization: this draft turns "design from scratch after the overseas decision" into "freeze upon draft review"; jurisdiction-neutral (thresholds are not in the contract, they belong to Provider configuration), and it is not pinned to a single COPPA / GDPR-K shape.
> Research basis: the F31 row ([features.md §5](./features.en.md): first-launch age screening; below the digital consent age go through verifiable parental consent VPC; consent records retained separately).

## Delegated Decision Record (2026-10-16, patrol FW1016D)

- **Decision**: the `consent` group is designed jurisdiction-neutral, draft first; freezing and implementation await the overseas expansion proposal.
- **Reasoning**: F31 is a "pending decision" blocker (freeze only after the overseas requirement is triggered). The established play of the contract-first repo is to front-load design risk — the draft does not change realname v1's frozen state and creates no implementation maintenance surface; once the overseas proposal lands, "draft → freeze review → implementation" is a mechanical step.
- **Overturn path**: the decision-maker can overturn this and void the draft, restoring the original position of "design from scratch after the trigger"; removing this file from docs is all it takes, with no implementation surface entangled.

## Design Principles (inherited from realname v1)

- **Additive-only**: v1's existing endpoints / fields / codes are untouched; the `consent` group is entirely new.
- **Verdicts travel in the 200 payload**: following the 2026-10-15 delegated-decision position (annotated in the error-code section of realname.md) — verdict states go into `data`, not into 403 codes; `DENIED` / `REVOKED` / `PENDING` are states, not error codes.
- **The client must not infer on its own**: both age determination and consent determination are issued server-side (the same principle as v1 `isMinor`).
- **Off by default**: no configured consent channel means the capability-disabled semantics (501 `COMMON_CAPABILITY_DISABLED`); the integrator hides the age gate UI and stays out of the error path.
- **Data red line aligned with v1**: birth date is personal data — transmitted only once via the declaration endpoint, never cached, never persisted, never enters diagnostics collection (same as v1 "Field Masking and the Data Red Line"); the UI shows year only.

## State Machine (consent states)

```text
UNDECLARED ──declare-age──► ADULT(adult, terminal)
     │
     └─declare-age─► PENDING_PARENT(minor, awaiting parental consent)
                          │  parent-consent/request issued, resubmittable once expiresAt passes
                          ├─provider verdict─► GRANTED(consented, playable)
                          │        └─parent withdrawal─► REVOKED(withdrawn, gate re-closed)
                          └─provider verdict─► DENIED(rejected)
```

| state | Meaning | Game-side handling |
| --- | --- | --- |
| `UNDECLARED` | Age not declared | First-launch age gate (no protected operation is allowed through before declaration) |
| `ADULT` | Adult | Terminal state; consent no longer required |
| `PENDING_PARENT` | Minor and consent undecided | Gate closed; a VPC request can be issued / resubmitted, prompted per `requestExpiresAt` |
| `GRANTED` | Parental consent granted | Gate allowed through |
| `DENIED` | Parent rejected | Gate closed; whether a re-request is allowed belongs to Provider policy |
| `REVOKED` | Withdrawn after being granted | Gate closed; the withdrawal is linked to diagnostics being stopped (see "Diagnostics Linkage") |

- **Anti-rollback**: repeated age declarations take effect only in the "stricter" direction (adult → minor is allowed, the reverse is not); corrections go through the Provider operations surface and do not enter the player contract.

## Endpoint Group (new in v1.1, mounted at `/v1/realname/consent/*`)

| Endpoint | Auth | Description |
| --- | --- | --- |
| `GET /v1/realname/consent/status` | Bearer | Consent status query |
| `POST /v1/realname/consent/declare-age` | Bearer | First-launch age declaration `{ birthDate: "YYYY-MM-DD" }` |
| `POST /v1/realname/consent/parent-consent/request` | Bearer | Issue a VPC request `{}` → `{ requestId, requestExpiresAt }` |
| Push event `realname.consent.updated` | SSE | State change notification (reuses the M3 push channel, same shape as `config.updated`) |

- Player-side endpoint authentication and scope requirements are the same as in [auth.md](../contract/auth.en.md); account dimension (same as v1, not the player dimension).
- Parental verification itself is **not in this contract** (belongs to the Provider: credit card verification / signature forms / government ID / enhanced email and other jurisdictional mechanisms); the contract only expresses "request issued + result state + change push".

## Data Model

### status (consent status)

| Field | Type | Description |
| --- | --- | --- |
| `state` | enum | The six states in the table above |
| `declaredAt` | timestamp? | Age declaration time |
| `grantedAt` | timestamp? | Consent granted time (issued for `GRANTED`) |
| `revokedAt` | timestamp? | Withdrawal time (issued for `REVOKED`) |
| `requestExpiresAt` | timestamp? | Expiry point of the current VPC request (issued for `PENDING_PARENT`) |

### declare-age / parent-consent-request

- Requests / responses use the primitives envelope; the `declare-age` response `data` = the status from the table above (declaring yields the verdict immediately).
- `parent-consent/request` is idempotent: repeated issues during `PENDING_PARENT` return the same request (when not expired); after expiry, resubmitting gets a new `requestId`.

## Jurisdiction-Neutral: Thresholds and Rules Belong to Provider Configuration

| Configuration | Description | Relation to the contract |
| --- | --- | --- |
| `minimumAge` | Minimum age to play (below it is always the `DENIED` semantics, expressed as the Provider decides) | Not in the contract |
| `digitalConsentAge` | Digital consent age (13–16 across GDPR-K member states; 13 under COPPA) | Not in the contract |
| `vpc.mechanism / ttl` | Parental verification mechanism and request validity period | Not in the contract |

- Server-side verdict input = `birthDate` + Provider threshold configuration; switching jurisdiction = switching configuration, and the contract and the six-platform SDKs are untouched.

## Provider Interface Extension (additive, v1 methods untouched)

```text
RealNameProvider additions:
  ConsentStatus(accountId) → ConsentState
  DeclareAge(accountId, birthDate) → ConsentState            // anti-rollback: only stricter takes effect
  RequestParentConsent(accountId) → ConsentRequest{ requestId, expiresAt }
```

- Hot-swap / degradation chain / circuit breaker follow v1's general Provider governance rules; the degradation safe state = `PENDING_PARENT` (neither allowed through nor rejected, routed to manual review).

## Audit and Retention (consent records retained separately)

- New audit events: `realname.consent.declared / parent_requested / granted / denied / revoked` — account-dimension metadata only, birth date and parent identity inputs **never enter the event** (same red line as v1); they go through the shared seam `gateway/audit.AuditSink`, with both implementations in the same shape.
- **Consent records and identity data are stored separately with independent retention periods** (so compliance evidence can be provided for each separately); the retention duration belongs to the integrator's compliance position (the same boundary as F33).

## SDK Surface (L2/L4 projection)

```text
RealName.Consent
  .Status()                  → { state, declaredAt?, grantedAt?, revokedAt?, requestExpiresAt? }
  .DeclareAge(birthDate)     → status
  .RequestParentConsent()    → { requestId, requestExpiresAt }
OnConsentChanged callback reserved (consumes the realname.consent.updated push)
```

- 501 = consent not enabled: the query returns "not enabled", the UI hides the age gate, and it stays out of the error path (same as v1).
- On receiving `REVOKED` → the SDK internally triggers the diagnostics stop-send linkage (see below), so the integrator does not need to wire it event by event.

## Diagnostics Linkage (privacy withdrawal)

- The reserved position in [diagnostics.md:117](../contract/diagnostics.en.md): "Consent state linkage (privacy withdrawal) — the host passes in the consent state → stop sending across all classes". This draft lands the server-side trigger source of that linkage: on issuing `REVOKED`, each platform's SDK stops sending diagnostics (the red line "off takes effect immediately" is built in).

## Error Codes

- **Zero new codes**: all verdicts are expressed in the 200 payload (2026-10-15 delegated-decision position). If the freeze review finds that the integrator's game-side policy execution needs codes, add them through the "registered reserved codes" process (errors.md + the `TestErrorCodesWiredOrReserved` gate); they are not pre-registered.

## Freeze Checklist (when the overseas expansion is proposed)

1. Review and finalize this draft → realname.md bumps to v1.1 (new `consent` section, existing sections untouched).
2. Add fixtures (if needed) → `go run ./tools/contractgen -write` regenerates the six platforms.
3. Gateway: warden / wardenalt, both implementations in the same shape (Provider methods + audit events + degradation safe state).
4. Six-platform SDKs (unity / cocos / miniprogram / laybox / ue / godot) + e2e; errors.md bumps only when codes are needed.
