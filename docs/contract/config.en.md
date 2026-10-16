[English](./config.en.md) | [中文](./config.md)

# Remote Config Contract

> Status: **Frozen v1** · Milestone M3 · Frozen 2026-10-09.
> Domain prefix `CONFIG` registered ([errors.md](./errors.en.md)); this document is the consistency contract between the M3 server-side (default Provider **scribe**, same pipeline as branding/app) and the per-platform SDKs. After freezing, additions only, no modifications; breaking changes raise the version (see [versioning.md](./versioning.en.md)).
> Research anchors: must-have pipeline F47 (remote config / feature flags), F48 (segmentation and conditional targeting), F50 (version history and rollback); see [features.md §14](../research/features.en.md).

## Capabilities and Scope

- Capability domain `app` (`providers.CapApp`), routes `/v1/app/*`; this contract is a **player-side read-only projection** — key-value editing, condition rules, publish and rollback all go through the Provider management plane (the integrator's admin console or publishing via croupier), not through the gateway's player side; same pattern as announcements.
- Key-value model: flat `key → value`, where `value` is any JSON value (string / number / boolean / object; the client parses complex structures itself). Key names 1–128 characters, `[a-zA-Z0-9_.-]`; per config set ≤200 keys, single value ≤32 KiB.
- Version semantics (F50): every publish produces a new `configVersion` (globally monotonically increasing); **rollback = publishing historical content under a new version number** — the player side only ever sees the version increase, there is no version rollback.
- Conditions and gradual rollout (F48) are **management-plane semantics**: the DSL for condition dimensions, audience segmentation, and release percentages never enters the player contract. Player-side projection rules:
  - Request parameters (optional): `platform` / `appVersion` / `region`; absent dimensions do not participate in filtering.
  - Under the same request parameters and the same account, the content of a given `configVersion` is **stable** (no flapping for the same account during gradual rollout).
  - A client that matches none of the keys gets empty `items`, which is not an error.
- Real-time hot updates go through the [messages.md](./messages.en.md) channel `config.updated` event (see below); when the channel is closed, startup / foreground-entry pulls are the fallback.
- This contract does **not cover**: A/B experiments and metric significance (F49, a Provider extension point), or config audit and change-author records (F50, a management-plane responsibility, not exposed on the player side).

## Endpoint Overview (mounted at `/v1/app/*`)

| Endpoint | Auth | Description |
| --- | --- | --- |
| `GET /v1/app/config` | Bearer | Full set of key-values matching the current conditions + `configVersion` |

- Authentication and scope header requirements are the same as [auth.md](./auth.en.md); if missing → the corresponding common error code.
- An empty config set returns `200 {"configVersion": n, "items": {}}`, not 404; `CONFIG_NOT_FOUND` is reserved for the v1.1 single-key query and is not used in this version.

## Data Model

| Field | Type | Description |
| --- | --- | --- |
| `configVersion` | int | Monotonically increasing; basis for hot-update detection (compared against the local cache; if identical, no-op) |
| `items` | object | `key → JSON value` map; key and value limits per "Capabilities and Scope" |

Response example:

```json
{
  "configVersion": 7,
  "items": {
    "shop_switch": true,
    "pvp_ratio": 1.5,
    "banner": { "img": "summer.png", "jump": "activity_01" }
  }
}
```

## Push Events (via the messages.md channel)

| event type | data | Semantics |
| --- | --- | --- |
| `config.updated` | `{"configVersion": 7}` | The management plane published a new version; the client re-pulls accordingly |

- The event is **broadcast to all connected clients**; the server does not track each client's condition matches: on receiving it, the client re-pulls `/v1/app/config` and compares against its cached `configVersion` — identical means no-op, different means the freshly pulled result takes effect as a hot update (clients whose hit set has not changed re-pull the same content, no visible change; `configVersion` is globally monotonic and does not look at this client's hit set).
- The data structure carries only the version, not the key-values; inventing additional fields is forbidden (same pattern as `announcement.published`).

## Error Codes (Domain Table)

This domain's player-side endpoints add **no new error codes**: parameters / auth / rate limiting / capability disabled are all common error codes ([errors.md](./errors.en.md)); `CONFIG_NOT_FOUND` (404) is reserved for the v1.1 single-key query.

## Client Requirements (per-platform SDKs)

- Pull at startup and on entering the foreground; after the `config.updated` event arrives, re-pull and compare the version, ignoring if identical (no re-render).
- `501 COMMON_CAPABILITY_DISABLED` = the config capability is not wired up: the SDK returns an empty result / a "not enabled" state; the integrator then uses built-in defaults and **does not enter the error path**.
- Value-parse failures (integrator structural changes) are the consumer's error: the SDK returns the raw JSON value and performs no secondary validation.
- The client may cache the last `configVersion` and its content; on a failed re-pull it keeps using the cache (config is an accelerator, not a dependency).

## Extension Reservations (Note Added 2026-10-11)

| Possible Extension | Compatible Handling |
| --- | --- |
| Single-key query | `CONFIG_NOT_FOUND` is already reserved for the v1.1 single-key query (error codes section above) and is not used in this version; adding the endpoint = compatible |
| New targeting dimensions | Condition DSL dimensions are a management-plane open set; a new player-side query parameter follows the "absent dimensions do not participate in filtering" semantics, wire-compatible without a version bump |
| Typed values | v1 values are arbitrary JSON (opaque); typed values = v1.2 adds an optional field; older clients keep reading the raw value with no secondary validation (the convention already frozen above) |
| Publish scheduling / timed effect | Management-plane semantics (same as gradual rollout / rollback); the player side only ever sees the stable projection of a monotonically increasing version |
| Key-space expansion | The limits (200 keys / 32 KiB) are management-plane configuration; adjusting them does not change the wire |
