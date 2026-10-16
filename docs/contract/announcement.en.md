[English](./announcement.en.md) | [中文](./announcement.md)

# Announcement Contract

> Status: **Frozen v1.1** · Milestone M2 (v1 frozen 2026-10-08; v1.1 additions in two batches: 2026-10-12 targeting filter F35, 2026-10-13 multi-language content F76).
> Domain prefix `ANNOUNCEMENT` registered ([errors.md](./errors.en.md)); this document is the consistency contract between the M2 server-side (default Provider **herald**) and the per-platform SDKs. After freezing, additions only, no modifications; breaking changes raise the version (see [versioning.md](./versioning.en.md)).

## Capabilities and Scope

- Capability domain `announcements` (`providers.CapAnnouncements`), routes `/v1/announcements/*`; default Provider **herald**, swappable and disableable (Provider principle: provided by default, swappable and disableable).
- This contract is a **player-side read-only projection**: publishing by operations goes through the Provider management plane (the integrator's operations system / admin console), not through the gateway's player side — the gateway only holds a read-only projection, so it is naturally immune to unauthorized writes.
- Real-time push does not go through this domain's endpoints: it goes through the [messages.md](./messages.en.md) channel (`announcement.published` event; the data is isomorphic to this document's DTOs).
- This contract does **not cover**: announcement management/editing/deletion (Provider management plane) and localized multi-language content (title/body are single-language; integrator multi-language goes through `extras` or separate management-plane entries).

## Endpoint Overview (mounted at `/v1/announcements/*`)

| Endpoint | Auth | Description |
| --- | --- | --- |
| `GET /v1/announcements` | Bearer | List of visible announcements (paginated, `publishedAt` descending) |
| `GET /v1/announcements/{announcementId}` | Bearer | Announcement details |

- Authentication and scope header requirements are the same as [auth.md](./auth.en.md) (`Authorization: Bearer` + `X-Courier-Game-Id` / `X-Courier-Env`); if missing → the corresponding common error code.
- Successful responses return HTTP 200; the envelope and pagination follow [primitives.md](./primitives.en.md) (`items` + `nextCursor`; an empty/absent `nextCursor` = last page).

## Data Model

### announcement

| Field | Type | Description |
| --- | --- | --- |
| `id` | id | Generated server-side, UUIDv7, prefix `ann_` |
| `title` | string | 1–120 characters |
| `body` | string | 1–4000 characters |
| `severity` | enum | `INFO` / `WARNING` / `CRITICAL` (uppercase; drives player-side UI color tiers) |
| `startAt` | timestamp? | Visibility window start (absent = visible immediately upon publishing) |
| `endAt` | timestamp? | Visibility window end (absent = never expires) |
| `publishedAt` | timestamp | Publish time; list sort key |
| `filters` | object? | Targeting filter dimension map (v1.1): `dimension → string[]` **open set**; matching basis for the server, read-only echo; absent = no filtering, visible to everyone |

- **Visibility**: `publishedAt <= now` and `now ∈ [startAt?, endAt?]` (absent bounds are treated as unbounded); only visible announcements enter the list and detail views — expired or not yet at `startAt` always returns `ANNOUNCEMENT_NOT_FOUND`.
- Timestamps are always RFC 3339 millisecond UTC verbatim ([primitives.md](./primitives.en.md)); unknown fields are tolerated.

## Endpoint Contracts

### `GET /v1/announcements`

Query parameters:

