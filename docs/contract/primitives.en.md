
# Primitive Contract: Request / Response / Pagination / Timestamp / ID / TraceID

> Status: Frozen v1 (2026-10-08, frozen at the M0 cross-platform review); **v1.1** (2026-10-10, batch 18 added the "Cancellation and Timeouts" and "Concurrency Discipline" sections — a compatible addition, additive-only). This document defines the wire-format primitives shared by all domain contracts; domain contracts must not invent their own.

## Response Envelope

Success:

```json
{ "data": { } }
```

Failure (structure in [errors.md](./errors.en.md)):

```json
{
  "error": { "code": "AUTH_TOKEN_EXPIRED", "message": "session expired", "retryable": false },
  "traceId": "4bf92f3577b34da6a3ce929d0e0e4736"
}
```

- A successful response has only `data`; a failure response has only `error` + `traceId`; the two are mutually exclusive.
- A list response is `data` containing a pagination object (see below).

## ID

- Always strings, server-generated, UUIDv7 (time-ordered, good for indexing).
- Auto-increment IDs are not exposed externally.
- Domain prefixes are optional (e.g. `acc_`, `ses_`, `ply_`, `tkt_`, `ord_`), declared by the domain contract; an ID without a prefix is still valid.

## Timestamp

- RFC 3339 UTC, millisecond precision: `2026-10-08T03:20:00.123Z`.
- Wire keys are always camelCase, ending in `At` (`createdAt`, `expiresAt`) or ending in `Ms` (`timeoutMs`).
- Per-platform property names follow language convention (C# `CreatedAt`, etc.); serialization keys follow this document.

## TraceID

- 32 hex characters, compatible with W3C Trace Context (`traceparent` header support is optional).
- `traceId` appears only in the **failure response** body (see the mutual-exclusion rule in "Response Envelope"); successful responses do not carry it.
- Requests may carry `X-Request-Id` (client-generated, an idempotency/correlation ID); the gateway echoes it back verbatim in a response header; **it is not mixed with traceId**.
- When reporting a problem, the client includes the `traceId` of the failure response, or uses `X-Request-Id` to correlate request timing.

## Pagination

Cursor-based, no page numbers:

- Request: `?limit=20&cursor=<opaque>`; `limit` defaults to 20, maximum 100.
- Response:

```json
{ "data": { "items": [ ], "nextCursor": "<opaque>" } }
```

- An empty or missing `nextCursor` = last page. The cursor is an opaque string; clients must not parse it.

## Null Values and Enums

- An optional field that is absent is simply unset; "null" and "absent" are not distinguished, and the client treats it as absent.
- Boolean fields are two-state, not three-state.
- Enums are always UPPER_SNAKE_CASE strings (`"PENDING_REVIEW"`), mapped to each platform's own enum; magic numbers must not go over the wire.
  The sole exception: scope's `env` values are lowercase (`dev`/`staging`/`prod`, see [scope.md](./scope.en.md)).

## Cancellation and Timeouts (v1.1)

The already-established behavior of each platform SDK, frozen as contract semantics:

- The client may cancel an in-flight request at any time (timeout, host abort); **cancellation is a client-side action the server is unaware of** — the request may already have completed on the server. Cancellation does not constitute a rollback; the server-side result is authoritative via the fetch endpoint (the same discipline as "push is a hint, fetch is authoritative").
- Timeout is the common form of cancellation; the timeout value is client-configurable, and the contract does not freeze a number.
- On the client, cancellation uniformly surfaces as a transport failure (`TransportError`, mapped to `COMMON_UNAVAILABLE`, `retryable=true`), **not a domain typed error** — consumers that branch only on the wire code need no new branch.

## Concurrency Discipline (v1.1)

- **Retry**: errors with an envelope `retryable=true` and transport failures (timeout/network loss) may be retried automatically, backing off first by the `Retry-After` seconds; retries **apply to write requests too** — the request may not have reached the server, and the risk of a double send is absorbed by the server's idempotency surface (the payment callback signature is idempotent so it never ships twice, and the management plane recovers dropped orders through reconciliation), with the client confirming the truth via the fetch endpoint (perceiving shipment after an order = polling `DELIVERED`, see [payment.md](./payment.en.md)).
- **Single-flight session refresh**: when concurrent requests receive `AUTH_TOKEN_EXPIRED`, only one refresh is allowed in flight; the rest wait and reuse the same result; after the refresh completes, each request **replays at most once and this does not count against the retry budget**.
- **Invalidated session means a clean sweep**: `AUTH_REFRESH_REUSED` or `AUTH_TOKEN_REVOKED` → the client must clear its local session back to the unauthenticated state, and must not retry or reuse the old credentials.
- **Multiple sessions are legal**: concurrent logins of the same account on multiple devices/platforms do not revoke each other; revocation comes only from an explicit sign-out, device unbinding, or refresh-replay detection (auth.md "Data Model").
- **No ordering guarantee**: the server does not define an arrival order among concurrent requests; write sequences that require ordering are serialized by the caller (e.g. appending a message after creating a ticket: `POST /tickets` first, then `POST /messages` after success).

---

## Extension Reserve (compatibility stance, note added 2026-10-11)

> This section is an added note and does not touch the frozen lines above. The primitives are the compatibility bedrock of all domain contracts; extension handling follows [versioning.md](./versioning.en.md).

| Possible extension | Compatibility handling |
| --- | --- |
| Adding fields to requests/responses | **Adding only optional fields** = compatible; adding a required field = breaking, bump the version |
| Unknown fields | Clients must tolerate (ignore) them; this applies across the whole contract — no platform may refuse to parse because of an unknown field |
| New resource type | A new ID prefix (e.g. `xxx_`) = compatible, just register it in this section; changing a prefix's semantics = bump the version |
| New endpoints / new domain routes | Compatible, frozen along with the domain contract |
| Changes to pagination / timestamp / envelope semantics | Breaking = bump the version (e.g. cursor rules, RFC 3339 precision) |
