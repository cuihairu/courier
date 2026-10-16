[English](./support.en.md) | [中文](./support.md)

# Support Contract

> Status: **Frozen v1.1** · Milestone M2 (v1 frozen 2026-10-08; v1.1 additions in three batches: 2026-10-12 ticket context F43 and ticket attachments F44, 2026-10-13 reports and penalty appeals F45).
> Domain prefix `SUPPORT` registered ([errors.md](./errors.en.md)); this document is the consistency contract between the M2 server-side (default Provider **croupier**) and the per-platform SDKs. After freezing, additions only, no modifications; breaking changes raise the version (see [versioning.md](./versioning.en.md)).

## Capabilities and Scope

- Capability domain `support` (`providers.CapSupport`), routes `/v1/support/*`; default Provider **croupier**, swappable and disableable.
- Player side: submitting tickets, appending messages, viewing lists/details, FAQ search. The **agent side** (replies, closing, assignment) goes through croupier's internal management plane, not through the gateway — the gateway only lets players read and write their own tickets.
- Real-time visibility of agent replies goes through the [messages.md](./messages.en.md) channel (`support.ticket_replied` event); when the channel is closed, falling back to pulling details.
- This contract does **not cover**: the agent workbench or ticket assignment policy (attachments are covered by the "Ticket Attachments" section below, F44; explicitly out of scope at the v1 freeze, later frozen as an extension section per the proposal).

## Endpoint Overview (mounted at `/v1/support/*`)

| Endpoint | Auth | Description |
| --- | --- | --- |
| `POST /v1/support/tickets` | Bearer | Submit a ticket |
| `GET /v1/support/tickets` | Bearer | My ticket list (paginated, `updatedAt` descending) |
| `GET /v1/support/tickets/{ticketId}` | Bearer | Ticket details (including messages) |
| `POST /v1/support/tickets/{ticketId}/messages` | Bearer | Append a player message |
| `GET /v1/support/faq` | Bearer | FAQ search (keyword, at most 20 items) |

- Authentication, scope headers, envelope and pagination are the same as [auth.md](./auth.en.md) / [primitives.md](./primitives.en.md).
- Ticket **ownership validation**: tickets not owned by the caller always return 404 `SUPPORT_TICKET_NOT_FOUND` (existence is not leaked).

## Data Model

### ticket

| Field | Type | Description |
| --- | --- | --- |
| `id` | id | Generated server-side, UUIDv7, prefix `tkt_` |
| `title` | string | 1–120 characters |
| `category` | string? | Ticket category (uppercase underscore enum, integrator-defined set, unknown values tolerated, not rejected; default `OTHER`) |
| `status` | enum | `OPEN` / `REPLIED` / `CLOSED` (uppercase; submitting a ticket = `OPEN`, an agent reply = `REPLIED`, closing from either side = `CLOSED`) |
| `context` | object? | Ticket context (v.1): a flat **open-set** map of raw values only; the server passes it through with zero interpretation, it is written at ticket creation and echoed verbatim in the detail as `ticket.context`; absent = no context |
| `createdAt` / `updatedAt` | timestamp | RFC 3339 millisecond UTC |

### ticket_message

| Field | Type | Description |
| --- | --- | --- |
| `senderType` | enum | `PLAYER` / `AGENT` / `SYSTEM` |
| `body` | string | 1–4000 characters |
| `createdAt` | timestamp | RFC 3339 millisecond UTC |

Detail responses order `messages` by `createdAt` ascending; the ticket's `body` is the first `PLAYER` message.

### faq

| Field | Type | Description |
| --- | --- | --- |
| `id` | id | UUIDv7, prefix `faq_` |
| `question` / `answer` | string | Question and answer |
| `keywords` | string[] | Keywords for search (may be empty) |

## Endpoint Contracts

### `POST /v1/support/tickets`

Request body (bare object, outside the envelope):

```json
{ "title": "...", "body": "...", "category": "PAYMENT", "context": { "level": 42, "device": "Pixel 8" } }
```

