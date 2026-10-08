# Courier Unity SDK (C#)

> M1 目标端。UPM 包结构,三包分离:Core / Service / UI(可选包)。

## 计划结构

```text
Runtime/
  CourierClient.cs        入口：Init(gameId, env, endpoint)
  Core/                生命周期状态机、DTO、token 存储接口、重试、错误枚举（M1）
  Identity/            login / logout / bind / device（M1）
  Session/             token 轮换与会话状态（M1）
  Player/              profile / gamePlayer（M3）
  App/                 version / checkUpdate / checkMaintenance / config / branding（M3）
  Communication/       announcements / messages / push（M2）
  Support/             createTicket / listTickets / getFaq（M2）
  Payment/             createOrder / queryOrder（M5，仅契约面）
  RealName/            submit / status（可选，合规，M2 后段）
  Diagnostics~/        可选包程序集（默认全关，no-op）
Editor/
UI~/                   可选 UI 包：LoginPanel / AnnouncementPanel / SupportPanel（消费 Branding）
Samples~/
package.json
```

## 约定

- DTO 以 `../../docs/contract/` 为唯一事实源，与其他端字段逐一对齐；禁止 `object`/`dynamic` 敷衍。
- 生命周期状态机（Uninitialized→…→PlayerReady、Suspended/Resuming）在 Core 实现，平台信号由 Adapter 注入。
- Adapter 禁带业务：只做网络（UnityWebRequest）、存储（加密存储，不用明文 PlayerPrefs）、生命周期、主线程调度。
- UI 包为独立可选 UPM 包，消费 Branding 下发；不装 UI 包不影响功能面。
