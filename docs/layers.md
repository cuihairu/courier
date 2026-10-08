# 五层架构：L1–L5

> **状态**：Draft — 下一阶段的结构基线。原则：**不堆业务，先立骨架**——每层先把职责、边界、接口形状立住，业务能力按 [roadmap](./roadmap.md) M1–M4 分批填充，节奏与验收见 [todo](./todo.md)。

## 分层总览

```text
 游戏客户端：Unity / Unreal / Cocos / Godot / 小程序 / Layabox
 ───────────────────────── 客户端侧 ─────────────────────────
 L1 SDK Contract      语言无关契约：DTO、错误模型、scope 规则（唯一事实源）
 L2 Core              平台无关内核：状态机、token 存储接口、重试、序列化
 L3 Platform Adapter  平台绑定：原生网络栈、安全存储、生命周期、打包形态
 ────────────────────────── 线 路 ───────────────────────────
                      HTTPS JSON（WebSocket 推送 M2+，复用 chirp 会话）
 ───────────────────────── 服务端 ───────────────────────────
 L5 Gateway           唯一入口：鉴权、限流、scope 注入、审计、聚合
 L4 Service Provider  服务编排：auth / 公告 / 客服 / 助手 / 支付的玩家侧投影
 ───────────────────────── 生 态 ───────────────────────────
        herald（公告投递）   croupier（客服/FAQ）   chirp（实时）   oddsmaker（风控）
```

L1–L3 在客户端侧，L4–L5 在服务端侧；L1 被所有层依赖，自己不依赖任何层。

## L1 SDK Contract（契约层）

**职责**

- 定义所有跨端公共类型与行为约定：DTO 字段、统一错误模型（code / message / traceId）、scope 规则（`game_id + env` 全局隔离，URL 与 payload 不得覆盖）、鉴权头与 token 语义、推送事件载荷。
- 以「文档 + Schema」形态存在（`docs/api/`），是唯一事实源；后续演进为 JSON Schema / OpenAPI / IDL，生成或校验各端类型。

**边界**

- 不含任何实现；不含平台细节。
- 不因某端方便而私加字段：变更必须先改契约、评审通过后同步各端（与设计边界 7「无 any/unknown 敷衍」配套）。

**接口形状**

- `docs/api/{errors,scope,auth,announcements,support,assistant,payments}.md`，按域拆分；每个域包含：路由、请求/响应 DTO、错误码、时序。

**现仓映射**：`docs/api/`（待建）；各端 SDK README 的「DTO 以 docs/api/ 为唯一事实源」约定；architecture.md 关键决策「公共 DTO 以契约为唯一事实源，禁止各自发明字段」。

**下一步**：契约三件套 errors.md / scope.md / auth.md（M1 冻结，todo 批次 1）。

## L2 Core（平台无关内核）

**职责**

- 可单测的纯逻辑：DTO 解析与校验、登录状态机（登录 / 登出 / refresh 轮换 / 吊销处理）、token 存储接口、HTTP transport 接口、重试与超时策略、错误归一。
- 每端一个 `CourierClient` 门面，按域暴露 Auth / Announcements / Support / Assistant / Payments。

**边界**

- 不 import 任何平台 API：网络与存储全部通过注入接口（ITransport / ITokenStore / IClock）。
- 不写 UI；不含服务端业务编排；不感知具体生态后端。

**接口形状**

- `CourierClient.Init({ gameId, env, endpoint })` → 按域子模块；域接口与 L1 契约一一对应。

**现仓映射**：`sdks/unity` `Runtime/Core`、`sdks/cocos` `src/core`（规划）；其余端同构。

**下一步**：Unity 端 Core 先行（todo 批次 4），纯逻辑单测覆盖。

## L3 Platform Adapter（平台绑定层）

**职责**

- 把 Core 接到平台能力上：平台原生 HTTP/WebSocket 栈、token 安全存储（实现 Core 的 ITokenStore）、生命周期挂钩（前后台切换、进程重启）、打包形态（Unity UPM 包 / UE 插件 / npm 分发 / Godot 插件目录）。

**边界**

- 不含业务语义；不做协议决定（协议归 L1）。
- 每平台一个适配器；平台特有扩展必须以 Core 接口为基类；跨平台共享逻辑上移 Core，不下沉到某个适配器。

**接口形状**

