
# Payment Contract (Payment) — Open Forms

> Status: **Frozen v1.1** · Milestone M5 · v1 frozen 2026-10-09; **v1.1 (2026-10-09) added the push events** (`payment.paid` / `payment.delivered`, an additive compatible change, see "Push Events").
> The domain prefix `PAYMENT` is registered ([errors.md](./errors.en.md)); this document is the consistency basis between the M5 server (default provider **teller**) and the SDKs on every platform. Once frozen it is additive-only; breaking changes bump the version (see [versioning.md](./versioning.en.md)).

## Capabilities and Scope

- Capability domain `payments` (`providers.CapPayments`), routes `/v1/payments/*`; default provider **teller** (self-built orders/sandbox channel), replaceable and switchable off.
- What the contract pins down is the **order-to-fulfillment core flow** (create order → channel callback → fulfillment → reconciliation); it assumes nothing about the wallet shape; channels and wallets are chosen by the integrator.

### The Four Forms (the order's fulfillment target, open and unassumed)

| form | Meaning | Fulfillment (DELIVERED) means |
| --- | --- | --- |
| `DIRECT_PURCHASE` | Direct purchase | The item/entitlement has been granted in the game |
| `ACCOUNT_WALLET` | Account wallet | The balance has been credited (account dimension) |
| `GAME_WALLET` | Game wallet | The in-game currency has been credited (game dimension) |
| `EXTERNAL_PAYMENT` | External payment | The integrator's own store order has been confirmed (this domain only records it for reconciliation) |

- **No unified-wallet assumption**: how the balance is stored and what the currency is called are the integrator's business; the contract only guarantees that order status and amount facts are queryable and reconcilable.
- A form is merely an enum annotation on the order plus a difference in fulfillment semantics; it produces no different endpoints or state machine.

## Security Boundaries (red lines, implementations must not bypass)

1. **Server-side pricing**: the client's order-request body carries only `skuId`; amount/currency always come from the server's SKU price table — client-quoted prices are untrusted.
2. **Callbacks trust only the channel signature**: the channel→gateway S2S callback must carry `X-Payment-Signature` (HMAC-SHA256 over the raw body, keys configured per channel); a missing/mismatched signature is always 403, and forged signatures are **rejected outright**.
3. **Callback idempotency**: repeated callbacks for the same order (same status) = idempotent acceptance (200), no second fulfillment; fulfillment idempotency is backed by the integrator's fulfillment hook (the contract requires idempotency per order).
4. **Order ownership check**: an order that is not yours is always 404 `PAYMENT_ORDER_NOT_FOUND` (existence is not leaked).
5. **Risk check first**: order creation may go through a RiskProvider first (large amounts / abnormal frequency); rejected → 403 `PAYMENT_RISK_REJECTED` (by default the Provider does not block; integrators wire it in explicitly).
6. **Broken orders recover via reconciliation**: an order whose fulfillment fails stays at `PAID`, never lost; recovery goes through management-plane reconciliation re-delivery (fulfillment is idempotent).

## Data Model

### sku (price-table entry, registered in the management plane)

| Field | Type | Description |
| --- | --- | --- |
| `id` | id | Integrator-defined SKU identifier |
| `productId` | string | Fulfillment target (item/currency-pack ID; semantics belong to the integrator) |
| `form` | enum | One of the four forms |
| `amountCents` | int64 | Server-side price (cents) |
| `currency` | string | ISO 4217 (e.g. `CNY`) |

### order (order)

| Field | Type | Description |
| --- | --- | --- |
| `id` | id | Server-generated, UUIDv7, prefix `order_` |
| `status` | enum | `CREATED` / `PAID` / `DELIVERED` / `CLOSED` (uppercase) |
| `skuId` / `productId` / `form` | — | Settled from the SKU at order time (a snapshot; later SKU edits do not affect already-created orders) |
| `amountCents` / `currency` | — | Same as above |
| `payToken` | string? | Issued **only in the create-order response**: the channel-side payment credential (a simulated credential for the sandbox channel; semantics for real channels belong to the channel adapter) |
| `createdAt` / `updatedAt` | timestamp | RFC 3339 milliseconds UTC |
| `paidAt` / `deliveredAt` | timestamp? | Optional, not sent when unset |

State machine: `CREATED → PAID → DELIVERED` (forward); `CREATED/PAID → CLOSED` (order closed); callbacks against `DELIVERED/CLOSED` orders are idempotently accepted and never rolled back. **Broken-order state = stuck at `PAID`** (paid but not delivered, recovered via reconciliation).

## Endpoint Overview (mounted at `/v1/payments/*`)

| Endpoint | Auth | Description |
| --- | --- | --- |
| `GET /v1/payments/skus` | Bearer | Purchasable SKU list (price-table projection) |
| `POST /v1/payments/orders` | Bearer | Create an order (server-side pricing; risk check first) |
| `GET /v1/payments/orders` | Bearer | My order list (`updatedAt` descending, paginated) |
| `GET /v1/payments/orders/{orderId}` | Bearer | Order detail (status polling; delivery awareness relies on it) |
| `POST /v1/payments/callback` | **channel signature** | S2S: channel payment callback (HMAC-SHA256; not a player endpoint, no Bearer) |

- Auth, scope headers, envelope, and pagination are the same as [auth.md](./auth.en.md) / [primitives.md](./primitives.en.md).

### `POST /v1/payments/orders`

Request body (bare object): `{ "skuId": "..." }`.

