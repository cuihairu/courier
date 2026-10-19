
# Scope Contract: game_id + env

> Status: Frozen v1 (2026-10-08, frozen at the M0 cross-platform review). The same isolation model as the ecosystem (Croupier / Oddsmaker).

## Rules

- The isolation unit is the pair `game_id + env`, globally unique.
- Specified **once** at SDK initialization (`CourierClient.Init({ gameId, env, endpoint })`), and invisible to callers afterwards.
- **URL paths and payloads never carry scope fields**; scope travels only in headers.

## Transport

| header | Description |
| --- | --- |
| `X-Courier-Game-Id` | Game identifier, attached automatically by the SDK on every request |
| `X-Courier-Env` | Environment enum: `dev` / `staging` / `prod` (registry-based, extensible) |

## Gateway Validation

1. Either header missing or not in the registry → `COMMON_INVALID_ARGUMENT`.
2. The gateway uses the headers as the single source of truth and injects them downstream; downstream implementations must not read scope from the path/payload.
3. Tokens are bound to a scope at issuance; using a token scoped to game A to access resources scoped to game B → `SCOPE_MISMATCH` (an account is a company-level entity; crossing games requires going through login again or a future token exchange, not sharing player-level tokens).

## Relationship to the Account Model

- `account` is a company-level entity that exists across games; `player` is a game-dimension entity.
- Hence in M1 the same account signs in separately per `game_id + env`, producing sessions scoped to each; "one account signs in to all games" means the account is reusable, not that tokens cross games.

---

## Extension Reserve (note added 2026-10-11)

| Possible extension | Compatibility handling |
| --- | --- |
| New environment values (e.g. `qa` / `canary`) | **Clients must not treat env as a closed enum** — handle it as a string and tolerate unknown values; new values are registered with this contract's v1.1 (wire-compatible) |
| New isolation dimension (e.g. company/org-level `orgId`) | v1.1+ adds an optional header; absent = single organization, existing clients are unaffected |
| Cross-game identity linkage | Account reuse is already built in (company-level entity); using tokens across games is breaking semantics and will never be enabled in place |
