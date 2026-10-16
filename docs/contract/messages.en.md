[English](./messages.en.md) | [中文](./messages.md)

# Push Channel Contract (Messages)

> Status: **Frozen v1.1** · Milestone M2 · freeze date 2026-10-08 (v1.1 · 2026-10-11 adds the "transport framing" server-side hard requirement — it clarifies the framing semantics that streaming reads depend on and does not change any endpoint/header/DTO/wire shape).
> This domain has **no dedicated error-code prefix** (channel-layer errors all reuse generic codes, see the note in [errors.md](./errors.en.md)); this document is the consistency basis between the M2 server (default provider **chirp**) and the SDKs on every platform. Once frozen it is additive-only; breaking changes bump the version (see [versioning.md](./versioning.en.md)).

## Capabilities and Scope

- Capability domain `messages` (`providers.CapMessages`), routes `/v1/messages/*`; default provider **chirp**, swappable and disableable.
- **Disabled = pull-only fallback**: capability not configured / explicitly disabled → `501 COMMON_CAPABILITY_DISABLED`, and the SDKs on every platform switch to pure pull mode (polling announcements [announcement.md](./announcement.en.md), manual/scheduled refresh for support [support.md](./support.en.md)), so **functionality is not crippled**.
- **Session reuse**: a single connection carries all domain events (announcements, support, later domains) instead of opening one per domain — mobile connections are a scarce resource.
- Transport choice: **SSE** (text/event-stream) — fully implementable with the standard library, HTTP semantics reused for free (scope/trace/rate limiting/envelope), and streamable with Unity `UnityWebRequest`; WebSocket is left for later extension (it bumps the version when both ends need to be written).
- This contract **does not cover**: offline push (APNs/FCM, a later domain), event backfill (M2 semantics below).

## Endpoint Overview (mounted at `/v1/messages/*`)

| Endpoint | Auth | Description |
| --- | --- | --- |
| `GET /v1/messages/stream` | Bearer | SSE event stream (long-lived connection) |

- Bearer is mandatory: the channel serves **authenticated players**, so anonymous → 401 `COMMON_UNAUTHENTICATED`.
- The scope header is the same as [auth.md](./auth.en.md); rate limiting counts connection establishments (not events within the stream).
- Capability disabled → `501 COMMON_CAPABILITY_DISABLED` (the degradation signal; the SDK uses it to safely turn off push UI).

## Stream Protocol

