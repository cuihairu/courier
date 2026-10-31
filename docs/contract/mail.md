# Mail Contract (Mail) — Operations-to-Player Mail

> Status: **Frozen v1** · F62 · 2026-11-01. The domain prefix `MAIL` is registered ([errors.md](./errors.md)); this document is the consistency basis between the F62 server (default provider **carrier**) and the SDKs on every platform. Once frozen it is additive-only; breaking changes bump the version (see [versioning.md](./versioning.md)).

## Capabilities and Scope

- Capability domain `mail` (`providers.CapMail`), routes `/v1/mail/*`; default provider **carrier**, swappable and disableable.
- This is a **player-side projection**: operations send mail through the Provider management plane (the integrator's operations system), not through the gateway's player side.
- Mail is **only the carrier** of attachments: it does not introduce a second unit of value; granting goes through the integrator's fulfillment hook (mail's attachment describes what to grant).
- Real-time push uses the [messages.md](./messages.md) channel (`mail.updated` event); the data is a minimal hint, the list endpoint is authoritative.

## Endpoint Overview (mounted at `/v1/mail/*`)

| Endpoint | Auth | Description |
| --- | --- | --- |
| `GET /v1/mail` | Bearer | My mail list (personal + server-wide, newest first) |
| `GET /v1/mail/{mailId}` | Bearer | Mail detail |
| `POST /v1/mail/{mailId}/claim` | Bearer | Claim attachments (idempotent per account+mail) |

- Auth and scope headers are the same as [auth.md](./auth.md); the envelope and pagination follow [primitives.md](./primitives.md).

## Data Model

### mail

| Field | Type | Description |
| --- | --- | --- |
| `id` | id | Server-generated, UUIDv7, prefix `mail_` |
| `title` | string | 1–120 characters |
| `body` | string | 1–4000 characters |
| `attachments` | array | Opaque item descriptors (type/amount/currency; no second unit of value) |
| `createdAt` | timestamp | Send time; list sort key (newest first) |
| `expiresAt` | timestamp? | Optional expiry; absent = never expires |

### attachment

| Field | Type | Description |
| --- | --- | --- |
| `id` | string | Integrator-defined item ID |
| `type` | string | Grant kind (open set; semantics belong to the integrator) |
| `amount` | int64 | Quantity |
| `currency` | string? | Optional currency label (empty for item grants) |

## Endpoint Contracts

### `GET /v1/mail`

Query: `limit` (default 20, max 100), `cursor` (opaque). Response `data` = pagination of the caller's visible mail (server-wide + personal), `createdAt` descending.

### `GET /v1/mail/{mailId}`

Hit and visible → 200 with the mail. Not found / not owned / expired → 404 `MAIL_NOT_FOUND` (existence not leaked).

### `POST /v1/mail/{mailId}/claim`

Idempotent claim: repeated claims return the same result without a second grant. The fulfillment hook is injected by Provider construction; a grant failure is a 503 `COMMON_UNAVAILABLE` and the mail stays unclaimed (claim state is not marked until the hook succeeds).

## Push Events

`mail.updated` (payload `{ "mailId": "mail_..." }`) — a hint that it is time to pull the list; list is authoritative.

## Error Code Domain Table (prefix `MAIL`, errors.md v9)

| code | HTTP | retryable | Description |
| --- | --- | --- | --- |
| `MAIL_NOT_FOUND` | 404 | false | Mail not found or not owned by the current player |

## Client Requirements

- Capability not wired in (501 `COMMON_CAPABILITY_DISABLED`) → queries return null; hide the mail UI.
- Push is a hint, polling is authoritative.
