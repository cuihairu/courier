[English](./roadmap.en.md) | [中文](./roadmap.md)

# Courier Roadmap

> Principle: every milestone is end-to-end usable (gateway + at least one SDK client + acceptance), no "half-finished features". **Contract first: the previous milestone's contract is frozen before the current milestone starts.** Batch cadence see [todo](./todo.en.md); the five-layer definitions see [layers](./layers.en.md).

## M0 — SDK Contract Foundation (completed, 2026-10)

**Scope**

- Freeze the primitives contracts: Error / Request / Response / Pagination / Timestamp / ID / TraceID / Scope / Version / Event (`docs/contract/`, initial draft established).
- Initial drafts of the domain contracts: RealName / Branding / Diagnostics (red lines and Provider semantics built in).
- Cross-engine review: at least Unity plus one non-C# engine go through it; once frozen, Contract is promoted to v1.

**Acceptance**

- Unity / Cocos clients can each hand-write their DTOs and error enums from the contracts without ambiguity; the contract test skeleton is in place.

## M1 — Auth (foundation) (completed, 2026-10)

**Scope**

- gateway slimming skeleton: directory restructuring auth / session / scope / routing / aggregation / middleware / providers; Provider registry; unconfigured Provider → `COMMON_CAPABILITY_DISABLED`.
- AccountProvider default implementation (self-built): email+password and guest (device) login, access/refresh rotation, revocation, device binding, basic rate limiting. Data tables: `accounts`, `account_credentials`, `sessions`, `devices`.
- Unity SDK first: lifecycle state machine + Identity/Session domains + secure token storage + auto-refresh; Core/Service/UI three-package layout in place (the UI package may be empty).
- Contract: `docs/contract/auth.md` frozen.

**Acceptance**

- The Unity sample completes the full chain: guest login → bind email → logout → refresh rotation → denied after revocation.
- Concurrent login rate limiting takes effect; errors are structured JSON (code/message/traceId, enums consistent across clients).

## M2 — Announcements + Support (compliance back half: real name) (completed, 2026-10)

**Scope**

- AnnouncementProvider (herald/croupier player-side projection), SupportProvider (croupier tickets/FAQ), MessageProvider (chirp session push channel; can be off = pull-only).
- First release of the optional UI packages (announcement board / support page), consuming Branding (all default labels at this point).
- **Real name (M2 back half, compliance)**: RealNameProvider interface + hot-swap / degradation chain / circuit breaker landed; off by default; the first implementation is recommended to be self-built verification, with cloud vendors (Alibaba Cloud / Tencent Cloud / Yidun) integrated as needed by adopters.

**Acceptance**

- Ops publishes an announcement → the player client receives the push within 5s and can pull history; the pull-based fallback works when the push channel is off.
- Player submits a ticket → agent replies → visible in real time on the player client.
- Real name: primary Provider circuit-breaks → fallback takes over → total failure transitions to the `PENDING_REVIEW` safe state; off by default throughout, and the data scope is explicitly stated when enabled.

## M3 — Remote Config + Player Profile + App (version/maintenance) + Branding + Diagnostics (completed, 2026-10)

**Scope**

- ConfigProvider: `game_id / env / platform / version / region` conditional matching; `config.updated` push takes effect live.
- Player Profile: account↔character (player) profile interface.
- Courier.App: GetVersion / CheckUpdate / CheckMaintenance / GetEnvironment.
- BrandingProvider shares the same pipeline as config; UI packages fully consume the brand configuration (company name / logo / theme color / support entry); build-time asset guidelines land with the template project.
- Diagnostics optional package: Crash (Sentry / GlitchTip, both self-hostable), OTel Trace (any OTLP collector), performance metrics, analytics events; all off by default, and unconfigured means no-op with zero overhead.

**Acceptance**

- Modify remote config → clients matching the conditions take effect live; non-matching ones are unaffected.
- Maintenance mode on → new sessions receive `APP_MAINTENANCE`; brand configuration change → UI packages hot-swap; the four diagnostics categories report nothing by default, and each category takes effect as it is enabled.

## M4 — Assistant (completed, 2026-10)

**Scope**

- Retrieval-based Q&A over the FAQ knowledge base (the default Provider can connect to croupier faq); an LLM interface is reserved but not depended on by default; one-click handoff to a human agent when it can't answer (reusing the M2 pipeline).

**Acceptance**

- FAQ hit rate is measurable; the human-handoff pipeline is end-to-end.

## M5 — Payment (Contract only, shape open) (contract and gateway completed, 2026-10; real-channel sandbox follows the deployment side)

**Scope**

