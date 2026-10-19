
# Authentication Contract (Auth / Identity)

> Status: **Frozen v1.1** · Milestone M1 (v1 frozen 2026-10-08; v1.1 2026-10-12 adds account deletion and data erasure, F03).
> The domain prefix `AUTH` is registered ([errors.md](./errors.md)); this document is the consistency basis between the M1 server (the AccountProvider self-built implementation) and the SDKs on every platform. Once frozen it is additive-only; breaking changes bump the version (see [versioning.md](./versioning.md)).

## Capabilities and Scope

- Auth methods: **email + password**, **guest (device)**.
- An account (`account`) is a **company-level entity** that exists across games; when a session is issued it is bound to the scope `game_id + env` ([scope.md](./scope.md)) — the same account logs in separately in each game, and a token cannot be used across games.
- This contract **does not cover**: player profile (M3), real name (realname.md), third-party OAuth / phone number (later domain contract extensions).

## Endpoint Overview (mounted at `/v1/identity/*`)

| Endpoint | Auth | Description |
| --- | --- | --- |
| `POST /v1/identity/register` | anonymous | Register with email + password and log in |
| `POST /v1/identity/login` | anonymous | Log in with email + password |
| `POST /v1/identity/guest` | anonymous | Guest login (idempotent per device: the same device returns to the same guest account) |
| `POST /v1/identity/bind` | Bearer | Bind an email to a guest account (upgrade); the M1 flow "bind email" |
| `POST /v1/identity/refresh` | anonymous | refresh token rotation |
| `POST /v1/identity/logout` | Bearer | Revoke the current session |
| `GET /v1/identity/session` | Bearer | Current session info |
| `GET /v1/identity/devices` | Bearer | List of bound devices |
| `DELETE /v1/identity/devices/{deviceId}` | Bearer | Unbind a device and revoke all of its sessions |
| `POST /v1/identity/account/delete` | Bearer | Request deletion: EMAIL enters a grace period, GUEST is deleted immediately (idempotent acceptance; acceptance revokes all sessions) |
| `POST /v1/identity/account/delete/cancel` | anonymous | Cancel deletion within the grace period (with a one-time `cancelToken`) |

- Auth column `Bearer` = request header `Authorization: Bearer <accessToken>`; missing/malformed → `COMMON_UNAUTHENTICATED` (401).
- Every request must carry the scope headers (`X-Courier-Game-Id` / `X-Courier-Env`, see [scope.md](./scope.md)); missing → `COMMON_INVALID_ARGUMENT`.
- Successful responses are always HTTP 200; for the envelope see [primitives.md](./primitives.md).

## Data Model

Four tables; the fields are the contract's wording (implementations choose their own storage, column names may differ, semantics must not drift); the DDL appendix is at the end of this document.

### accounts (accounts, company-level)

