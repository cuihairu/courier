
# 按需选装清单(字节实测)

> 用途:回答「哪些能不进、各进哪个省多少、得到什么」。数据口径见末节。

## UPM 四包(Unity)

包按依赖方向拆四层:core 是被依赖方,service 依赖 core,ui/diagnostics 依赖 service。**只装自己用到的**。

| 包 | 实测源码字节 | 不装省什么 | 装了得到什么 |
| --- | --- | --- | --- |
| `com.courier.core` | 79,759 B(~78 KiB) | —— 地基,不可省 | scope 头/Bearer 附着/信封解析、生命周期状态机、ApiClient 退避重试与 `AUTH_TOKEN_EXPIRED` 刷新重放、Identity/Session 编排(refresh 单飞)、传输与令牌存储抽象 |
| `com.courier.service` | 62,786 B(~61 KiB) | 自写九域 REST 调用 | `CourierServices` 门面:公告/客服(含附件直传)/应用配置/品牌/玩家/小助手/支付/实名 + consent 年龄门(v1.1)+ SSE 解析 |
| `com.courier.ui` | 22,673 B(~22 KiB) | 自己写面板渲染 | 公告面板 + 跑马灯(`MarqueeRendered` 事件)、客服面板;纯逻辑 C#,不绑 UI 框架,渲染走回调/事件 |
| `com.courier.diagnostics` | 20,495 B(~20 KiB) | 无埋点上报 | trace 关联/性能事件/崩溃上报三流(diagnostics.md) |

最小可用集 `core + service` = 142,545 B(~139 KiB);四包全装 = 185,713 B(~181 KiB)。
**常见裁剪**:游戏没有运营公告就砍 ui;自建埋点管道就砍 diagnostics;纯自建后端、只要登录/会话就只装 core。

## TS 三端(cocos / miniprogram / laybox)

同构单树,不可按目录拆包(单 `src/`),但可按「用不用 ui/diagnostics/platform」做死代码消除:

| 子树 | 实测字节 | 说明 |
| --- | --- | --- |
| `core/` | 44,570 B | 地基(含 `FetchTransport`,miniprogram 侧用 `createWechatTransport(wx)`) |
| `service/` | 41,575 B | 九域门面 + consent 年龄门(v1.1) |
| `ui/` | 13,323 B | 公告面板 + 跑马灯(`onMarqueeRendered`) |
| `diagnostics/` | 16,088 B | 三流上报 |
| `platform/` | 12,179 B | 只留自己用的那一端适配,其余可删 |
| `contract/` | 7,659 B | 生成物,别删 |

最小集 `core + service` ≈ 86,145 B(~84 KiB);整树 135,394 B ≈ 132 KiB。

## UE(header-only)与 Godot(单树)

| 端 | 实测字节 | 说明 |
| --- | --- | --- |
| UE `contract + core + service` | 288,453 B(~281 KiB) | 头文件树,未用的服务头不 include 即不进目标文件(无 UI 包、无 diagnostics 包) |
| Godot `src/` | 63,914 B(~62 KiB) | 含平台件 `platform/`(HTTPClient/FileTokenStore);UI 待引擎集成面 |

## Provider 维度(网关侧,不是包体积)

不按能力域接 Provider = 该域 501 `COMMON_CAPABILITY_DISABLED`(非故障),SDK 照契约安全隐藏对应 UI。默认接线路由表(`cmd/gateway/main.go`):

| 能力域 | 默认 Provider | 不接的后果 |
| --- | --- | --- |
| `identity` | `courier-account`(可换 `courier-account-alt`) | 无登录/会话 |
| `announcements` | `herald` | 公告隐藏 |
| `support` | `croupier` | 客服隐藏(含附件直传) |
| `messages` | `chirp` | 无推送通道(SSE 流不可用) |
| `app` | `scribe` | 无配置/品牌/版本(维护门随之失能) |
| `player` | `archivist` | 无玩家档案 |
| `assistant` | `sage` | 无小助手 |
| `payments` | `teller` | 无支付 |
| `realname` | 无默认(需自接) | 实名/防沉迷面隐藏;consent 组(v1.1)另需 `COURIER_REALNAME_CONSENT*` 开启,否则年龄门 UI 隐藏(501 → null) |

替换/降级语义见 routing 包（`gateway/routing`）与 [layers.md](./layers.md):primary + `fallbacks[]`,健康检查为逐请求探活(无缓存),熔断仅实名域已落地(批次 7);禁用或全链不健康分别回 501 / 503 `COMMON_UNAVAILABLE`。自建/替换后端的完整接入步骤见 [provider-guide.md](./provider-guide.md)。

## 口径注

- 字节 = 2026-10-19 `du -sb` 实测**源码**字节(批次 102 复测:unity service / TS service / UE service / Godot src 含 consent 组 v1.1 增量;批次 97:unity core 含 lifecycle 信号中继与 refresh 单飞、TS core 含查表键型修复、UE 含测试收口增量;历史批次 63/60/55 见 git);UPM 统一取 `packages/*/Runtime/`(包根另含 README/package.json,非源码)——批次 55 原口径混用(后两包取包根),批次 60 统一:UPM 包在引擎内编译、无独立构建产物,TS 三端无 bundler 不产构建产物——此处是「接入面积」,不等于下载体积或引擎内占用。
- TS 三仓代码同步生成(`tools/tsync`),`cocos/src/` 的字节代表三端口径:`service/core/contract/ui/diagnostics` 子树三端**字节全同**(diff 实证),差异只在 `platform/` 适配层——miniprogram +24 B、laybox +1,473 B(相对 cocos)。
- 引擎侧真实产物(Unity IL2CPP 剥离、UE GC 段、Godot 字节码)随项目依赖图变化,本清单不估算。