- `category` is optional, an uppercase underscore enum (integrator-defined set, unknown values tolerated, not rejected); default `OTHER`.
- `context` is optional (v1.1): a flat **open-set** map of raw values only (string / number / boolean); keys 1–32 characters `[a-zA-Z0-9_.]`, ≤10 keys, single value ≤256 characters, total ≤1 KiB; exceeding the limits → `COMMON_INVALID_ARGUMENT` (400).
- **Server does zero interpretation**: no whitelist, no business-meaning validation; it stores the value as-is and echoes it in the ticket detail `ticket.context` (visible to agents); unknown keys are tolerated, not rejected. Appending messages does not carry a context (context belongs to the ticket level, not the message level).
- Zero new error codes.
- On success `data` = ticket (`status=OPEN`, including the `context` echo).
- Abuse protection: ticket submission is rate limited (default 5 per account per minute; exceeding the limit → 429 `RATE_LIMITED` + `Retry-After`).

### `POST /v1/support/tickets/{ticketId}/messages`

Request body `{ "body": "..." }` → `data` = ticket_message (`senderType=PLAYER`).

- Ticket `CLOSED` → 409 `SUPPORT_TICKET_CLOSED`; not mine / does not exist → 404 `SUPPORT_TICKET_NOT_FOUND`.
- After a successful append, `status` stays `OPEN` (the player speaking does not change the status; only an agent reply transitions it to `REPLIED`).

### `GET /v1/support/tickets`

`limit` defaults to 20, max 100; `cursor` follows [announcement.md](./announcement.en.md); `items` = tickets, ordered by `updatedAt` descending.

### `GET /v1/support/tickets/{ticketId}`

`data` = `{ "ticket": {...}, "messages": [...] }` (messages ascending).

### `GET /v1/support/faq`

| Parameter | Description |
| --- | --- |
| `keyword` | Keyword; a hit = question/answer/keywords contains it (case-insensitive); the contract specifies 1–64 characters, the gateway does not enforce an upper length limit (longer values are matched verbatim); absent = return everything |
| `limit` | Default 20, max 50 |

`data` = `{ "items": [faq...], "nextCursor": "" }`; no hits return empty `items` (not an error).

## Error Codes (Domain Table)

| code | HTTP | retryable | Description |
| --- | --- | --- | --- |
| `SUPPORT_TICKET_NOT_FOUND` | 404 | false | Ticket does not exist or does not belong to the current player |
| `SUPPORT_TICKET_CLOSED` | 409 | false | Ticket is closed; appending a message is refused |

## Client Requirements (per-platform SDKs)

- After submitting a ticket, listen for `support.ticket_replied` ([messages.md](./messages.en.md)): when it arrives → pull that ticket's details and update incrementally; when the channel is closed → fall back to polling or the user manually refreshing.
- `SUPPORT_TICKET_CLOSED` is not retried; 429 backs off per `Retry-After` (retry semantics in [primitives.md](./primitives.en.md)).
- An empty FAQ search result is normal; the UI presents "no hits" rather than an error.
- When submitting a ticket, the SDK **automatically attaches** the known facts (`gameId` / `platform` / `appVersion`, from scope and lifecycle); the game may append custom keys (level / character / reproduction steps). **Automatically attached keys cannot be overwritten by business code** (v1.1).

---

## v1.1 Freeze Record (2026-10-12)

F43 ticket context attachment ([features.md](../research/features.en.md) F43) frozen per the 2026-10-11 proposal: `context` flat open-set map ("Endpoint Contracts" above and the ticket data model), server-side pass-through with zero interpretation, SDK automatic attachment of the facts ("Client Requirements" above), zero new error codes. The original proposal's "Extensions and Evolution" table takes effect with the freeze:

| Possible Extension | Compatible Handling |
| --- | --- |
| New context keys (any business field) | Defined by the game; open-set map, no contract change needed |
| A key promoted to a structured field (e.g. device info) | v1.2 adds an optional field, the old `context` key stays for compatibility, not deleted or modified |
| Attachment / log upload (F44) | Separate endpoint ("Ticket Attachments" section below, already frozen); **binary is never passed through `context`**; diagnostics logs use diagnostics' own endpoints |
| Agent-side filtering / aggregation of context | Management-plane capability (croupier); the player contract is untouched |

