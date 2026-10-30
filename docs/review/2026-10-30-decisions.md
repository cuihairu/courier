# Delegated Decisions on Blocked/Pending Items (2026-10-30)

> Status: delegated decision (user authorization of 2026-10-14: "decide per recommendation, can be overturned later"); items needing keys/accounts/user-private data are marked "awaiting user input, deferred" and are not forced.
> Scope: all BLOCKED / pending-decision / missing-user-input entries in todo.md; analysis is based on repo facts (file:line), not invention.
> This batch covers 2 entries (F62, F80) — both external-condition blockers; the ledger stays as-is with cards listed verbatim.

---

## F62 Email/gift-pack codes (todo.md:18)

- **Recommended decision**: stay unchartered — awaiting user input (an actual operating-side requirement trigger); evaluation remains complete, no charter this round.
- **Rationale**:
  1. Blocker fact (2026-10-28/29 recheck): repo-wide grep for mail/gift-pack/redeem/promo hits the existing evaluation and contract docs (research/mail-cdk-evaluation.md, features.md §10); the same broad terms also hit ledger/status prose in todo.md:18 and docs/research/implementation-status.md:110, none of which is a new operating requirement — no operating requirement has been raised and there is no verifiable external evidence of one.
  2. Evaluation is already done and favorable (contract surface highly isomorphic, low cost to charter per the sketch) — nothing further to analyze in-repo; the only missing input is the requirement trigger itself, which is a user/operating-side decision.
  3. Cost of chartering now: implementation surface (contract + gateway + six SDKs) would be built against an unconfirmed requirement — risk of rework if the operating side wants a different shape.
  4. Benefit of waiting: zero wasted work; the sketch makes chartering fast once a real requirement lands.
  5. Risk: low — no gate depends on F62.
- **Immediate action**: none (record decision).
- **Not doing / deferred**: no charter, no contract draft, no SDK surface; freeze-and-charter per the sketch once the operating-side requirement triggers.
- **Teaching note**: "charter" here means formally starting the feature (freeze its design, then build it). The homework is already done — we know roughly what it would cost. What we do not have is anyone actually asking for the feature. Building a feature nobody asked for is the classic way to waste engineering time, so it stays parked until a real request arrives.

## F80 UE/Cocos persistent token-store adapters (todo.md:22)

- **Recommended decision**: uphold the existing delegated decision (2026-10-19): **not earlier than the engine surface** — land together with the UE HttpModule integration surface, not on its own; stays unchartered until that surface starts or an integrator self-adapts.
- **Rationale**:
  1. Blocker fact (2026-10-28 recheck): engine side still unmoved — no .uplugin/.uproject in sdks/ue, no HttpModule adapter or UObject wrapper landed; needs a real UE engine environment, which the repo does not have.
  2. The ledger already carries a delegated decision for this entry (2026-10-19 patrol dispatch decision clause, taking the ledger's own suggestion); today's batch confirms it rather than re-litigating it.
  3. Cost of landing early: a persistent token-store adapter for UE would be written against an engine integration surface that does not exist yet — unverifiable and likely to be reshaped by the real integration.
  4. Benefit of waiting: integrators can already fill the gap via their own TokenStore implementation (noted in layers.md); Cocos ships MemoryTokenStore plus its WeChat persistent adapter (platform/wechat/tokenStore.ts), and the Laya adapter belongs to the laybox SDK (sdks/laybox/src/platform/laya/tokenStore.ts).
  5. Risk: low — documented extension point exists; no gate depends on F80.
- **Immediate action**: none (record decision).
- **Not doing / deferred**: no .uplugin/.uproject scaffolding, no HttpModule adapter, no UObject wrapper; re-evaluate once engine-side integration starts or an integrator self-adapts.
- **Teaching note**: a "persistent token store" is where a game engine keeps login tokens so players stay signed in after a restart. Writing one for Unreal before any Unreal integration exists is like building a garage before owning the car — the shape of the car determines the shape of the garage. The engine integration is the car; it has not arrived, so the adapter waits.

## Summary

| Entry | Blocker type | Recommended decision | Immediate action |
|---|---|---|---|
| F62 | awaiting operating requirement (user decision) | stay unchartered | none |
| F80 | awaiting UE engine integration surface | uphold "not earlier than engine surface" | none |

> This batch is a decisions record with no new implementation; unlock conditions and acceptance criteria are unchanged.
