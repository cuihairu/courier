# Redeem Code Contract (CDK) — Gift-Pack / Redeem Codes

> Status: **Frozen v1** · F62 · 2026-11-01. The domain prefix `CDK` is registered ([errors.md](./errors.md)); this document is the consistency basis between the F62 server (default provider **voucher**) and the SDKs on every platform. Once frozen it is additive-only; breaking changes bump the version (see [versioning.md](./versioning.md)).

## Capabilities and Scope

- Capability domain `cdk` (`providers.CapCdk`), routes `/v1/cdk/*`; default provider **voucher**, swappable and disableable.
- A code is redeemed **once per account** (idempotency key = account + code). A code may be single-use (`maxRedemptions=1`) or limited-redemption (counting); the semantics belong to Provider.
- Code generation, batch creation, and redemption reporting are management-plane / integrator-side live-ops tooling; the gateway exposes only the player-side redemption endpoint.
- Anti-abuse is covered by the existing global rate limiting + failure backoff.

## Endpoint Overview (mounted at `/v1/cdk/*`)

| Endpoint | Auth | Description |
| --- | --- | --- |
| `POST /v1/cdk/redeem` | Bearer | Redeem a code (idempotent per account+code) |

Request body: `{ "code": "..." }`. Success response `data` = `{ "code", "grant", "repeated" }`.

## Data Model

### code (management-plane registered)

| Field | Type | Description |
| --- | --- | --- |
| `value` | string | The code string |
| `grant` | string | Grant descriptor (semantics belong to the integrator) |
| `maxRedemptions` | int | Total allowed redemptions (≥1) |

### redemption result

| Field | Type | Description |
| --- | --- | --- |
| `code` | string | Redeemed code |
| `grant` | string | Grant descriptor echoed back |
| `repeated` | bool | True if this account had already redeemed this code |

## Endpoint Contract

### `POST /v1/cdk/redeem`

- Unknown code → 404 `CDK_NOT_FOUND`.
- Redemption limit reached → 409 `CDK_EXHAUSTED`.
- Repeated same account + code → 200 with `repeated:true`, no second grant.
- The grant hook is injected by Provider construction; a grant failure is a 503 `COMMON_UNAVAILABLE` and the code stays redeemable.

## Push Events

`cdk.redeemed` (payload `{ "code": "..." }`) — a hint; no sensitive data (redemption results are pulled via the redemption response / integrator reporting).

## Error Code Domain Table (prefix `CDK`, errors.md v9)

| code | HTTP | retryable | Description |
| --- | --- | --- | --- |
| `CDK_NOT_FOUND` | 404 | false | Code not found or not registered |
| `CDK_EXHAUSTED` | 409 | false | Code redemption limit reached |

## Client Requirements

- Capability not wired in (501 `COMMON_CAPABILITY_DISABLED`) → redemption returns null; hide the redemption UI.
