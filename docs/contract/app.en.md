[English](./app.en.md) | [中文](./app.md)

# App State Contract (App)

> Status: **Frozen v1** · Milestone M3 · frozen 2026-10-09.
> The domain prefix `APP` is registered ([errors.md](./errors.en.md)); this document is the consistency basis between the M3 server (default provider **scribe**, on the same pipeline as config/branding) and the SDKs on every platform. Once frozen it is additive-only; breaking changes bump the version (see [versioning.md](./versioning.en.md)).

## Capabilities and Scope

- Capability domain `app` (`providers.CapApp`), routes `/v1/app/*`; the version switch and the maintenance switch are set in the integrator's management plane — this contract is the player-side read-only projection.
- This contract **does not cover**: update-package distribution and diffing (integrators build their own or use a store channel), the compliance copy of forced-update dialogs (belongs to Branding).

## Endpoint Overview (mounted at `/v1/app/*`)

| Endpoint | Auth | Description |
| --- | --- | --- |
| `GET /v1/app/version` | anonymous OK | Version check (update-gate decision) |
| `GET /v1/app/maintenance` | anonymous OK | Maintenance status query |
| `GET /v1/app/environment` | anonymous OK | Environment echo (for initialization verification) |

- All three endpoints are **reachable anonymously** (during maintenance the client must still be able to query status); a valid Bearer also works.
- Scope header requirements are the same as [auth.md](./auth.en.md) (`X-Courier-Game-Id` / `X-Courier-Env`); missing → the corresponding generic code.
- Successful responses are HTTP 200; for the envelope see [primitives.md](./primitives.en.md).

## Endpoint Contracts

### `GET /v1/app/version`

Query parameter: `platform` (optional; `ios`/`android`/`windows`/..., case-sensitive).

Response `data`:

| Field | Type | Description |
| --- | --- | --- |
| `latestVersion` | string | Latest version |
| `minVersion` | string | Minimum usable version; below it a forced update applies |
| `updateUrl` | string? | Update redirect URL (optional payload) |
| `forceUpdate` | bool | Server decision: true when the client version < minVersion |

- The client version is reported via the `appVersion` query parameter (absent = no decision, `forceUpdate` is false).
- When `forceUpdate = true` the client guides the user to update.
- **The sole channel for the version decision is this endpoint's 200 payload; the gateway currently does not send `426 APP_VERSION_UNSUPPORTED`**
  (2026-10-15 patrol delegated decision, aligned with the implementation: `checkUpdate` is a query endpoint, and returning 426 would turn "check whether an
  upgrade is needed" into an error path). Server-side interception of protected operations is done by the **integrator** in a layer in front of Courier;
  that layer keeps using `426 + updateUrl` — the code is registered and the six-platform mapping is in place, so integrators can use it as-is;
  when the integrator does not set up that layer, the client guides the upgrade based on `forceUpdate`. See the "registered reserved codes" note in
  [errors.md](./errors.en.md).

### `GET /v1/app/maintenance`

Response `data`:

| Field | Type | Description |
| --- | --- | --- |
| `inMaintenance` | bool | Maintenance switch |
| `estimatedRecoveryAt` | timestamp? | Estimated recovery time (optional) |
| `message` | string? | Maintenance notice copy (optional, single language) |

- While maintenance mode is on, the gateway returns `503 APP_MAINTENANCE` for **new-session** auth/business requests (the payload may carry `estimatedRecoveryAt`); established sessions are not forcibly kicked. The `/v1/app/*` endpoints of this domain and `/healthz` are not intercepted by the maintenance gate (otherwise the client would have no way to learn about recovery).

### `GET /v1/app/environment`

Response `data`:

| Field | Type | Description |
| --- | --- | --- |
| `gameId` | string | Echoes the game_id from the scope |
| `env` | string | Echoes the env from the scope |

- Purpose: after SDK initialization, verify that the scope configuration matches the gateway's decision (troubleshooting a wrongly wired environment).

## Error Codes (Domain Table)

| code | HTTP | retryable | Description |
| --- | --- | --- | --- |
| `APP_MAINTENANCE` | 503 | false | Under maintenance (payload carries the estimated recovery time, optional) |
| `APP_VERSION_UNSUPPORTED` | 426 | false | Version too old (payload carries the download URL, optional) |

> `APP_VERSION_UNSUPPORTED` is currently a **registered reserved code, not sent by the gateway** (see "the sole channel for the version decision" above);
> `APP_MAINTENANCE` is actually issued by the maintenance gate. For the full table of reserved codes see [errors.md](./errors.en.md).

Everything else (auth, scope, rate limiting, capability disabled) uses the generic codes; see [errors.md](./errors.en.md).

## Client Requirements (per-platform SDKs)

- Startup flow: fetch `/v1/app/version` → guide the update per `forceUpdate`; fetch `/v1/app/maintenance` → maintenance page; once both pass, proceed to login.
- On `503 APP_MAINTENANCE`: show the maintenance page and poll this endpoint per `estimatedRecoveryAt`; make no other requests.
- On `426 APP_VERSION_UNSUPPORTED` (only when the integrator's front layer intercepts): guide per `updateUrl`; the SDK never auto-jumps to a store.
- `501 COMMON_CAPABILITY_DISABLED` = the App capability is not wired in: skip the version/maintenance checks and log in directly (the capability quietly does not exist).

---

## Extension Reservations (Note Added 2026-10-11)

| Possible Extension | Compatible Handling |
| --- | --- |
| New read-only endpoints | Adding an endpoint = compatible, frozen along with the domain contract |
| Forced-update compliance copy / dialogs | Belongs to Branding (already stated in this contract); the SDK only guides via `426 + updateUrl` and ships no built-in copy |
| Update-package distribution / diffing | Not covered by this contract; reserved for a later domain proposal |
| Maintenance whitelist expansion | Management-plane configuration (currently built in as `/v1/app/*` + `/healthz`); the client contract does not change |
| Staged rollout / per-version maintenance | Management-plane semantics; the player side only ever sees the version / maintenance projections (same wording as config) |