---

## Reports and Penalty Appeals (F45, Frozen · Batch 56, 2026-10-13)

> Source: [features.md](../research/features.en.md) F45. **The determination and enforcement of penalties live entirely in the management plane** (croupier / GM / integrator risk control); this contract is only a player-side read-only projection plus an appeal entry point — same pattern as announcements/config; the gateway performs no risk control. **Status: Frozen** (frozen 2026-10-13, batch 56; original Draft proposal 2026-10-11 batch 48, frozen verbatim with no semantic changes).

### Endpoints

| Endpoint | Auth | Description |
| --- | --- | --- |
| `POST /v1/support/reports` | Bearer | Submit a report |
| `GET /v1/support/penalties` | Bearer | My penalty list (paginated, `issuedAt` descending) |
| `GET /v1/support/penalties/{penaltyId}` | Bearer | Penalty details (including appeal state) |
| `POST /v1/support/penalties/{penaltyId}/appeal` | Bearer | Submit an appeal (at most one in-progress appeal per penalty) |

### Data Model

**report**: `id` (`rpt_` prefix), `targetType` (`PLAYER` / `MESSAGE` / `TICKET`, open set, extendable), `targetId`, `reason` (`CHEATING` / `ABUSE` / `SPAM` / `INAPPROPRIATE` / `OTHER`, extendable on the management plane), `detail?` (≤1000 characters). The `id` is returned upon acceptance; **report progress is never fed back to the reporter** (anti-harassment design).

**penalty** (projection): `id` (`pen_`), `type` (`MUTE` / `SUSPENSION` / `LEADERBOARD_BAN`, open set, client tolerates unknown), `reason` (human-readable explanation aimed at players, containing no internal or agent information), `state` (`ACTIVE` / `EXPIRED` / `LIFTED`), `issuedAt` / `expiresAt?`, `appeal?` (`{ "state": "PENDING" | "ACCEPTED" | "REJECTED", "submittedAt", "decidedAt?" }`).

### Semantics

- Appeal decisions go through the management plane; results are pull-based (`appeal.state`); submitting while an appeal is already in progress → 409 `SUPPORT_APPEAL_EXISTS`.
- Not mine / penalty does not exist → 404 `SUPPORT_PENALTY_NOT_FOUND` (existence is not leaked).
- **2 new error codes added**: `SUPPORT_PENALTY_NOT_FOUND` (404, retryable=false), `SUPPORT_APPEAL_EXISTS` (409, retryable=false).
- v1 **does not change auth login semantics**: whether a penalty blocks login is decided by integrator policy and the management plane (see the extensions section).

### Extensions and Evolution

| Possible Extension | Compatible Handling |
| --- | --- |
| New penalty types / new report reasons | Extended sets on the management plane; `type` is an open set and the client tolerates unknown values; the `reason` set is registered in this section only |
| Penalties blocking login | v1.2 optional-semantics proposal (e.g. integration with `AUTH_ACCOUNT_SUSPENDED`); v1 does not touch auth |
| Report evidence attachments | Shares the separate endpoint with F44 attachments (generalizing the `targetType` dimension is left for a v1.2 proposal); not stuffed into report |
| Cross-game / company-level penalties | penalty gains an optional `gameId?` (absent = the issuing scope) as an additive-field extension; v1 covers only the current scope |
| Report handling progress | Never fed back to the reporter (anti-harassment); internal flow on the management plane; if the product needs it, a separate anonymized progress endpoint can be proposed |

Freeze in effect: the original proposal's "Extensions and Evolution" table takes effect with the freeze (new penalty types / new report reasons go through extended sets on the management plane, `type` is an open set the client tolerates unknown values, no version bump); the new error codes land in errors.md v8; v1 **does not change auth login semantics** (penalties blocking login are left for a v1.2 proposal).

---

## Ticket Attachments (F44, Frozen · Batch 53, 2026-10-12)