Success response `data` = the order snapshot (`CREATED` + `payToken`). SKU missing/not purchasable → 404 `PAYMENT_SKU_NOT_FOUND`; rejected by risk → 403 `PAYMENT_RISK_REJECTED`.

### `POST /v1/payments/callback` (S2S)

Request header: `X-Payment-Signature: <hex HMAC-SHA256(channelSecret, rawBody)>`; request body (bare object):

```json
{ "orderId": "order_...", "paidAt": "2026-10-09T12:00:00.000Z" }
```

- Signature verification happens **before** body parsing (the raw body participates in the HMAC); on failure → 403 `PAYMENT_INVALID_SIGNATURE`, with no distinction between "wrong key / wrong signature / forgery".
- Acceptance = the order moves to `PAID` → the fulfillment hook runs synchronously → on success it moves to `DELIVERED`; if fulfillment fails the order stays at `PAID` (acceptance is still 200 — the callback itself succeeded; fulfillment is a reconciliation matter).
- Repeated callbacks against `PAID` (called back but not yet delivered) and `DELIVERED` orders = idempotent 200, no second fulfillment.

## Fulfillment and Reconciliation (management plane, not through the gateway's player routes)

- **Fulfillment hook**: injected by Provider construction (integrators fulfill per order: grant items / credit balance / external confirmation); the idempotency requirement is per order.
- **Reconciliation re-delivery**: the management plane enumerates orders stuck at `PAID` and retries fulfillment one by one (where "broken orders recover via reconciliation" lands); in this v1 it is an in-process method; persistence and scheduled reconciliation follow the deployment plane.

## Push Events (added in v1.1)

**Broadcast** over the push channel ([messages.md](./messages.en.md), default Provider chirp); the payload is a minimal hint, **without business fields such as amount/SKU** (the broadcast channel is receivable by anyone; business data belongs to the pull endpoints, filtered by ownership):

| type | Trigger | payload |
| --- | --- | --- |
| `payment.paid` | Order `CREATED → PAID` (channel callback accepted) | `{ "orderId": "order_..." }` |
| `payment.delivered` | Order moves to `DELIVERED` (synchronous fulfillment on the callback; includes re-deliveries from reconciliation recovery) | `{ "orderId": "order_..." }` |

- Semantics as in [events.md](./events.en.md): at-most-once + pull as the fallback — the push is only a hint that "it is time to pull"; on receipt the client calls `GET /v1/payments/orders/{orderId}` (ownership check: 404 when it is not yours; the broadcast leaks nothing beyond existence). Idempotent repeated callbacks do not repeat the push (it reports state transitions only, not acceptances).
- Delivery-awareness upgrade: **the push is a hint, polling is authoritative** — when not connected to the stream / on dropped frames, polling (`GET orders/{id}`) remains a fully usable path; no realtime expectations are made.

## Error Code Domain Table (prefix `PAYMENT`, errors.md v4)

| code | HTTP | retryable | Description |
| --- | --- | --- | --- |
| `PAYMENT_SKU_NOT_FOUND` | 404 | false | SKU missing or not purchasable |
| `PAYMENT_ORDER_NOT_FOUND` | 404 | false | Order missing or not owned by the current player |
| `PAYMENT_RISK_REJECTED` | 403 | false | Rejected by the risk pre-check (large amount / abnormal frequency) |
| `PAYMENT_INVALID_SIGNATURE` | 403 | false | Channel callback signature verification failed (forgeries are always rejected) |
| `PAYMENT_ORDER_STATE` | 409 | false | Illegal transition in the order state machine (protection against management-plane misoperation) |

## Client Requirements (per-platform SDKs)

- Capability not wired in (501 `COMMON_CAPABILITY_DISABLED`) → every query returns null; hide the store/top-up UI.
- **Delivery awareness = the push is a hint, polling is authoritative** (from v1.1 there are the `payment.paid`/`payment.delivered` pushes; when not connected to the stream / on dropped frames, polling the detail endpoint remains fully usable; no realtime expectations).
- `payToken` appears exactly once, in the create-order response: for the sandbox channel it is passed straight back and the simulated channel completes the payment; for real channels it is consumed per the channel SDK's semantics (the contract does not interpret channel SDKs).
- Render amount display with `amountCents`/`currency` as-is; **never** do any amount math on the client.
- `PAYMENT_RISK_REJECTED` / `PAYMENT_INVALID_SIGNATURE` are not retried; back off on 429 per `Retry-After`.

---

## Extension Reservations (Note Added 2026-10-11)

| Possible Extension | Compatible Handling |
| --- | --- |
| New payment channels (Stripe / PayPal / channel IAP) | Wired in via the Provider adapter layer, **the callback-signature surface does not change** — the channel signature is the authentication; a new channel = new signature-key configuration, contract untouched |
| New order forms | Adding enum values to `orderType` = compatible; the state machine does not branch by form (the four forms being annotations only is an established red line) |
| Subscriptions (F23) | v1.1+ extension slot: subscription state follows the store's server notifications, Provider-ized; the order model is reused, `orderType=SUBSCRIPTION` reserved |
| Wallet balance / spending endpoints | **Not in the v1 player contract** (the four forms are only order annotations); the balance surface belongs to a later domain proposal |
| Refunds / reversals | The reconciliation surface (Reconcile) is already built in; a refund endpoint = a v1.1+ proposal, management plane first |
| Channel-credential evolution | The contract only guarantees that `payToken` appears once in the create-order response; channel SDK semantics stay out of this contract |
