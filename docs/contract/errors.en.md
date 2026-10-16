[English](./errors.en.md) | [中文](./errors.md)

# Error Contract

> Status: Frozen v1 (2026-10-08, frozen at the M0 cross-platform review; the error code table is **v8** — v2 added M2 domain codes, v3 added REALNAME time-window/quota codes, v4 added M5 PAYMENT domain codes, v5 added AUTH deregistration codes, v6 added OAUTH domain codes, v7 added SUPPORT attachment codes (2026-10-12), v8 added SUPPORT report/appeal codes (2026-10-13)). Error consistency across Unity / UE / Cocos / Godot is the core value of the Universal SDK: the same `code` maps to the same enum and the same retry semantics on every platform.

## Error Body

```json
{
  "error": {
    "code": "AUTH_TOKEN_EXPIRED",
    "message": "session expired",
    "retryable": false
  },
  "traceId": "4bf92f3577b34da6a3ce929d0e0e4736"
}
```

- `code`: machine-readable, `<DOMAIN>_<REASON>` in UPPER_SNAKE_CASE; after each platform maps it to an enum, it is the sole basis for branch decisions.
- `message`: human-readable, may vary by language; it **must not** be used for client-side branch decisions.
- `retryable`: when `true`, the client may retry automatically per its backoff strategy; paired with the `Retry-After` header (seconds).

## Domain Prefix Registry

`COMMON` / `AUTH` / `SESSION` / `SCOPE` / `RATE` / `ANNOUNCEMENT` / `SUPPORT` / `ASSISTANT` / `PAYMENT` / `REALNAME` / `CONFIG` / `APP` / `DIAG` / `OAUTH`

A new domain must register its prefix here before defining concrete codes.

## HTTP Status Mapping

| HTTP | Semantics |
| --- | --- |
| 400 | Invalid parameters/format |
| 401 | Unauthenticated (credentials invalid/expired/revoked) |
| 403 | Authenticated but no permission/denied (real-name not passed, account disabled) |
| 404 | Resource not found |
| 409 | Conflict (session/state race) |
| 426 | Version too old, upgrade required |
| 429 | Rate limited |
| 500 | Internal error |
| 501 | Capability not enabled (degradation signal, see below) |
| 503 | Dependency unavailable/under maintenance |

## Error Code Table v1 (the primitives frozen set)

| code | HTTP | retryable | Description |
| --- | --- | --- | --- |
| COMMON_INTERNAL | 500 | true | Internal error |
| COMMON_INVALID_ARGUMENT | 400 | false | Invalid parameters |
| COMMON_UNAUTHENTICATED | 401 | false | Unauthenticated |
| COMMON_PERMISSION_DENIED | 403 | false | Permission denied |
| COMMON_NOT_FOUND | 404 | false | Resource not found |
| COMMON_CAPABILITY_DISABLED | 501 | false | **Capability not integrated/disabled (degradation signal)** |
| COMMON_UNAVAILABLE | 503 | true | Dependency unavailable |
| RATE_LIMITED | 429 | true | Rate limited, honor Retry-After |
| SCOPE_MISMATCH | 400 | false | Scope mismatch (see scope.md) |
| AUTH_INVALID_CREDENTIALS | 401 | false | Invalid credentials |
| AUTH_TOKEN_EXPIRED | 401 | false | access token expired (should refresh) |
| AUTH_TOKEN_REVOKED | 401 | false | token revoked (should sign in again) |
| AUTH_REFRESH_REUSED | 401 | false | refresh token replayed (security event, should sign in again) |
| AUTH_ACCOUNT_DISABLED | 403 | false | Account disabled |
| AUTH_DEVICE_LIMIT | 403 | false | Device count over the limit |
| AUTH_EMAIL_TAKEN | 409 | false | Email already registered (added when M1 auth.md froze) |
| AUTH_ACCOUNT_PENDING_DELETE | 403 | false | Account in the deregistration grace period, sign-in/binding rejected (added when M1 auth.md v1.1 froze) |
| AUTH_DELETE_TOKEN_INVALID | 401 | false | Deregistration cancellation token missing/invalid/expired (added when M1 auth.md v1.1 froze) |
| SESSION_CONFLICT | 409 | false | Session conflict (remote sign-out, etc.) |
| ANNOUNCEMENT_NOT_FOUND | 404 | false | Announcement not found or outside its visible window (added when M2 announcement.md froze) |
| SUPPORT_TICKET_NOT_FOUND | 404 | false | Ticket not found or not owned by the current player (added when M2 support.md froze) |
| SUPPORT_TICKET_CLOSED | 409 | false | Ticket closed, appending messages rejected (added when M2 support.md froze) |
| REALNAME_REQUIRED | 403 | false | Real-name required (not submitted) |
| REALNAME_REJECTED | 403 | false | Real-name verification failed |
| REALNAME_PENDING_REVIEW | 403 | false | Pending review (the safe state when the degradation chain is fully down) |
| REALNAME_CURFEW_BLOCKED | 403 | false | Outside playable hours (added when late-M2 realname.md froze; data may carry nextWindowAt) |
| REALNAME_CHARGE_BLOCKED | 403 | false | Over the top-up quota (added when late-M2 realname.md froze; data may carry quota fields) |
| CONFIG_NOT_FOUND | 404 | false | Config key not found |
| APP_MAINTENANCE | 503 | false | Under maintenance (payload may carry the estimated recovery time, optional) |
| APP_VERSION_UNSUPPORTED | 426 | false | Version too old (payload may carry the download URL, optional) |
| PAYMENT_SKU_NOT_FOUND | 404 | false | SKU not found or not purchasable (added when M5 payment.md froze) |
| PAYMENT_ORDER_NOT_FOUND | 404 | false | Order not found or not owned by the current player (added when M5 payment.md froze) |
| PAYMENT_RISK_REJECTED | 403 | false | Preemptive risk-control rejection: large amount/abnormal frequency (added when M5 payment.md froze) |
| PAYMENT_INVALID_SIGNATURE | 403 | false | Channel callback signature verification failed; forgeries are always rejected (added when M5 payment.md froze) |
| PAYMENT_ORDER_STATE | 409 | false | Illegal order state machine transition (added when M5 payment.md froze) |
| OAUTH_PROVIDER_UNKNOWN | 400 | false | Provider not registered or not enabled (added when oauth.md v1 froze) |
| OAUTH_IDENTITY_INVALID | 401 | false | Third-party credential verification failed/expired/forged; invalid and expired are not distinguished, to prevent enumeration (added when oauth.md v1 froze) |
| OAUTH_IDENTITY_TAKEN | 409 | false | This third-party identity is already bound to another account (added when oauth.md v1 froze) |
| SUPPORT_ATTACHMENT_TOO_LARGE | 413 | false | Attachment exceeds the per-file limit (default 10 MiB; added when support.md v1.1 froze) |
| SUPPORT_ATTACHMENT_LIMIT | 409 | false | Ticket attachment count at the limit (default 3; added when support.md v1.1 froze) |
| SUPPORT_PENALTY_NOT_FOUND | 404 | false | Penalty not found or not owned by the current player; existence not leaked (added when support.md v1.1 F45 froze) |
| SUPPORT_APPEAL_EXISTS | 409 | false | This penalty already has an appeal in progress; at most one per penalty (added when support.md v1.1 F45 froze) |

