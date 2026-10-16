[English](./index.en.md) | [中文](./index.md)

# Contract Fixtures (machine-readable snapshots)

> This directory holds **machine-readable snapshots** of the contract documents, consumed by [tools/contractgen](https://github.com/cuihairu/courier/tree/main/tools/contractgen) to generate the six-platform contract sources (C# / TS / C++ / GDScript); any drift turns the tests red. Convention: **documents first, fixtures follow, generated output is never hand-edited**.

| File | Content | Consumers |
| --- | --- | --- |
| `errors.json` | Error code table (version/http/retryable + domain prefixes) | contractgen generates each platform's `ErrorCode`/`ErrorSpecs`; the gateway `codeTable` sync test |
| `scope.json` | Scope header constants and the env enum | Scope constant generation for every platform |
| `dto.json` | Envelope and pagination wire keys | `Envelope`/`Page` type generation for every platform |

Change process: edit [errors.md](../errors.en.md) and the other documents → update the fixtures in sync (versions only add) → `go run ./tools/contractgen -write` to regenerate → the contract tests for every platform must be green before the sync counts as complete (see [Constitution · Change Process](../index.en.md)).