> Source: [features.md](../research/features.en.md) F44 feedback and log upload. v1 explicitly left attachments uncovered, to be delivered as a new endpoint extension; this section delivers on that. **The gateway never stores binaries**: the contract only defines the shape of "apply for direct upload", while the storage itself sits behind the Provider (same projection pattern as announcements/config) — swapping S3 / OSS / locally signed URLs leaves the contract untouched. **Status: Frozen** (frozen 2026-10-12, batch 53; original Draft proposal 2026-10-11 batch 50, frozen verbatim with no semantic changes).

### Endpoints

| Endpoint | Auth | Description |
| --- | --- | --- |
| `POST /v1/support/tickets/{ticketId}/attachments` | Bearer | Apply for direct attachment upload, returns a short-lived pre-signed URL |

- Single-endpoint design: the client takes `uploadUrl` and **uploads straight to storage** (HTTP `PUT`, not through the gateway); completion confirmation goes through the storage → Provider callback; the player side treats the ticket detail as authoritative — zero binary traffic through the gateway, naturally immune to large files taking the gateway down.

### Request and Response

Request body `{ "fileName", "contentType", "sizeBytes", "kind"? }`:

| Field | Description |
| --- | --- |
| `fileName` | 1–255 characters, echoed verbatim for display, not interpreted by the server |
| `contentType` | MIME; must be within the management-plane allowlist (default `image/png` / `image/jpeg` / `text/plain` / `application/zip` / `application/pdf`), otherwise → `COMMON_INVALID_ARGUMENT` |
| `sizeBytes` | ≤ 10 MiB; exceeding the limit → 413 `SUPPORT_ATTACHMENT_TOO_LARGE` |
| `kind` | Optional enum `IMAGE` / `LOG` / `OTHER` (open set, client tolerates unknown) |

Response `data`: `{ "attachmentId": "att_…", "uploadUrl", "method": "PUT", "headers": {…}, "expiresAt" }` — `uploadUrl` is a short-lived pre-signed URL (default 10 minutes, configurable on the management plane); if it expires, just apply again, **no recovery semantics**.

### Semantics

- The ticket must belong to the current player (otherwise 404 `SUPPORT_TICKET_NOT_FOUND`) and be open (409 `SUPPORT_TICKET_CLOSED`); at most 3 attachments per ticket, exceeding the limit → 409 `SUPPORT_ATTACHMENT_LIMIT`.
- The ticket detail (already frozen `GET /tickets/{id}`) gains `attachments[]` (additive extension, older clients tolerate unknown fields): `{ id, fileName, contentType, sizeBytes, kind?, createdAt, downloadUrl? }` — `downloadUrl` is likewise short-lived and pre-signed, players only ever see their own; the agent side goes through croupier's management plane.
- **2 new error codes added** (errors.md v7): `SUPPORT_ATTACHMENT_TOO_LARGE` (413, retryable=false), `SUPPORT_ATTACHMENT_LIMIT` (409, retryable=false).
- Red line: the server and SDK do **zero content interpretation** (no semantic scanning, no rewriting); virus scanning and content moderation belong to the management plane; attachments and `context` (F43 proposal) are mutually exclusive carriers — binary never enters `context`.

### Extensions and Evolution

| Possible Extension | Compatible Handling |
| --- | --- |
| Report evidence attachments (already frozen in F45) | Endpoint-group generalization proposal (`targetType` dimension, bundled into v1.2), sharing the same storage plane and quota semantics |
| Storage Provider swap (S3 / OSS / local disk) | The pre-signed URL abstraction already hides the storage behind the Provider; the contract only recognizes the "short-lived direct upload URL" shape; swapping storage = swapping Provider config, contract untouched |
| New attachment types (video / large files) | Extended via management-plane allowlist and quota configuration; oversized files go through the Provider's direct-upload plane, never into the player contract (fields kept minimal) |
| Thumbnails / preview | Provider / management-plane capability; the player side only recognizes `downloadUrl`, the contract adds no preview semantics |
| Attachment deletion | v1 only adds, never deletes (once the ticket is closed, nothing more can be uploaded to it); if the player side needs a delete entry point for compliance, it goes through a v1.2 add-endpoint proposal |
