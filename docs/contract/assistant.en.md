[English](./assistant.en.md) | [中文](./assistant.md)

# Assistant Contract (Assistant)

> Status: **Frozen v1** · Milestone M4 · frozen 2026-10-09.
> The domain prefix `ASSISTANT` is registered ([errors.md](./errors.en.md)); once frozen it is additive-only; breaking changes bump the version (see [versioning.md](./versioning.en.md)).

## Capabilities and Scope

- Capability domain `assistant` (`providers.CapAssistant`), routes `/v1/assistant/*`; default provider **sage** (self-built FAQ retrieval), replaceable and switchable off.
- Player side: FAQ knowledge-base retrieval Q&A; on a hit it answers, and when it cannot answer, one-tap handoff to a human (reusing the [support.md](./support.en.md) ticket flow).
- **An LLM interface is reserved but not depended on by default**: the v1 default implementation is FAQ retrieval (zero external dependencies, works offline, predictable); integrators may swap in an LLM provider without changing the contract's semantics (the consuming side does not perceive the implementation).
- This contract **does not cover**: LLM prompt engineering, model selection, multi-turn conversation memory (a matter between the integrator and the LLM provider they wire in).

## Design Trade-off (Why Answers Come from the Knowledge Base Instead of Generation)

In the game customer-support scenario, answers must be **auditable, reproducible, and hallucination-free**; FAQ knowledge entries are registered by operations ([support.md](./support.en.md) faq data model), a retrieval hit returns the original entry, and answering never generates new content. The details of retrieval scoring stay out of the contract (implementation freedom); the only invariant pinned down: **a hit entry's question/answer is returned verbatim, never rewritten**.

## Endpoint Overview (mounted at `/v1/assistant/*`)

| Endpoint | Auth | Description |
| --- | --- | --- |
| `POST /v1/assistant/query` | Bearer | Ask: hit → answer entry; miss → a signal that handoff is available |

- Auth, scope headers, and the envelope are the same as [auth.md](./auth.en.md) / [primitives.md](./primitives.en.md).
- Stateless: the server stores no session; multi-turn is done by the integrator carrying the full `query` text each time (the contract does no conversation-history transport).

### `POST /v1/assistant/query`

Request body (bare object, outside the envelope):

| Field | Type | Constraints |
| --- | --- | --- |
| `text` | string | 1–500 characters (judged after server-side trimming) |

```json
{ "text": "怎么找回账号" }
```

Success response `data`:

| Field | Type | Description |
| --- | --- | --- |
| `matched` | bool | Whether a knowledge entry was hit |
| `answer` | object? | The entry snapshot on a hit (see below); absent on a miss |
| `suggestTransfer` | bool | Must be `true` on a miss (the UI shows "transfer to a human" based on it); `false` on a hit |
| `transferTicket` | object? | (reserved) `{ "endpoint":"POST /v1/support/tickets" }`; in v1 the client calls the support domain directly and the server does not backfill |

```json
{ "matched": true,
  "answer": { "id": "faq_a1b2c3", "question": "怎么找回账号",
              "answer": "在登录页点击『忘记密码』...", "keywords": ["找回", "账号"] },
  "suggestTransfer": false }
```

On a miss:

```json
{ "matched": false, "suggestTransfer": true }
```

- **A miss is not an error**: 200 + `matched:false` is returned (the same philosophy as support FAQ retrieval returning an empty result — the UI presents a guiding path instead of an error).
- Empty `text` (after trimming) or missing `text` → 400 `COMMON_INVALID_ARGUMENT`.
- Hit-scoring rules, tie-breaking, and tokenization are implementation details and stay out of the contract; the only invariant = the returned `answer` content is a verbatim snapshot of the knowledge entry.

## Error Code Domain Table (prefix `ASSISTANT`)

v1 adds **zero new error codes**: everything reuses the generic codes (argument errors `COMMON_INVALID_ARGUMENT`, unauthenticated `AUTH_*`, degradation `COMMON_CAPABILITY_DISABLED`). The domain prefix `ASSISTANT` is registered and reserved for the future (e.g. retrieval rate-limit refinements); additions must go through the [errors.md](./errors.en.md) change process.

## Client Requirements (per-platform SDKs)

- Capability not wired in (501 `COMMON_CAPABILITY_DISABLED`) → the query returns null and the UI hides the assistant entry (consistent with the degradation of every domain).
- `matched:false` and `suggestTransfer:true` → the UI shows "transfer to a human": file a ticket via the support domain ([support.md](./support.en.md)); after the ticket is filed, reuse the `support.ticket_replied` realtime flow.
- Overlong/empty `text` is a caller error (the SDK throws 400 directly); the UI should limit input length locally first.
- No local cache: answers change as the knowledge base updates, so nothing is cached (unlike config — config is a versioned snapshot, assistant is an ad-hoc query).

## Extension Reservations (Note Added 2026-10-11)

| Possible Extension | Compatible Handling |
| --- | --- |
| LLM implementation swap | The interface is reserved and not depended on by default (the Capabilities section above); swapping the provider = swapping the Provider configuration, contract semantics unchanged (the consuming side does not perceive the implementation) |
| Ticket-handoff closed loop | The `transferTicket` reserved slot (the response field above); v1 clients call the support domain directly, server backfill = adding a field in v1.2 |
| Multi-turn sessions | Add-optional-`conversationId` proposal; v1's single-turn Q&A semantics unchanged, and the no-local-cache wording is re-reviewed with the proposal |
| Knowledge-source extension (beyond FAQ) | The sage Provider surface; the player contract only ever sees the query/answer shape; hit-scoring rules stay out of the contract (frozen above) |
| Domain-scoped fine-grained rate limiting | The `ASSISTANT` prefix is registered but unused (the error-code section above); additions must go through the errors.md change process |
