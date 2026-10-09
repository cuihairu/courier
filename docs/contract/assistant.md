# 小助手契约(Assistant)

> 状态:**Frozen v1** · 里程碑 M4 · 冻结日期 2026-10-09。
> 域前缀 `ASSISTANT` 已注册([errors.md](./errors.md));冻结后只加不改,破坏性变更升版本(见 [versioning.md](./versioning.md))。

## 能力与范围

- 能力域 `assistant`(`providers.CapAssistant`),路由 `/v1/assistant/*`;默认供应商 **sage**(自建 FAQ 检索),可换可关。
- 玩家侧:FAQ 知识库检索问答;命中有答案,答不出一键转人工(复用 [support.md](./support.md) 提单链路)。
- **LLM 接口预留但默认不依赖**:v1 默认实现为 FAQ 检索(零外部依赖、可离线、可预测);接入方可换 LLM 供应商,契约语义不变(消费侧不感知实现)。
- 本契约**不覆盖**:LLM 提示词工程、模型选型、多轮对话记忆(接入方与所接 LLM 供应商之间的事)。

## 设计取舍(为何答案来自知识库而非生成)

游戏客服场景答案必须**可审计、可复现、不幻觉**;FAQ 知识条目由运营登记([support.md](./support.md) faq 数据模型),检索命中即返回原条目,作答不生成新内容。检索评分细节不进契约(实现自由),只钉不变量:**命中条目的 question/answer 原样返回,不改写**。

## 端点总览(挂载 `/v1/assistant/*`)

| 端点 | 认证 | 说明 |
| --- | --- | --- |
| `POST /v1/assistant/query` | Bearer | 提问:命中 → 答案条目;未命中 → 可转人工信号 |

- 认证、scope header、信封同 [auth.md](./auth.md) / [primitives.md](./primitives.md)。
- 无状态:服务端不存会话;多轮由接入方每次携带完整 `query` 文本(契约不做对话历史传输)。

### `POST /v1/assistant/query`

请求体(裸对象,信封外):

| 字段 | 类型 | 约束 |
| --- | --- | --- |
| `text` | string | 1–500 字符(服务端修剪首尾后判定) |

```json
{ "text": "怎么找回账号" }
```

成功响应 `data`:

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `matched` | bool | 是否命中知识条目 |
| `answer` | object? | 命中时的条目快照(见下);未命中 = 缺省 |
| `suggestTransfer` | bool | 未命中必为 `true`(UI 据此展示「转人工」);命中为 `false` |
| `transferTicket` | object? | (预留)`{ "endpoint":"POST /v1/support/tickets" }`;v1 客户端直接调 support 域,服务端不回填 |

```json
{ "matched": true,
  "answer": { "id": "faq_a1b2c3", "question": "怎么找回账号",
              "answer": "在登录页点击『忘记密码』...", "keywords": ["找回", "账号"] },
  "suggestTransfer": false }
```

未命中:

```json
{ "matched": false, "suggestTransfer": true }
```

- **未命中不是错误**:返回 200 + `matched:false`(与 support FAQ 检索空结果同哲学,UI 呈现引导路径而非报错)。
- 空 `text`(修剪后)或缺 `text` → 400 `COMMON_INVALID_ARGUMENT`。
- 命中评分规则、同分取舍、分词方式均为实现细节,不进契约;唯一不变量 = 返回的 `answer` 内容是知识条目的原样快照。

## 错误码域表(前缀 `ASSISTANT`)

v1 **零新增错误码**:全部复用通用码(参数错误 `COMMON_INVALID_ARGUMENT`、未认证 `AUTH_*`、降级 `COMMON_CAPABILITY_DISABLED`)。域前缀 `ASSISTANT` 已注册,留给未来(如检索限流细分);新增须走 [errors.md](./errors.md) 变更流程。

## 客户端要求(各端 SDK)

- 能力未接(501 `COMMON_CAPABILITY_DISABLED`)→ 查询返回 null,UI 隐藏小助手入口(与各域降级一致)。
- `matched:false` 且 `suggestTransfer:true` → UI 展示「转人工」:调用 support 域提单([support.md](./support.md)),提单成功后复用 `support.ticket_replied` 实时链路。
- `text` 超长/为空是使用方错误(SDK 直接 400 抛出);UI 侧应先本地限制输入长度。
- 无本地缓存:答案随知识库更新变化,不缓存(与 config 不同——config 是版本化快照,assistant 是即席查询)。
