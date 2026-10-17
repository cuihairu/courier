
# Versioning Contract

> Status: Frozen v1 (2026-10-08, frozen at the M0 cross-platform review).

## Three-Layer Versioning

| Layer | Carrier | Rule |
| --- | --- | --- |
| Contract version | The header of each document in this directory (semver) | Semantic; evolves through the change process after freezing |
| API path version | `/v1/…` | The path is bumped only for breaking changes; within `/v1` only compatible changes are made |
| SDK version | Each platform's package version (semver) | Bound to the compatibility matrix (see below) |

## Compatibility Rules (after freezing)

| Change | Compatibility |
| --- | --- |
| Adding an optional field | Compatible |
| Adding an error code | Compatible (clients must tolerate unknown codes and fall through to the catch-all branch) |
| Adding an endpoint | Compatible |
| Adding a required request field | **Breaking** |
| Changing a field's semantics/type | **Breaking** |
| Removing a field/endpoint | **Breaking** |
| Narrowing the value range | **Breaking** |

Hard requirement for clients: **must tolerate unknown fields and unknown error codes** (the cost and duty of forward compatibility).

## Deprecation Process

1. Mark the documentation and responses `deprecated` (may include `Deprecation` / `Sunset` headers).
2. Keep a grace period of at least one full milestone.
3. When removing, bump the API path version, and retire the old version per the committed period.

## SDK Compatibility Matrix

| SDK version | Contract | API |
| --- | --- | --- |
| SDK 0.x (M1–M2 period) | v1.x | /v1 |
| SDK 1.x and later | v1.x | /v1 |

When a breaking contract version bump occurs (Contract v2 / API `/v2`), the SDK major version is bound to the new contract version accordingly.

An SDK major version upgrade must not introduce behavioral divergence within a contract version still supported by the server.

## Extension Reserve (note added 2026-10-11)

> This document is itself the evolution rule; extension handling is already in the body (compatibility matrix, deprecation process, version-bump binding); here we only record the conclusions to avoid drifting from the body.

| Possible extension | Compatibility handling |
| --- | --- |
| New capability domain | A new contract file + a domain prefix registered with [errors.md](./errors.en.md) at freezing (during the Draft period it is only reserved; precedent: oauth.md batch 49); the matrix in this document is not touched |
| Breaking contract evolution | Follows this document's compatibility matrix and version-bump process (step 5 of the change process in [index.md](./index.en.md)); never an in-place rewrite |
| Adding sections to primitives | Precedent: primitives v1.1 added cancellation and concurrency discipline by adding sections, with the freeze date listed separately — adding sections = compatible |
