
# Design Decision Record (Delegated-Decision Ledger)

> Rule: this records only "design/requirement tradeoff" delegated decisions (user authorization 2026-10-14; applied in batches from FIRE1019H onward).
> Four categories must always stay with the user and never enter this record: version numbers/releases, license changes, paid procurement, asset licensing.
> Each entry: the decision, a one-sentence rationale, and the rejected options. To overturn a decision, open a new entry referencing the old number — never rewrite history.

## D-001 F33 audit retention and query surface — recommended shape (2026-10-19)

**Decision**: the recommended integrator ingestion shape = the existing `HTTPSink` direct-write → an integrator-side persistence proxy (**append-only, partitioned by day**, JSONL or an object-storage prefix) → a **365-day** baseline retention (statutory/platform requirements take precedence; longer-term data moves to cold archive as needed); queries and compliance exports happen on the **integrator-side read-only surface** — the gateway holds no storage and adds no query endpoints.

**Rationale**: the gateway-side event surface plus the log/direct-write sink halves are fully landed (the F33 in-repo surface, batches 62/65/66/102); what remains is evidence infrastructure, which belongs on the deployment surface with the smallest attack surface; 365 days is the common SOC 2-style evidence baseline, and events carry only account-dimension metadata (the realname.md data red line), so personal-data risk is not amplified.

**Rejected**: ① a DB/table schema inside the gateway — violates the "persistence follows the deployment surface" layering (provider-guide §5) and breaks the gateway's zero-external-dependency posture; ② an audit query API on the gateway — a contract-external endpoint plus a new attack surface; evidence data must not exit through the game gateway; ③ putting retention days into the contract — the jurisdiction-neutrality red line: thresholds stay out of the contract (same precedent as the consent thresholds).

**Blast radius**: one recommended-ingestion clause in provider-guide §4 realname row; the todo.md F33 row closed as a delegated decision (real integration still follows the deployment surface).

## D-002 F77 phase 2 Options Store seam — byte-blob shape (2026-10-19)

**Decision**: `teller.Options.Store` / `telleralt.Options.Store` = an `OrderStore{ Load() ([]byte, error); Save([]byte) error }` **byte-level full-snapshot** seam; `ErrSnapshotMissing` = the first-boot sentinel (equivalent to a missing file); every other read error and corrupted bytes fail closed like a corrupted snapshot (HealthCheck → 503, never silently boots from empty state and loses orders); precedence **Store > StateFile > in-memory** (when both are set the file is neither read nor written); implementers must make Save atomic (no half state on failure).

**Rationale**: the seam exchanges bytes only; order/journal structure and indexes stay inside the Provider — zero new dependencies, zero new attack surface, zero new env vars for both implementations (a pure Go API injection), and an integrator writes a few-line adapter each for DB / Redis / object storage; it is fully isomorphic to the phase-1 StateFile semantics (full state + atomic + fail-closed), reusing all of its test semantics.

**Rejected**: ① per-order KV (Get/Put/Delete) — leaks order keying/sharding into the seam, a larger adapter surface with shape divergence between the two implementations; ② an event-journal Append/Replay interface — forces teller (a mutable state table) onto the event model, asymmetric; ③ a built-in SQL/NoSQL store — would introduce the repo's first external dependency; persistence belongs to the deployment surface.

**Blast radius**: the seam in teller/telleralt + 6×2 tests (three-generation restart / precedence / first-boot sentinel / corrupted snapshot / read failure / write failure); provider-guide §4 payments row; the todo.md F77 row's phase 2 closed — the item is complete.

## D-003 F78 phase 2 management entry — not chartered, SIGHUP is the final state (2026-10-19)

**Decision**: the config-reload entry stays at `SIGHUP` (batch 99 phase 1) as the **final state**; a management HTTP endpoint and file-watching are both **not chartered**. Container-orchestration scenarios inject the signal via exec (docker exec / kubectl exec kill -HUP 1).

**Rationale**: the management endpoint's prerequisite (a management-plane auth contract) does not exist — an unauthenticated reload endpoint turns config injection into an attack surface; both file-watch forms break either the zero-dependency posture (fsnotify) or add resident-background complexity (mtime polling), while SIGHUP already covers the operational need with failure semantics (keep the old table) proven by e2e.

**Rejected**: ① a management HTTP endpoint (e.g. POST /v1/admin/reload) — missing prerequisite contract, new attack surface; ② fsnotify file-watching — would introduce the repo's first external dependency; ③ mtime polling (a Ticker) — a resident goroutine for low-frequency benefit, a poor match.

**Blast radius**: the todo.md F78 row's phase 2 closed (reload entry marked final), the item is complete; the architecture/provider-guide governance text is unchanged (the SIGHUP semantics were registered in batch 99).