Business error codes for each domain freeze along with that domain's contract; once a code in this table is frozen, its semantics must not change.

> **Registered reserved codes (currently no trigger points)**: `REALNAME_REQUIRED` / `REALNAME_REJECTED` / `REALNAME_PENDING_REVIEW` / `REALNAME_CURFEW_BLOCKED` / `REALNAME_CHARGE_BLOCKED` / `SESSION_CONFLICT` / `COMMON_PERMISSION_DENIED` / `CONFIG_NOT_FOUND` / `APP_VERSION_UNSUPPORTED` are registered, and the gateway currently has no trigger point for any of them; clients handle them via the unknown-code catch-all branch (per-code retention reasons below).
>
> **Retention reasons (patrol delegated decision, 2026-10-15; closes the former "error-based triggers awaiting a design ruling" item)**: decision-type results always go through a 200 payload, never an error code — verify / curfew / charge-check are **decision endpoints**; returning 403 would turn "check whether a top-up is allowed" into an error path; moreover, the 200 payload (status.state + isMinor + curfew.playable + charge-check.allowed) can express all five real-name states at once, which a single 403 code cannot. Code by code: the five `REALNAME_*` are the decision payloads in [realname.md](./realname.en.md); `SESSION_CONFLICT` belongs to **player-visible login policies** such as remote sign-out (this repo's contract adopts multi-session coexistence: device cap of 5 + unbind revocation, with replay going separately through `AUTH_REFRESH_REUSED`); integrators wanting a single-session policy can simply configure the device cap as 1; `COMMON_PERMISSION_DENIED` — every domain uses a more specific domain code and does not leak existence (tickets/announcements owned by others are uniformly `*_NOT_FOUND`); `CONFIG_NOT_FOUND` — see [config.md](./config.en.md) (reserved for the v1.1 single-key query); `APP_VERSION_UNSUPPORTED` — see [app.md](./app.en.md) (version decisioning goes through forceUpdate). These nine are checked one by one by `TestErrorCodesWiredOrReserved` in `tools/contractgen`: forgetting to wire a new code, or forgetting to remove it from this note after wiring, fails the gate.

> **The messages domain (push channel, M2 [messages.md](./messages.en.md)) has no dedicated prefix**: channel-layer errors all reuse the common codes (disabled `COMMON_CAPABILITY_DISABLED`, unauthenticated `COMMON_UNAUTHENTICATED`, rate limited `RATE_LIMITED`); the business semantics of a pushed event are carried by the event `type` and data, not by an error.

## Degradation Signal: COMMON_CAPABILITY_DISABLED

When a Provider is not integrated or is disabled by configuration, the gateway returns `501 COMMON_CAPABILITY_DISABLED`. This is a **capability being off**, not a fault:

- The SDK maps it to a "capability unavailable" state (query interfaces return empty/not-enabled rather than taking an exception path).
- Based on this, integrators can safely hide the corresponding UI instead of handling an error.
- This is the landing point of the principle "not crippled just because some vendor isn't integrated": a missing Provider = the capability quietly does not exist.

## Client Cross-Platform Mapping (examples)

```csharp
if (error.Code == ErrorCode.TokenExpired) { /* refresh */ }
```

```cpp
if (error.code == ErrorCode::TokenExpired) { /* refresh */ }
```

```typescript
if (error.code === ErrorCode.TokenExpired) { /* refresh */ }
```

Each platform names its enums per its own language convention, with a value set corresponding one-to-one with this document; contract tests guarantee no drift.

---

## Extension Reserve (error code registration stance, note added 2026-10-11)

| Possible extension | Compatibility handling |
| --- | --- |
| Adding an error code | **Compatible** — clients must tolerate unknown codes via the catch-all branch (a hard requirement in [versioning.md](./versioning.en.md)); new codes enter the table when the domain contract freezes |
| Adding a domain prefix | Compatible; a new capability domain = a new prefix, registered in this table |
| Deprecating an error code | The server stops emitting it; the table entry is **kept, not deleted, and its semantics are never reused**; client enums may keep it |
| Changing an existing code's HTTP mapping / retryable / semantics | Breaking = bump the version |
