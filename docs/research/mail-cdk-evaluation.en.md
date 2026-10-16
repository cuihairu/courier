[English](./mail-cdk-evaluation.en.md) | [中文](./mail-cdk-evaluation.md)

# F62 Mail / Redeem Codes: Evaluation Conclusion

> Status: **evaluation complete, not initiated** · 2026-10-16 patrol FW1016D delegated decision · per [features.md §10](./features.en.md) "only F62 mail / redeem codes warrants priority evaluation".
> Bottom line up front: **the contract surface is highly isomorphic to existing surfaces, so the initiation cost is low; but no ops need has triggered, so it stays not-initiated. The unlock condition is unchanged (triggered by an actual ops-side need).** After this evaluation, initiating = freezing per the sketch, not designing from zero.

## Delegated Decision Record (2026-10-16, patrol FW1016D)

- **Decision**: complete the evaluation (this document), do not initiate.
- **Reason**: F62 is a "demand trade-off" blocker (needs an ops-demand trigger); the features.md §10 conclusion "this section is gameplay, not services; everything stays under discussion to prevent scope creep" is the standing verdict, and this evaluation does not reopen it — the evaluation itself is the legitimate action that section explicitly allows ("only F62 warrants priority evaluation").
- **Overturn path**: an ops need lands → initiate by freezing per this document's sketch; the decision-maker may also reject the direction outright, and this document is simply archived.

## Requirement Breakdown (ops side sends, player side receives)

| Capability | Description | Relation to existing surfaces |
| --- | --- | --- |
| Server-wide mail | Ops broadcast announcement-type mail / mail with attachments | Isomorphic to announcements (herald): published once, pulled by players via cursor |
| Individual mail | Targeted compensation / support cover letters | Isomorphic to ticket attachments (croupier): owner index + attachments |
| Redeem codes (cdk) | One code one redemption / one code limited redemptions | Idempotent claiming is isomorphic to the payments domain (payment) |

## Contract Sketch (for evaluation, not frozen)

```text
mail domain (mounted at /v1/mail/*):
  GET  /v1/mail              cursor pagination + unread count
  GET  /v1/mail/{id}         detail
  POST /v1/mail/{id}/claim   attachment claim (idempotent: repeated claims give the same result)
  DELETE /v1/mail/{id}       read/delete (soft delete; cursor semantics unchanged)
  push event mail.updated     reuses the push channel (same shape as config.updated / realname.consent.updated)

cdk domain (mounted at /v1/cdk/*):
  POST /v1/cdk/redeem        { code } → redemption result (idempotency key = account + code)
```

- Server-wide mail **does no server-side fan-out**: the player side filters by audience (all | segment) at pull time, same as announcements; this avoids O(all players) write amplification.
- The attachment model aligns with the payment domain's currency/item semantics and **does not introduce a second unit of value** (value granting still goes through the payments domain's semantics; mail is only the carrier).
- Redeem-code anti-abuse: covered by the existing global rate limiting + failure backoff; a code is one-time (atomic redemption) or limited-redemption (counting); the semantics belong to Provider.

## Isomorphism (initiation cost)

| Initiation factor | Reuse | New surface |
| --- | --- | --- |
| Pagination | primitives cursor | none |
| Push | chirp SSE / the announcement push channel | the `mail.updated` event name |
| Idempotent claiming | payment claim semantics | the mail/claim idempotency key |
| Audit | the `gateway/audit.AuditSink` seam | mail.claimed / cdk.redeemed event types |
| Six-platform generation | tools/contractgen fully automatic | fixture additions |
| Two-implementation closure | the nine-domain two-implementation pattern | 2 Provider implementations per domain |

## Boundaries (what is not being done)

- **Granting / the ops surface does not enter the gateway**: any ops-side UI is a [roadmap non-goal](../roadmap.en.md); sending mail, code-batch generation, and redemption reporting belong to live-ops tooling / the integrator, via Provider S2S or direct writes to their storage.
- Mail has no rich-text rendering spec (the client UI is free); no message semantics beyond cross-platform read sync (boundary vs. messages/chirp: mail is the ops-reach storage surface; chirp is the session push channel).

## Step Checklist Once Initiation Is Triggered

1. Sketch review → freeze mail.md / cdk.md (error codes go into errors.md v9).
2. fixtures → `go run ./tools/contractgen -write` to regenerate the six platforms.
3. Gateway: two Provider implementations with the same shape + audit events.
4. Six-platform SDK + UI (mail list / claim / redemption entry) + e2e.
5. Ledger lands batch by batch following the usual batch conventions.
