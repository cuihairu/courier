# Courier SDK Contract(M0 立宪)

> **状态**:Draft v0 — 待冻结评审;冻结后升 v1 并进入变更流程管理。
> **原则**:**SDK Contract > Everything**——契约是 Courier 的宪法。Gateway、Provider、Core、Adapter、生态后端(herald / croupier / chirp / oddsmaker / 自建 accounts)都只是契约的**实现者**;新增引擎 = 新增 Adapter,而不是重新设计 SDK。

## 为什么立宪

Courier 的价值不在某个具体业务,而在「同一套能力接口,跨引擎跨平台行为一致」。这份一致性只能由契约保证:

```text
              Courier
                 │
       ┌─────────▼─────────┐
       │   SDK Contract    │
       │  Auth / Session   │
       │  Player / App     │
       │  Announcement     │
       │  Support / Pay    │
       │  Message / …      │
       └─────────┬─────────┘
        ┌────────┼────────┐
        ▼        ▼        ▼
      Unity      UE      Cocos  …
```

Gateway、Provider、herald、Chirp、Croupier、Oddsmaker 全部只是「契约的实现者 / 后端提供者」,不是 Courier 的核心。

## 宪法原则

1. **契约先行**:任何跨端能力,先改契约、评审冻结,再动实现。M1 Auth 开工前,M0 契约必须冻结。
2. **唯一事实源**:本目录是全部跨端类型、错误码、事件、语义的唯一事实源;各端 SDK 不得私造字段(设计边界「无 any/unknown 敷衍」的落点)。
3. **跨端一致**:同一语义在 C# / C++ / TypeScript / GDScript 里命名风格各异、行为完全一致。
4. **供应商不可知**:契约描述「能力」,不描述「哪家实现」——公告契约不知道 herald 存在。默认供应商是配置,不是契约。
5. **默认关闭(红线)**:采集类、合规类能力(Diagnostics / RealName)契约内建红线——默认关闭,显式开启才工作,开启必须明示数据范围。
6. **字段最小化**:敏感字段能不采就不采;每个涉及个人数据的契约必须同时写「不采集什么」。
7. **向后兼容**:已冻结契约只加不改;破坏性变更必须升版本(见 [versioning.md](./versioning.md))。

## 契约分层

| 层 | 内容 | 冻结点 |
| --- | --- | --- |
| 基元契约 | [primitives](./primitives.md) / [errors](./errors.md) / [scope](./scope.md) / [versioning](./versioning.md) / [events](./events.md) | M0 |
| 域契约 | [realname](./realname.md) / [branding](./branding.md) / [diagnostics](./diagnostics.md) | 随本批产出初稿,实现前冻结 |
| 域契约(后续) | auth / announcements / support / config / payment | 各里程碑开工前冻结(M1:auth) |

## 变更流程

1. 提案:在本文档对应文件修改并标注版本草案(Draft)。
2. 评审:跨端负责人过一遍(至少 Unity + 一非 C# 端)。
3. 冻结:去掉 Draft 标注,记入版本号。
4. 同步:各端 SDK 按契约对齐,契约测试通过才算同步完成。
5. 破坏性变更走 [versioning.md](./versioning.md) 的升版流程,不做原地改写。

## 索引

- [primitives.md](./primitives.md) — Request / Response / Pagination / Timestamp / ID / TraceID
- [errors.md](./errors.md) — 错误体、错误码注册表、HTTP 映射、降级信号
- [scope.md](./scope.md) — game_id + env 隔离
- [versioning.md](./versioning.md) — 三层版本与兼容规则
- [events.md](./events.md) — 事件信封、生命周期事件、推送通道
- [realname.md](./realname.md) — 实名认证(默认关,Provider 热插拔)
- [branding.md](./branding.md) — 品牌定制(Core 无品牌逻辑)
- [diagnostics.md](./diagnostics.md) — 诊断(默认关,四类能力上报 Schema)