| Parameter | Description |
| --- | --- |
| `limit` | Items per page, default 20, max 100; out of range → `COMMON_INVALID_ARGUMENT` |
| `cursor` | Pagination cursor (pass the previous page's `nextCursor` back verbatim; invalid → `COMMON_INVALID_ARGUMENT`) |
| `platform` / `appVersion` / `region` / `language` | Optional targeting dimensions (v1.1); **absent dimensions do not participate in filtering**; unknown query parameters are ignored (primitive tolerance rule) |

**Targeting filter matching rules (v1.1)**: for every dimension present in `filters`, if the request carries the corresponding query parameter and the value is not in the set → that announcement is not visible to that request (not returned in the list; detail returns 404 `ANNOUNCEMENT_NOT_FOUND`); if the request omits that dimension's parameter → that dimension does not exclude anything. This reuses the player-side projection semantics already frozen in [config.md](./config.en.md) (absent dimensions skipped, stable within a request, condition DSL stays out of the player contract) rather than inventing a second filtering mechanism; the SDK does not interpret, evaluate, or validate `filters` content; zero new error codes.

Response `data`:

```json
{
  "items": [ { "id": "ann_...", "title": "...", "body": "...", "severity": "INFO",
               "startAt": "2026-10-08T00:00:00.000Z", "endAt": null,
               "publishedAt": "2026-10-08T00:00:00.000Z" } ],
  "nextCursor": ""
}
```

### `GET /v1/announcements/{announcementId}`

- Hit and within the visibility window → `data` = single announcement;
- Does not exist / not visible → 404 `ANNOUNCEMENT_NOT_FOUND`.

## Error Codes (Domain Table)

| code | HTTP | retryable | Description |
| --- | --- | --- | --- |
| `ANNOUNCEMENT_NOT_FOUND` | 404 | false | Announcement does not exist or is outside its visibility window |

All others (auth, scope, rate limiting, capability disabled) are common error codes; see [errors.md](./errors.en.md).

## Client Requirements (per-platform SDKs)

- List/detail failures are handled via the envelope `error`; `ANNOUNCEMENT_NOT_FOUND` is not retried.
- When the channel is closed ([messages.md](./messages.en.md) 501 `COMMON_CAPABILITY_DISABLED`), announcements fall back entirely to **pull** (this domain) — M2 acceptance criterion: "pull fallback works when the push channel is closed".
- When the `announcement.published` event arrives, the client may incrementally update the list or do a full re-pull; the data structure is isomorphic to this domain's DTOs — do not invent additional fields.

---

## v1.1 Freeze Record (2026-10-12)

F35 announcement targeting filters ([features.md](../research/features.en.md) F35) frozen per the 2026-10-11 proposal: `filters` open-set map + four optional query dimensions; matching rules as in "Endpoint Contracts" above; reuses config projection semantics, zero new error codes. The original proposal's "Extensions and Evolution" table takes effect with the freeze:

| Possible Extension | Compatible Handling |
| --- | --- |
| New filter dimensions (channel / finer region / custom tags) | Management-plane new dimension key + client-side new query parameter, **wire-compatible without a version bump**; older SDKs do not send the new parameter → that dimension automatically does not participate in filtering (absent-dimension skip semantics as fallback) |
| Refresh after the targeted hit set changes | Reuse the `announcement.published` event; the list contract is untouched |
| Gradual rollout / bucketing / percentage release | Management-plane semantics (same as config gradual rollout); the player side only ever sees the stable projection |
| Hit explanation / debugging | Management-plane capability, not exposed to the player side — to avoid leaking operational targeting rules |

---

## Multi-language Content (F76, Frozen · Batch 57, 2026-10-13)

> Source: [features.md](../research/features.en.md) F76, paired with the already frozen `language` filter dimension. **Status: Frozen** (frozen 2026-10-13, batch 57; original Draft proposal 2026-10-11 batch 48, frozen verbatim with no semantic changes).

### Data Model

| Field | Type | Description |
| --- | --- | --- |
| `language` | string? | RFC 5646 tag (`zh-CN` / `en` / `ja`); absent = no language restriction, visible to everyone |
| `groupKey` | string? | Management-plane association of multi-language entries with the same content; not interpreted by the client, provided only for product logic such as "dedupe the same announcement" |

- Model: **one announcement = single-language content**; multi-language = published as separate entries on the management plane (contractualization of the v1 stance "integrator multi-language via separate management-plane entries"); the `title` / `body` field types are unchanged, zero breakage.
- Client side: the SDK **automatically attaches** the `language` query parameter (from the system/app locale; the game may explicitly override it); the server applies the same `filters` matching rules to the `language` dimension.
- Automatic-attachment conventions (fixed by the SDK implementation): the integrator provides the locale in the init config (`CourierConfig.language`; the engine-side system language is read in by the integrator; the SDK itself uses zero engine APIs and is test-injectable), and when the list request does not explicitly carry a `language` query parameter it is appended automatically; an explicit value = override; absent config = not attached (same behavior as v1).

### Extensions and Evolution

| Possible Extension | Compatible Handling |
| --- | --- |
| Multiple languages visible at once (single multi-language object) | Breaking (`title` becomes an object), goes to v2, never changed in place |
| Language fallback (`zh-CN` missing → fall back to `zh`) | Handled on the management plane when publishing entries (publish the fallback-language entry); the client contract adds no fallback logic |
| New filter dimensions (channel / finer region / custom tags) | Already frozen with v1.1 targeting filters (freeze record table above): management-plane new dimension key + client-side new query parameter, wire-compatible without a version bump |

Freeze in effect: the original proposal's "Extensions and Evolution" table takes effect with the freeze (multiple languages visible at once = breaking, goes to v2, never changed in place); `language` / `groupKey` are additive optional fields, older clients tolerate unknown fields when reading for compatibility; zero new error codes (errors version untouched); no new matching mechanism — the `language` query parameter reuses the already frozen `filters` matching rules, while the inline `language` field is language-declaration metadata and does not participate in matching (visibility is still decided by `filters`).
