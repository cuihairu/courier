[English](./oauth.en.md) | [中文](./oauth.md)

# Third-Party Identity Provider Contract (OAuth / SSO)

> Status: **Frozen v1** (2026-10-12, frozen in batch 52; the original Draft v0.1 proposal was 2026-10-11 batch 49, frozen as written without changing semantics) · Capability domain `oauth` (routes `/v1/identity/oauth/*`).
> The domain prefix `OAUTH` enters [errors.md](./errors.en.md) v6 with this domain's freeze (existing frozen rows are untouched).

## Why a Separate Domain

- [auth.md](./auth.en.md) v1's frozen wording (line 10) explicitly states that "third-party OAuth / phone number" is not covered, leaving it for later domain contract extensions; the F03 proposal's extension table likewise stipulates that "when the OAuth domain contract is extended, adding an enum value to `type` covers it".
- Third-party providers (Google / Apple / WeChat / Facebook / Line / Discord / Steam …) are essentially **vendors**: the verification mechanism (JWKS / introspection / code-for-token), client secret, and app configuration all live on the admin side, and the player contract only sees "exchange third-party credentials for a Courier session". The contract must remain agnostic about "which vendors exist".
- Boundary with [auth.md](./auth.en.md): this domain **does not redesign the account, session, or token model** — `accounts` / `sessions` / `devices`, token rotation, replay detection, and scope binding are all reused from the auth domain; this domain only adds the "third-party identity ↔ account" mapping and the rebind/unbind endpoints.

## Capabilities and Scope

- Capability domain `oauth` (`providers.CapOAuth`), default provider **courier-account** (swappable and disableable, the same provider surface as the auth domain).
- Provides three things: **exchange a third-party identity for a Courier session**, **bind a third-party identity to a logged-in account**, and **unbind and query bindings**.
- This contract **does not cover**: third-party SDK integration (integrators integrate on their own and obtain the credentials), phone-number verification-code login (an isomorphic domain, proposed separately), third-party account profile (avatar/nickname belong to [player.md](./player.en.md) or the Provider), third-party account deletion (belongs to the `OnAccountDeleted` hook chain of the auth.md F03 proposal).

## Endpoint Overview (mounted at `/v1/identity/oauth/*`)

| Endpoint | Auth | Description |
| --- | --- | --- |
| `POST /v1/identity/oauth/exchange` | anonymous | Exchange third-party credentials for a Courier session (creates an account if there is no match) |
| `POST /v1/identity/oauth/bind` | Bearer | Bind a third-party identity to the current account |
| `DELETE /v1/identity/oauth/bindings/{provider}` | Bearer | Unbind the specified provider from the current account |
| `GET /v1/identity/oauth/bindings` | Bearer | List of the current account's bindings |

- The scope header, envelope, pagination, and rate limiting follow [auth.md](./auth.en.md) / [primitives.md](./primitives.en.md); successful responses are HTTP 200.
- Sensitive endpoints (`exchange` / `bind`) join the auth domain's IP rate-limit bucket.

## Data Model

The auth domain's `accounts` / `sessions` / `devices` are reused; this domain adds only one mapping table:

### account_identities (third-party identity mapping, account dimension)

| Field | Description |
| --- | --- |
| `accountId` | → accounts.id |
| `provider` | Provider identifier (lowercase letters, digits, and hyphens, e.g. `google` / `apple` / `wechat`); **an admin-side registered set, not enumerated by the contract** |
| `subject` | The third-party user's unique identifier (the normalized identifier obtained after third-party verification) |
| `createdAt` | Binding time |