- Payment Contract: `Order / Purchase / Receipt` + security boundaries; the shape is chosen by the adopter (Account Wallet / Game Wallet / Direct Purchase / External Payment); the contract makes no unified-wallet assumption.
- Sandbox channel first: place order → callback (signature verification) → delivery → reconciliation, full chain; RiskProvider sits in front (large amounts / abnormal frequency).

**Acceptance**

- Sandbox channel full-chain E2E; forged signatures are all rejected; interrupted orders can be recovered via reconciliation; callbacks trust only the channel signature.

## Follow-up cadence: verification and delivery (review archived 2026-10-11)

> M0–M5 have landed; the focus shifts from "expanding capabilities" to "proving the existing architecture can be used reliably by external game projects" (batch 46). For the full feature-by-feature comparison and gap list, see [implementation status](./research/implementation-status.en.md).

- **Phase A — Implementation status and evidence audit** (started, batch 45): 76 features checked one by one against repository evidence — core 47 (✅45/❌2), Provider 13, under discussion 9, out of scope 7 (as of batch 57, see implementation-status.md); logging a gap does not equal greenlighting it; go through a contract proposal as needed.
- **Phase B — Cross-engine consistency**: lifecycle / cancellation / concurrent refresh / account switching / SSE reconnection behavior scenarios produce the same results on all six clients; verification via install/compile/run on real engine projects (UE engine integration and device debugging follow the deployment side). **Progress** (batch 63): coverage audit of the five scenarios across the six clients completed — lifecycle/cancellation/SSE reconnection are complete on all six; concurrent refresh backfilled: the audit found Unity serial and non-shared (inconsistent with TS/Godot's "concurrent shared single rotation"), fixed by sharing in-flight tasks and constructing real concurrent-overlap acceptance with suspended responses; the UE synchronous core is accepted via reentrancy protection (one rotation sends exactly one request); account switching backfilled at client level (newly added for Unity/Godot, already present for UE/TS); remaining open items = ~~Unity ApplicationLifecycleMonitor adapter-layer tests~~ (closed at batch 81: signal normalization split into the engine-agnostic `LifecycleSignalRelay`, 8 unit tests added to `CoreTests~`, aligned one by one with godot's `test_adapter.gd`; the Monitor keeps only engine callback forwarding) and pure HTTP cancellation (SSE-only on all six, not a cross-engine inconsistency).
- **Phase C — Provider replaceability**: at least two implementations per domain (default + alternative/test double, delivered domain by domain starting with the account domain at batch 18); disable/hot-reload does not break in-flight requests; account/real name/payment define independent safe-switching policies (interface replaceable ≠ data migratable). **Progress** (batch 61): the sensitive-domain switching policy is defined and landed in architecture.md as "Sensitive-domain independent switching policies" (per-domain configuration switching surfaces for account/real name/payment + what is not migrated + evidence index); test coverage for "disable/hot-reload does not break in-flight requests" is complete — real-name side `router` unit tests + e2e, account side newly added `Switchable` concurrent source-switching unit test (-race); remaining open item = second implementation per domain (account domain batch 18, real-name domain batch 64, player profile domain batch 67, push domain batch 68, announcements domain batch 69, assistant domain batch 70, config domain batch 71, payments domain batch 72, support domain batch 73 delivered — the second implementation is proven with same-scenario e2e behavior against the default implementation; all nine capability domains now have two implementations, "at least two implementations per domain" closed).
- **Phase D — Shippable SDK**: minimal examples for all six clients + hands-on quickstart + custom Provider integration guide (overlaps with the batch 44 project; that batch prevails). **Progress** (batch 55): all-six `examples/` + hands-on quickstart + the options checklist (`docs/sdk-selection.md`) landed (`425fe55`/`c5c4814`); the custom Provider integration guide landed (`docs/provider-guide.md`, batch 58) — all four Phase D items closed.

**The seven acceptance questions** (yardstick):

1. When adding a new engine, is it mainly adding an Adapter rather than rewriting business services?
2. When adding a new backend vendor, is it mainly adding a Provider rather than modifying all clients?
3. When a capability is turned off, do the other capabilities keep running normally?
4. Do different engines follow the same set of contracts and lifecycle semantics?
5. Can an adopter replace an ordinary service without modifying business code?
6. For stateful or high-risk capabilities such as account/payment/real name, is the distinction between interface replacement and data migration made explicit?
7. Can a new team integrate independently with only the docs and examples?

## Non-goals (not doing)

- Real-time multiplayer / matchmaking / lobby servers (Nakama / Agones territory).
- Channel-package aggregated joint operations (MSDK / QuickSDK territory).
- Any operator-side UI (default vendor Croupier, self-build possible).
- Self-built long-connection push channel (default Provider chirp, replaceable).
- Social / friends / guilds (to be discussed later).
- Unified wallet assumption (payment shape is open, see M5).