Response header: `Content-Type: text/event-stream` (SSE standard, [WHATWG HTML §server-sent-events](https://html.spec.whatwg.org/multipage/server-sent-events.html)).

**Transport framing (server-side hard requirement)**: the response must be delivered with streaming framing — no `Content-Length` (chunked or equivalent streaming transport), and a flush after every frame. The six SDKs' streaming read implementations (fetch / wx.request enableChunked / XHR onprogress / UnityWebRequest / HTTPClient poll / UE seam) all implement incrementally readable semantics, and a fixed-length buffered response would break streaming reads.

Frame format (SSE standard, not a JSON envelope):

```
id: 42
event: announcement.published
data: {"id":"ann_...","title":"...","body":"...","severity":"INFO","publishedAt":"..."}

```

- `event` = the event type, two lowercase segments `<domain>.<event>`, with underscores for multiple words (same naming rules as [events.md](./events.en.md)).
- `data` = a single-line JSON payload **isomorphic to that domain's pull-endpoint DTO** — the client needs zero new types and just reuses the domain DTO.
- `id` = a server-side (hub-level) monotonically increasing sequence number, **continuous across connections** (not within a connection; for debugging/logging; **M2 does no reconnect backfill**, and the `Last-Event-ID` header may be ignored).
- Heartbeat: the server sends a comment frame `: ping\n\n` every **25s** (keep-alive through proxies); it carries no business semantics for the client.
- Disconnect semantics: only **delivery while online** is guaranteed; events during offline periods are not backfilled and pull is relied on as the fallback — the channel is an "optimization", not a "dependency".

## Event Registry (M2)

| event | data (isomorphic to the pull DTO) | Trigger |
| --- | --- | --- |
| `announcement.published` | announcement ([announcement.md](./announcement.en.md)) | Operations publish a visible announcement |
| `support.ticket_replied` | `{ "ticketId": "tkt_..." }` | An agent replies to / closes a ticket; the client pulls the detail for incremental display |

M3 additions (not isomorphic to the pull DTO: only a version number is carried, and the client re-pulls to compare — the same convention as the config/branding contracts):

| event | data | Trigger |
| --- | --- | --- |
| `config.updated` | `{ "configVersion": 7 }` ([config.md](./config.en.md)) | The admin side publishes a new config version |
| `branding.updated` | `{ "version": 3 }` ([branding.md](./branding.en.md)) | The admin side changes branding assets |

M5 v1.1 additions (payment domain; broadcast hints carrying no business fields — amounts/SKUs belong to the pull endpoint, filtered by ownership):

| event | data | Trigger |
| --- | --- | --- |
| `payment.paid` | `{ "orderId": "order_..." }` ([payment.md](./payment.en.md)) | A channel callback is accepted and the order moves to `PAID` |
| `payment.delivered` | `{ "orderId": "order_..." }` | The order moves to `DELIVERED` (including re-sends on reconciliation recovery) |

New events are registered here as their domain contracts freeze; a `type` does not change once frozen, and a deprecated event is stopped by the server while its type is retained.

## Semantic Rules

1. **Events may be duplicated or out of order**: the channel is at-most-once; the client deduplicates idempotently by the domain DTO's `id`.
2. **Events are not authoritative**: the pull endpoints are authoritative (an event that arrives first is merely a hint that "you should pull now"; direct rendering is supported but the visibility window must be validated).
3. **Connection lifecycle**: the server may drop the stream at any time (release/migration/load); the client reconnects with exponential backoff (recommended starting at 1s, capped at 60s, with the `Retry-After` header taking precedence).
4. **Auth expiry**: a token expiring within the stream does not interrupt an existing connection (M2); new connections are verified against the current token.

## Errors (channel layer, all generic codes)

| Scenario | Response |
| --- | --- |
| Unauthenticated | 401 `COMMON_UNAUTHENTICATED` (structured envelope) |
| Capability disabled | 501 `COMMON_CAPABILITY_DISABLED` |
| Rate limited | 429 `RATE_LIMITED` + `Retry-After` |
| scope missing/mismatched | 400 `COMMON_INVALID_ARGUMENT` / `SCOPE_MISMATCH` |

## Client Requirements (SDKs on each platform)

- Channel health has two states, "present" or "absent"; when absent → pull-only mode, no half-dead polling.
- On receiving an event, deduplicate first (`id`/domain `id`), then parse by the domain DTO; an unknown `event` type **must be tolerated** (ignored, per the unknown-tolerance principle in the contract's versioning.md).
- Reconnect backoff is per semantic rule 3; the app may proactively drop the stream when backgrounded to save power and reconnect on foreground (lifecycle events are in [events.md](./events.en.md)).

## Reserved Extensions (note added 2026-10-11)

| Possible extension | Compatible handling |
| --- | --- |
| New event types | Registration-based: a row is added to the event table as the domain contract freezes (additive-compatible); a `type` does not change once frozen, and a deprecated event is stopped while its type is retained (semantics already frozen above) |
| Unknown events | The client must tolerate and ignore them (client requirements above); an old SDK not erroring on a new event = the compatibility backstop of the registration approach |
| WebSocket transport | Left for later extension (transport choice above); it bumps the version when both ends need to be written — the transport is not swapped in place |
| Push reach (APNs/FCM) | A Provider surface (features.md F37 wording), not going through this domain's endpoints; system push only serves as a "wake up and pull" hint |
| Payload compression / batched framing | Adding headers/sections goes through a v1.2 proposal; the v1.1 transport-framing hard requirement does not loosen |