- `(provider, subject)` is globally unique; `(accountId, provider)` is unique.
- `accounts.type` gains the enum value `OAUTH` (additive, landing in auth.md with this domain's freeze): an account created by pure third-party login is of type `OAUTH`, has no credential row, and behaves like `GUEST`'s minimal credential form.

### Data Red Lines (built in; no collection means no commitment)

- **Never store third-party credentials in the clear**: only the `provider + subject` mapping is stored; every login re-verifies online against the third party, and no id_token / access_token is cached.
- **Never send Courier data back to the third party**: verification is a pull-based check with no request-side interaction; accountId / email / device information is never pushed to any third party.
- `subject` must come from the third-party verification result and **must not be self-reported by the client**; any verification failure gives `OAUTH_IDENTITY_INVALID`, without distinguishing "invalid" from "expired" (prevents enumeration).

## Endpoint Contracts

### POST /v1/identity/oauth/exchange — `{ provider, token, tokenType?, deviceId?, platform? }`

- `provider` is required; not registered or not enabled on the admin side → `OAUTH_PROVIDER_UNKNOWN` (400).
- `token` is required: the credential issued by the third party; `tokenType` is an optional enum `ID_TOKEN` / `ACCESS_TOKEN` / `CODE`, defaulting to `ID_TOKEN` — the concrete verification method is decided by the provider implementation (JWKS signature verification / introspection / code-for-token), and **the contract does not describe verification details**.
- Verification flow: the server verifies online against the third party → obtains `subject` → looks up `account_identities` → on a hit, logs in that account; on a miss, creates an account with `type=OAUTH` and writes the mapping → issues a scope-bound session.
- The response `data` is isomorphic to [auth.md](./auth.en.md) `POST /login` (`tokens` + `account` + `deviceId?`), with `account.type = "OAUTH"`.
- Third-party credentials invalid/expired/forged → `OAUTH_IDENTITY_INVALID` (401); capability domain disabled → `COMMON_CAPABILITY_DISABLED` (501, the unified semantics of domain-level degradation, see [errors.md](./errors.en.md)); device cap reached → `AUTH_DEVICE_LIMIT` (403).

### POST /v1/identity/oauth/bind — `{ provider, token }` (Bearer)

- Binds that third-party identity to the current account for next-time one-tap login (binding the same provider multiple times = unbind first, then overwrite; a repeat binding of the same provider returns `COMMON_INVALID_ARGUMENT`).
- That `subject` is already bound to another account → `OAUTH_IDENTITY_TAKEN` (409, indicating it is bound to another account); verification failure → `OAUTH_IDENTITY_INVALID` (401).

### DELETE /v1/identity/oauth/bindings/{provider} (Bearer)

- Unbinds and deletes the mapping row; idempotent (deleting a nonexistent one still returns 200 with `data: {}`).
- Unbinding does not affect the account itself — a `type=OAUTH` account still exists after unbinding (it can be rebound next time); whether to reclaim empty accounts is an admin-side policy.

### GET /v1/identity/oauth/bindings (Bearer)

- `data.items[]`: `{ provider, createdAt }`; sorted by `createdAt` ascending; `subject` is not returned (to avoid leaking the third-party identifier to the client).

## Error Codes (proposed additions, entering errors.md with the freeze)

| code | HTTP | retryable | Description |
| --- | --- | --- | --- |
| `OAUTH_PROVIDER_UNKNOWN` | 400 | false | Provider not registered or not enabled |
| `OAUTH_IDENTITY_INVALID` | 401 | false | Third-party credential verification failed/expired/forged |
| `OAUTH_IDENTITY_TAKEN` | 409 | false | That third-party identity is already bound to another account |

The domain prefix `OAUTH` is registered in the [errors.md](./errors.en.md) registry; everything else (authentication, scope, rate limiting, capability disabled) uses generic codes throughout.

## Client Requirements (SDKs on each platform)

- The SDK **bundles no third-party SDK**: integrators integrate Google / Apple / WeChat etc. themselves to obtain the credentials and call `exchange`; Courier sees only one `{ provider, token }` shape.
- The third-party credential is **sent only once** to this domain's endpoint and must not be cached locally long-term; `OAUTH_IDENTITY_INVALID` → guide the user to re-authorize, do not retry automatically.
- `OAUTH_IDENTITY_TAKEN` → indicate "already bound to another account" and optionally guide the user to log in with that account instead; `OAUTH_PROVIDER_UNKNOWN` → indicate that this login method is unavailable (possibly not configured).
- `COMMON_CAPABILITY_DISABLED` (oauth domain) → do not show the corresponding login entry.
- Token storage, refresh, and replay detection all reuse the auth domain's wording (the client requirements section of [auth.md](./auth.en.md)); sessions issued by this domain behave no differently from ordinary sessions.

## Extensions and Evolution

| Possible extension | Compatible handling |
| --- | --- |
| New providers (WeChat / Line / Discord / Steam / Apple China region …) | Admin-side registration + provider implementation; `provider` is an open-set string, so **the contract is unchanged and the version does not bump** |
| Phone-number verification-code login | An isomorphic domain: a separate auth domain proposal (the SMS path has its own verification semantics and abuse protection, so it is not stuffed into this domain) |
| OAuth account upgrade (bind email) | Extends the auth domain's `bind` semantics for reuse (writing `account_credentials`), with `type` moving from `OAUTH` to `EMAIL`; fields are added without changing the table |
| Account cascade deletion | Reuses the `OnAccountDeleted` hook chain of the auth.md F03 proposal; `account_identities` rows are cleaned up as the account is anonymized, and `accounts.id` is retained |
| Cross-game identity linkage | `account_identities` hangs off the company-level account, so it is cross-game by nature; but tokens are never opened up across games in place ([scope.md](./scope.en.md) red line) |
| Multiple identifiers per provider (WeChat openid vs unionid) | Normalized to a single `subject` at the Provider configuration level; the contract only sees the normalized identifier |
| Third-party profile display (avatar / nickname) | Belongs to [player.md](./player.en.md) or the Provider, not to this domain (field minimization) |
| Third-party credential rotation / long-term login-free | **Not storing third-party credentials** is a red line and does not loosen for extensions; every login re-verifies |
| Deletion appeals / sanction interception | Belongs to the support domain F45 proposal; sessions issued by this domain are equally bound by it |