| Field | Type | Description |
| --- | --- | --- |
| `id` | id | Server-generated, UUIDv7, prefix `acc_` |
| `type` | enum | `EMAIL` / `GUEST` / `OAUTH` (`OAUTH` is a v1.1 addition, frozen together with [oauth.md](./oauth.md) v1; accounts created by pure third-party login have no credential row and behave like `GUEST`'s minimal credential form) |
| `email` | email? | Required and globally unique for `EMAIL` accounts; empty for `GUEST` / `OAUTH` |
| `status` | enum | `ACTIVE` / `DISABLED` / `PENDING_DELETE` / `DELETED` (the last two are v1.1); `DISABLED` rejects both login and already-issued sessions (`AUTH_ACCOUNT_DISABLED`); `PENDING_DELETE` rejects login/binding (`AUTH_ACCOUNT_PENDING_DELETE`); `DELETED` rows are anonymized and can no longer authenticate |
| `createdAt` / `updatedAt` | timestamp | RFC 3339 milliseconds UTC |

### account_credentials (credentials, 1:1 with accounts)

| Field | Description |
| --- | --- |
| `accountId` | → accounts.id |
| `passwordHash` / `salt` | PBKDF2-HMAC-SHA256 storage; **plaintext forbidden, reversible forbidden** |
| `algo` / `iterations` | Algorithm identifier and iteration count (e.g. `PBKDF2-SHA256` / `210000`) |
| `updatedAt` | Time of the password change |

`GUEST` accounts have no credential row; one is written after a successful `bind`. `OAUTH` accounts likewise have no credential row (the identity mapping lives in [oauth.md](./oauth.md) `account_identities`, not in this table).

### sessions (sessions, scope dimension)

| Field | Description |
| --- | --- |
| `id` | UUIDv7, prefix `ses_` |
| `accountId` | → accounts.id |
| `gameId` / `env` | The scope bound at issuance (the single source of truth; downstream must not read it from path/payload) |
| `deviceId`? | Bound device (see devices) |
| `accessTokenHash` / `refreshTokenHash` | SHA-256 only, never the original |
| `previousRefreshTokenHash` | The previous generation's refresh hash, used for **replay detection** |
| `accessExpiresAt` / `refreshExpiresAt` | access defaults to 15 minutes and refresh to 30 days (configurable by integrators; the contract does not freeze concrete values) |
| `revokedAt`? | Revocation time; non-empty means reject (`AUTH_TOKEN_REVOKED`) |

### devices (device bindings, account dimension)

| Field | Description |
| --- | --- |
| `id` | UUIDv7, prefix `dev_` (server-side row ID, distinct from the client `deviceId`) |
| `accountId` / `deviceId` | `(accountId, deviceId)` unique; `deviceId` is an opaque identifier supplied by the client (1–128 characters) |
| `platform`? | Self-reported by the client (`ios` / `android` / `windows` / …; the enum is not validated) |
| `createdAt` / `lastSeenAt` | Binding and most recent activity |

- The default device cap is **5** per account (configurable by integrators); binding a new device once the cap is reached → `AUTH_DEVICE_LIMIT` (403).
- Unbinding (`DELETE /devices/{deviceId}`) deletes the binding row and **revokes all sessions of that device**; after unbinding, the same device can bind again (a new row is generated).
- Guest accounts are idempotent per `deviceId`: if an account with `type=GUEST` bound to that device exists, it logs in directly; otherwise a new guest account is created and bound.

## Token Model

- **Opaque random tokens** (64 hex digits, generated server-side with `crypto/rand`), no JWT; the gateway verifies by querying the `sessions` table. JWT and signature verification belong to later domain contracts (if needed); this contract does not introduce them.
- `accessToken`: short-lived (default 15 min); `refreshToken`: long-lived (default 30 days).
- **Rotation**: every successful `refresh` issues a brand-new access+refresh; the old access **expires immediately**, and the old refresh is recorded in `previousRefreshTokenHash`.
- **Replay detection**: an already-rotated refresh appearing again (`== previousRefreshTokenHash`) is treated as a leaked token → `AUTH_REFRESH_REUSED` (401) and **the entire session is revoked**.
- **scope binding**: at token verification, if the session's `gameId+env` does not match the request scope → `SCOPE_MISMATCH` (400, see [scope.md](./scope.md)).

## Endpoint Contracts

### POST /register — `{ email, password, deviceId?, platform? }`

- Validation: email format, password 8–128 characters, deviceId 1–128 characters → violation gives `COMMON_INVALID_ARGUMENT` (400).
- Email already registered → `AUTH_EMAIL_TAKEN` (409).
- Success: create the `EMAIL` account + credentials + (optional) device binding + session, and issue tokens.

### POST /login — `{ email, password, deviceId?, platform? }`

- Account does not exist or password does not match → `AUTH_INVALID_CREDENTIALS` (401); **both cases return exactly the same** (prevents account enumeration).
- Account `DISABLED` → `AUTH_ACCOUNT_DISABLED` (403).
- Account `PENDING_DELETE` (within the deletion grace period) → `AUTH_ACCOUNT_PENDING_DELETE` (403; v1.1) — prevents "logging in resurrects the account"; cancellation goes through `cancelToken` and does not rely on a session.
- New device and the cap is already reached → `AUTH_DEVICE_LIMIT` (403).
- Success: issue a session (auto-bound when deviceId is bound).

### POST /guest — `{ deviceId, platform? }`

- `deviceId` is required.
- Success: issue a session idempotently per device (a `GUEST` account); the response has `account.type = "GUEST"`.

### POST /bind — `{ email, password }` (Bearer)

- The current account must be `GUEST`; already `EMAIL` → `COMMON_INVALID_ARGUMENT` (400).
- Account `PENDING_DELETE` → `AUTH_ACCOUNT_PENDING_DELETE` (403; v1.1).
- Email already registered by another account → `AUTH_EMAIL_TAKEN` (409).
- Success: the account becomes `EMAIL` and credentials are written; **already-bound devices and in-flight sessions are kept**, tokens are not reissued.

### POST /refresh — `{ refreshToken }`

| Case | Result |
| --- | --- |
| `refreshToken` matches the current generation and is not expired | Rotate: new access+refresh, the old generation is recorded as previous |
| Matches the current generation but the refresh has expired | `AUTH_TOKEN_EXPIRED` (401; the client should log in again) |
| Matches the previous generation (already rotated) | `AUTH_REFRESH_REUSED` (401) + revoke the entire session |
| Matches neither / session already revoked | `AUTH_INVALID_CREDENTIALS` / `AUTH_TOKEN_REVOKED` (401) |

### POST /logout (Bearer)

Revokes the current session; idempotent (revoking an already-revoked session still returns 200). Success is `data: {}`.

### GET /session (Bearer)

```json
{ "data": { "account": { }, "sessionId": "ses_…", "deviceId": "…", "accessExpiresAt": "…" } }
```

### GET /devices (Bearer)

`data.items[]`: `{ id, deviceId, platform?, createdAt }`; no `nextCursor` means the last page ([primitives.md](./primitives.md)).

### DELETE /devices/{deviceId} (Bearer)

Unbinds and revokes all sessions of that device (including the current session, if the current session is using that device); device not found → `COMMON_NOT_FOUND` (404).

### POST /account/delete (Bearer, v1.1)

Requests deletion of the current account. The request body is empty or `{}` (no fields).

| Case | Result |
| --- | --- |
| EMAIL account, grace period > 0 | Enters `PENDING_DELETE`; **acceptance revokes all sessions**; returns `cancelToken` |
| EMAIL account, grace period = 0 (admin-side configuration) | Anonymized immediately, `status=DELETED` |
| GUEST account | Anonymized and deleted immediately (no recovery value, no grace period) |
| Already `PENDING_DELETE` and accepted again | Idempotent: 200 and **re-issues** `cancelToken`, the old token expires immediately |

Success `data` (entering the grace period):

```json
{ "status": "PENDING_DELETE", "cancelToken": "…", "pendingDelete": true }
```

Success `data` (immediate deletion; `deletedAt` exists only in this form):

```json
{ "status": "DELETED", "deletedAt": "2026-10-12T00:00:00.000Z" }
```

- `cancelToken`: one-time, TTL = the grace period; the server stores only a hash.
- **Execution semantics = anonymization, not physical row deletion**: clear PII (email, credentials, device binding rows); `accounts.id` is retained for foreign keys and auditing; once the grace period ends, `PENDING_DELETE` finalizes to `DELETED` (the trigger timing is a server implementation detail the contract does not constrain).
- The default grace period is **14 days**, configurable on the admin side from 0–30 days; only EMAIL can have a grace period.
- The player side cannot query the deletion status (all sessions have been revoked); the only interactive path is cancel.

### POST /account/delete/cancel (anonymous, v1.1)

Cancels the deletion within the grace period. Request body `{ "cancelToken": "…" }`.

- Token missing / not matching / already used / expired → `AUTH_DELETE_TOKEN_INVALID` (401).
- Success `data: {}`: the account returns to `ACTIVE`; **revoked sessions are not restored**, a new login is required.
- After cancelling, deletion can be requested again, going through the acceptance flow once more (a new `cancelToken`).

## Error Codes (domain table)

References the frozen codes in [errors.md](./errors.md): `COMMON_INVALID_ARGUMENT` / `COMMON_UNAUTHENTICATED` / `COMMON_NOT_FOUND` / `RATE_LIMITED` / `SCOPE_MISMATCH` / `AUTH_INVALID_CREDENTIALS` / `AUTH_TOKEN_EXPIRED` / `AUTH_TOKEN_REVOKED` / `AUTH_REFRESH_REUSED` / `AUTH_ACCOUNT_DISABLED` / `AUTH_DEVICE_LIMIT` / `AUTH_ACCOUNT_PENDING_DELETE` / `AUTH_DELETE_TOKEN_INVALID`.

Newly frozen by this contract:

| code | HTTP | retryable | Description |
| --- | --- | --- | --- |
| `AUTH_EMAIL_TAKEN` | 409 | false | Email already registered |
| `AUTH_ACCOUNT_PENDING_DELETE` | 403 | false | Account is within the deletion grace period; login/binding rejected (added in v1.1) |
| `AUTH_DELETE_TOKEN_INVALID` | 401 | false | Deletion-cancellation token missing/invalid/expired (added in v1.1) |

## Basic Rate Limiting

- Sensitive endpoints (`register` / `login` / `guest` / `bind` / `refresh`) are rate-limited independently **by client IP**: default **10 requests/minute, bucket capacity 10** (configurable by integrators); exceeding → `RATE_LIMITED` (429) + `Retry-After`.
- The remaining endpoints use the gateway's global rate limiting; coordinated rate limiting across multiple instances follows later M1 engineering.

## Client Requirements (SDKs on each platform)

- Token storage goes through the Adapter layer's secure storage (ITokenStore); **plaintext on disk is forbidden** (see layers.md L3).
- On receiving `AUTH_TOKEN_EXPIRED` → automatically refresh once and replay the original request; `AUTH_REFRESH_REUSED` / `AUTH_TOKEN_REVOKED` / `AUTH_INVALID_CREDENTIALS` → clear the local session and return to the unauthenticated state.
- `COMMON_CAPABILITY_DISABLED` (identity domain) → the SDK reports "account capability not enabled" and does not enter the error path.

## Appendix: Reference DDL (SQLite dialect; implementations may choose their own storage)

```sql
CREATE TABLE accounts (
  id          TEXT PRIMARY KEY,            -- acc_<uuidv7>
  type        TEXT NOT NULL,               -- EMAIL | GUEST | OAUTH(v1.1)
  email       TEXT UNIQUE,                 -- EMAIL 必填全局唯一;GUEST/OAUTH 为 NULL
  status      TEXT NOT NULL DEFAULT 'ACTIVE',
  created_at  TEXT NOT NULL,               -- RFC3339 ms UTC
  updated_at  TEXT NOT NULL
);

CREATE TABLE account_credentials (
  account_id  TEXT PRIMARY KEY REFERENCES accounts(id),
  password_hash BLOB NOT NULL,             -- PBKDF2-HMAC-SHA256
  salt        BLOB NOT NULL,
  algo        TEXT NOT NULL,
  iterations  INTEGER NOT NULL,
  updated_at  TEXT NOT NULL
);

CREATE TABLE sessions (
  id            TEXT PRIMARY KEY,          -- ses_<uuidv7>
  account_id    TEXT NOT NULL REFERENCES accounts(id),
  game_id       TEXT NOT NULL,
  env           TEXT NOT NULL,
  device_id     TEXT,                      -- → devices.device_id(可空)
  access_token_hash  BLOB NOT NULL,
  refresh_token_hash BLOB NOT NULL,
  previous_refresh_token_hash BLOB,        -- 重放检测
  access_expires_at  TEXT NOT NULL,
  refresh_expires_at TEXT NOT NULL,
  created_at    TEXT NOT NULL,
  revoked_at    TEXT
);

CREATE TABLE devices (
  id          TEXT PRIMARY KEY,            -- dev_<uuidv7>
  account_id  TEXT NOT NULL REFERENCES accounts(id),
  device_id   TEXT NOT NULL,
  platform    TEXT,
  created_at  TEXT NOT NULL,
  last_seen_at TEXT,
  UNIQUE (account_id, device_id)
);
```

---

## v1.1 Freeze Record (2026-10-12)

F03 account deletion and data erasure ([features.md](../research/features.md) F03, a hard gate for store policies) was frozen as proposed on 2026-10-11: the endpoints `POST /account/delete` + `POST /account/delete/cancel` (see "Endpoint Contracts" above), acceptance revoking all sessions, a default grace period of 14 days (0–30 configurable), the anonymization execution semantics, and the error codes `AUTH_ACCOUNT_PENDING_DELETE` / `AUTH_DELETE_TOKEN_INVALID` added to [errors.md](./errors.md) v5. The original proposal's "Extensions and Evolution" table takes effect with the freeze:

| Possible extension | Compatible handling |
| --- | --- |
| Grace period length / whether to delete immediately | Admin-side configuration; the player contract is unchanged |
| Scope of downstream data cascading (each game's backend data differs) | The Provider hook `OnAccountDeleted(accountId)` is **registration-based** — the gateway knows nothing about any downstream storage, and each Provider subscribes to clean up on its own; onboarding a new Provider = registering the hook, and the contract is unchanged |
| Third-party OAuth accounts (not covered by v1 of this contract) | The delete endpoints are independent of `type`; the OAuth domain contract ([oauth.md](./oauth.md), v1 already frozen) has already added the `OAUTH` enum value to `type`, so delete/cancel is reused directly |
| Regulatory requirement to physically delete rows | A behavioral version bump (wire unchanged) goes through a v2 proposal; v1 anonymizes and preserves auditing and foreign-key integrity |
| Deletion reason / appeal record | Admin-side fields, not part of the player contract (PII risk; the player side need not send it back) |
