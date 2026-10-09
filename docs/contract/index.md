# Courier SDK Contract(M0 立宪)

> **状态**:基元五件 **Frozen v1**(2026-10-08,M0 跨端评审冻结;错误码表现已 **v3**——v2 追加 M2 域码,v3 追加 REALNAME 时段/额度码);auth 域契约 Frozen v1(M1);announcement / support / messages 域契约 Frozen v1(M2);realname 域契约 **Frozen v1**(M2 后段);config / app / branding / player 域契约 **Frozen v1**(M3,2026-10-09);diagnostics 域契约 **Frozen v1**(M3,可选包默认全关);assistant 域契约 **Frozen v1**(M4,2026-10-09)。冻结后进入变更流程管理。
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
| 基元契约 | [primitives](./primitives.md) / [errors](./errors.md) / [scope](./scope.md) / [versioning](./versioning.md) / [events](./events.md) | **M0 已冻结 v1** |
| 域契约 | [auth](./auth.md)(**Frozen v1 · M1**);[announcement](./announcement.md) / [support](./support.md) / [messages](./messages.md)(**Frozen v1 · M2**);[realname](./realname.md)(**Frozen v1 · M2 后段**);[config](./config.md) / [app](./app.md) / [branding](./branding.md) / [player](./player.md)(**Frozen v1 · M3**);[diagnostics](./diagnostics.md)(**Frozen v1 · M3 · 可选包默认全关**);[assistant](./assistant.md)(**Frozen v1 · M4**) | auth、M2 三域、realname、M3 四域 + diagnostics + assistant 已冻结;payment 实现前冻结 |
| 域契约(后续) | payment | 各里程碑开工前冻结 |

## 变更流程

1. 提案:在本文档对应文件修改并标注版本草案(Draft)。
2. 评审:跨端负责人过一遍(至少 Unity + 一非 C# 端)。
3. 冻结:去掉 Draft 标注,记入版本号。
4. 同步:各端 SDK 按契约对齐,契约测试通过才算同步完成。
5. 破坏性变更走 [versioning.md](./versioning.md) 的升版流程,不做原地改写。

## M0 冻结评审记录(2026-10-08)

评审视角(规则要求 ≥ Unity + 一非 C# 端,实际四端过读):Unity(C#)、Cocos/小程序(TypeScript)、UE(C++)、Godot(GDScript)。

| # | 发现 | 处置 |
| --- | --- | --- |
| 1 | primitives「每个响应 body 必带 traceId」与「成功响应只有 data」互斥,且与网关实现(成功无 body traceId,`X-Request-Id` 头回传)不一致 | 统一为:traceId 只在失败响应 body;`X-Request-Id` 负责成功请求的关联,不与 traceId 混用 |
| 2 | Timestamp 字段后缀示例为 snake_case(`created_at`),与已冻结 auth 契约及网关的 camelCase wire 不一致 | 修正为 `At`/`Ms` 后缀,wire key camelCase,各端属性名随语言习惯 |
| 3 | events `type` 规则写「小写点分」,示例却含 camelCase 段(`tokenExpired`/`signedOut`/`accountSwitched`/`ticketReplied`/`maintenanceChanged`/`forceUpdate`) | 规则明确为两段小写 + 多词下划线,示例全部归一 |
| 4 | primitives「枚举一律大写下划线」与 scope `env` 小写取值(dev/staging/prod)冲突 | primitives 标注 scope.md 为唯一例外 |
| 5 | versioning 兼容矩阵「SDK 0.x 绑定 Draft v0」与 M0 冻结先于 M1 开工的事实矛盾 | 矩阵改为 SDK 0.x 绑定 Contract v1.x |
| 6 | index 索引与分层表未收录已冻结的 auth.md | 索引与契约分层表归位 |

结论:基元五件升 **Frozen v1**;realname / branding / diagnostics 仍为 Draft 初稿(实现前冻结);auth 已于 M1 冻结。评审后基元五件进入变更流程管理,只加不改。

## 索引

- [primitives.md](./primitives.md) — Request / Response / Pagination / Timestamp / ID / TraceID
- [auth.md](./auth.md) — 认证与会话(Frozen v1 · M1)
- [announcement.md](./announcement.md) — 公告(玩家侧只读投影;Frozen v1 · M2)
- [support.md](./support.md) — 客服工单与 FAQ(Frozen v1 · M2)
- [messages.md](./messages.md) — 推送通道(SSE,会话复用;Frozen v1 · M2)
- [errors.md](./errors.md) — 错误体、错误码注册表、HTTP 映射、降级信号
- [scope.md](./scope.md) — game_id + env 隔离
- [versioning.md](./versioning.md) — 三层版本与兼容规则
- [events.md](./events.md) — 事件信封、生命周期事件、推送通道
- [realname.md](./realname.md) — 实名认证(默认关,Provider 热插拔;Frozen v1 · M2 后段)
- [config.md](./config.md) — 远程配置(键值+版本+条件灰度投影;Frozen v1 · M3)
- [app.md](./app.md) — 应用状态(版本/维护/环境;Frozen v1 · M3)
- [branding.md](./branding.md) — 品牌定制(Core 无品牌逻辑;Frozen v1 · M3)
- [player.md](./player.md) — 玩家档案(账号↔角色映射;Frozen v1 · M3)
- [diagnostics.md](./diagnostics.md) — 诊断(默认关,四类能力上报 Schema;Frozen v1 · M3)
- [assistant.md](./assistant.md) — 小助手(FAQ 检索问答 + 转人工;Frozen v1 · M4)
