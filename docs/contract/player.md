
# Player Profile Contract (Player Profile)

> Status: **Frozen v1** · Milestone M3 · frozen 2026-10-09.
> This document is the consistency basis between the M3 server (default provider **archivist**, a self-built lightweight implementation) and the SDKs on every platform. Once frozen it is additive-only; breaking changes bump the version (see [versioning.md](./versioning.md)).
> Positioning: the account↔character mapping and the account profile; **character data belongs to each game** (levels/inventory/progress never go through Courier).

## Capabilities and Scope

- Capability domain `player` (`providers.CapPlayer`), routes `/v1/player/*`; everything requires Bearer.
- The account profile = the display name shared across games (the facade of the login account); the character mapping = the `playerId` binding records under this scope (`game_id + env`); the game side uses `playerId` to fetch data from its own game server.
- This contract **does not cover** (the "field minimization" red line): character data and progress (belongs to each game's servers), friends/social relationships, hosting of profile avatar assets (only the URL string is stored; the assets belong to the integrator's object storage).
- Not collected: email/phone number are not echoed in this domain (the auth domain already has them); no device or behavioral fields of any kind.

## Endpoint Overview (mounted at `/v1/player/*`)

| Endpoint | Auth | Description |
| --- | --- | --- |
| `GET /v1/player/profile` | Bearer | Account profile |
| `PATCH /v1/player/profile` | Bearer | Change the display name |
| `GET /v1/player/characters` | Bearer | Character mappings bound in this game |
| `POST /v1/player/characters` | Bearer | Bind a character mapping (idempotent) |

- Scope header requirements are the same as [auth.md](./auth.md); success is HTTP 200; for the envelope see [primitives.md](./primitives.md).

## Data Model

profile:

| Field | Type | Description |
| --- | --- | --- |
| `displayName` | string | Display name; 1–30 characters (after trimming leading/trailing whitespace) |
| `avatarUrl` | string? | Avatar URL (optional; responses always contain the field, unset = empty string; only the URL is stored, the assets belong to the integrator) |
| `createdAt` / `updatedAt` | timestamp | Profile creation / last modification |

character (character mapping):

| Field | Type | Description |
| --- | --- | --- |
| `playerId` | string | Game-side character ID; 1–64 characters, defined and interpreted by each game |
| `boundAt` | timestamp | Binding time |

- Binding cap: ≤50 records per account per scope; exceeding it → `COMMON_INVALID_ARGUMENT` (integrators handle it with an "unbind first, then bind" product flow; v1 has no unbind endpoint, reserved for v1.1).
- `POST /v1/player/characters` is **idempotent**: binding the same `playerId` again returns the existing record (200), not an error.

## Error Codes (Domain Table)

This domain **adds no new error codes**: invalid arguments (display-name length, playerId length, over the binding cap) are always `COMMON_INVALID_ARGUMENT`; auth/rate limiting/capability disabled are always the generic codes ([errors.md](./errors.md)).

## Client Requirements (per-platform SDKs)

- After a successful `PATCH`, the response body is authoritative (the server-trimmed displayName);
- Binding is idempotent: repeated calls do not error; the returned existing record is authoritative;
- `501 COMMON_CAPABILITY_DISABLED` = the capability is not wired in: the SDK returns a "not enabled" state (null); integrators hide the profile entry and never enter the error path.

## Extension Reservations (Note Added 2026-10-11)

| Possible Extension | Compatible Handling |
| --- | --- |
| Character unbind endpoint | Reserved for v1.1 (see the binding-cap note above: v1 has no unbind, the "unbind first, then bind" flow); adding an endpoint = compatible |
| New profile fields | Add optional fields + unknown-field tolerance (primitive rules); third-party profile data (avatar/nickname) stores only the URL string, asset hosting belongs to the integrator (red line unchanged) |
| Character deletion/restoration | Add-endpoint proposal (soft delete + grace period), semantics aligned with the auth F03 deregistration proposal; not covered in v1 |
| Cross-game profile aggregation | The v1 profile and mappings hang off the scope; company-level aggregation belongs to management-plane analytics and does not enter the player contract |
| Binding-cap adjustment | Management-plane configuration (≤50/scope); the contract does not freeze a specific value |