- ITokenStore 平台实现：Unity 加密存储 / UE 平台凭证 / Cocos localStorage / 微信小程序 storage / Layabox、Godot 平台存储。
- ITransport 平台实现：各平台 HTTP 客户端；M2+ WebSocket。

**现仓映射**：`sdks/{unity,ue,cocos,miniprogram,laybox,godot}`（六端，目前均为规划 README）。

**下一步**：Unity UPM 骨架（todo 批次 5）：package.json + Runtime / Editor / Samples~ 结构。

## L4 Service Provider（服务编排层，服务端）

**职责**

- 面向玩家的能力编排与投影：auth（accounts / sessions 自建）、announcements（herald / croupier 玩家侧只读投影）、support（croupier 工单 / FAQ 投影）、assistant（croupier faq 检索编排）、payments（渠道抽象 + oddsmaker 风控前置）。
- 数据事实留在生态后端；本层只做投影、聚合、编排。

**边界**

- 不复制数据事实（投影可缓存，事实源唯一）。
- 不做入口治理：鉴权 / 限流归 L5，本层信任中间件注入的身份。
- 不服务运营调用方：运营能力一律走 Croupier（设计边界 1）。

**接口形状**

- 每域一个模块包，以显式接口注册进 L5 路由（如 `Register(mux)`）；模块间经接口互调，不经具体类型。

**现仓映射**：`gateway/cmd/gateway/main.go` 的模块挂载点注释（`/v1/auth/*` 等）；规划 `gateway/internal/{auth,announcements,support,assistant,payments}`。

**下一步**：模块接口形状冻结，auth 模块 M1 实现（todo 批次 3）。

## L5 Gateway（入口治理层，服务端）

**职责**

- 玩家 API 唯一入口：路由与协议（HTTPS JSON；M2+ WebSocket 复用 chirp）、鉴权与会话校验中间件、限流、scope 注入与校验（`game_id + env` 由 SDK 初始化指定一次）、审计与 trace 贯穿 gateway → 生态后端、健康检查。
- 生态后端的地址、凭证、拓扑对客户端不可见（architecture.md 关键决策 1）。

**边界**

- 不含业务逻辑（业务在 L4）；中间件只处理身份 / scope / 限流等横切面，不认识具体业务字段。
- 不暴露任何管理接口（归 Croupier）。

**接口形状**

- Go `http.ServeMux` + 中间件链：鉴权 → 限流 → scope → audit；`/healthz`；`/v1/{domain}/*`。

**现仓映射**：`gateway/cmd/gateway/main.go` + `gateway/go.mod`（module `github.com/cuihairu/courier/gateway`）；healthz 骨架已有。

**下一步**：中间件链骨架（先 trace + 结构化错误 + 限流），按域路由挂载（todo 批次 2）。

## 依赖规则

1. **只允许向下依赖**：L3 → L2 → L1；L5 → L4 → L1。禁止逆向（L1 不 import 任何上层）。
2. **内核不碰平台**：L2 面向接口注入平台能力；L4 只经 L5 中间件拿身份，不读原始请求细节。
3. **契约先行**：L2–L5 出现的公共类型必须能在 L1 找到对应定义；各端不得私造字段。
4. **差异各归其位**：平台差异只在 L3；服务端差异只在 L4/L5；L1 / L2 跨端完全一致。
5. **生态后端只在 L4/L5 出现**：L1–L3 对 herald / croupier / chirp / oddsmaker 一无所知。

## 与现仓结构映射

| 层 | 现仓落点 | 状态 |
| --- | --- | --- |
| L1 SDK Contract | `docs/api/`（待建） | 规划，批次 1 |
| L2 Core | `sdks/*/core`、`Runtime/Core` | 规划，批次 4（Unity 先行） |
| L3 Platform Adapter | `sdks/{unity,ue,cocos,miniprogram,laybox,godot}` | 规划，批次 5（Unity 先行） |
| L4 Service Provider | `gateway/internal/*`（待建，挂载点已注释预留） | 规划，批次 3（auth） |
| L5 Gateway | `gateway/cmd/gateway` | healthz 骨架已有，批次 2 补中间件链 |

## 演进原则

- 每层先立「接口形状 + 一个最小实现」，验证后再横向铺；不做半个功能（与 roadmap 原则一致）。
- 分批节奏与验收见 [todo](./todo.md)；业务里程碑见 [roadmap](./roadmap.md)；总体形态与关键决策见 [architecture](./architecture.md)。
