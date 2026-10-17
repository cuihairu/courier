
# Event Contract

> Status: Frozen v1 (2026-10-08, frozen at the M0 cross-platform review; concrete business events are registered with their domain contracts). Defines the event envelope, lifecycle events, and push channel semantics.

## Event Envelope

```json
{
  "id": "evt_01JAXXXX…",
  "type": "announcement.published",
  "occurredAt": "2026-10-08T03:20:00.123Z",
  "payload": { }
}
```

- `type`: `<domain>.<event>`, two lowercase dot-separated segments, with underscores for multi-word names (`lifecycle.token_expired`); the domain name corresponds to the error contract's domain prefix (lowercase form).
- `id`: globally unique; clients deduplicate idempotently by id.
- The `payload` structure is defined by each domain contract and is opaque to the envelope layer.

## Transport Channel

- From M2, WebSocket push, reusing the session channel of the default push Provider (chirp); **the channel itself is also a Provider capability**, and integrators may swap or disable it (once disabled, fetch only).
- **Correction (2026-10-10)**: the transport choice follows [messages.md](./messages.en.md) (frozen in M2) — **SSE** (text/event-stream), not the WebSocket tentatively chosen when this document was frozen (M0); WebSocket is left for a later extension (a version bump is needed when both directions must write).
- Fetch as fallback: every push event has a corresponding fetch endpoint; a lost push is compensated by fetching, and at-least-once is not promised.
- Semantics: at-most-once push + fetch compensation = clients consume with "the list is authoritative, push is a hint".

## Lifecycle Events (the SDK state machine's event surface)

The state machine is defined in [architecture.md](../architecture.en.md) "SDK Lifecycle"; its external events:

| type | Trigger |
| --- | --- |
| `lifecycle.initialized` | Init completes, entering Ready |
| `lifecycle.suspended` | Backgrounding/network loss, entering Suspended |
| `lifecycle.resumed` | Resumed, available again |
| `lifecycle.token_expired` | access expired, automatic refresh begins |
| `lifecycle.signed_out` | Signed out/kicked, back to Ready (unauthenticated) |
| `lifecycle.account_switched` | Account switched |

## Registered Business Events

| type | Domain contract | Description |
| --- | --- | --- |
| `announcement.published` | announcements (M2) | New announcement |
| `support.ticket_replied` | support (M2) | New ticket reply |
| `config.updated` | config (M3) | Remote config changed |
| `app.maintenance_changed` | app (M3) | Maintenance status changed |
| `app.force_update` | app (M3) | Force-update notice |
| `payment.paid` | payment (M5, v1.1) | Order paid (a hint; the payload contains only orderId; [payment.md](./payment.en.md)) |
| `payment.delivered` | payment (M5, v1.1) | Order delivered (including reconciliation recovery; a hint; the payload contains only orderId) |

Registering a new event = a domain contract change; follow the change process in the [master document](./index.en.md).

## Extension Reserve (note added 2026-10-11)

| Possible extension | Compatibility handling |
| --- | --- |
| Registering a new event | Add a row to this table + the owning domain contract defines the payload (additive); the event name rule (two lowercase segments + underscores for multi-word names, normalized in M0 review #3) is unchanged |
| Payload evolution | Add optional fields only (isomorphic to the domain DTO); a semantic change = a new event name, never reusing the old one (deprecating semantics follows the messages registration scheme) |
| Transport carrier | SSE is settled ([messages.md](./messages.en.md)); WebSocket is left for a later extension, changed with a version bump, not in place |
| Unknown events on the client | Tolerate and ignore (the unknown-tolerance principle in versioning.md) — an old SDK encountering a new event does not error, which is the compatibility fallback of the registration scheme |
