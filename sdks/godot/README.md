# Courier godot SDK

> 状态：M1 core + 服务域（M2–M5 契约面）+ 平台适配面已落位（生命周期状态机 + Transport/TokenStore 抽象 + ApiClient + Identity/Session + CourierClient 门面 + SSE 解析器 + 九域服务 + CourierServices 门面 + HTTPClient 真传输 + user:// 持久令牌存储 + messages SSE 流式传输，Godot 4.3 headless 八套测试全绿）；TLS 路径与真机验证随部署面，UI 待引擎集成面。

## 目标平台

- Godot Engine 4.3 (GDScript)

## 结构

```text
sdks/godot/
  project.godot     工程清单(class_name 全局类解析需 --import 生成类缓存)
  contract/         契约生成物(errors.gd / envelope.gd,tools/contractgen 生成,# 注释风格)
  src/core/         L2 core(平台无关):lifecycle / transport / tokenStore /
                    courierError / apiClient / identity / session / courierClient
  src/service/      L2 服务域(与 cocos/ue 同构):sseParser + 九域客户端 + CourierServices
  src/platform/     L3 平台绑定:httpTransport(HTTPClient 真传输) /
                    fileTokenStore(user:// 持久令牌存储) /
                    sseStreamTransport(messages SSE 流读,chunked 分框) /
                    lifecycleMonitor(前后台信号 → 状态机事件)
  tests/            SceneTree 脚本测试(headless 跑,不依赖场景)
```

## 运行测试

首次先导入生成类缓存(`.godot/`,已 gitignore),之后八条测试:

```sh
godot --headless --path sdks/godot --import
godot --headless --path sdks/godot --script tests/test_contract.gd
godot --headless --path sdks/godot --script tests/test_lifecycle.gd
godot --headless --path sdks/godot --script tests/test_core.gd
godot --headless --path sdks/godot --script tests/test_service.gd
godot --headless --path sdks/godot --script tests/test_service2.gd
godot --headless --path sdks/godot --script tests/test_platform.gd
godot --headless --path sdks/godot --script tests/test_stream.gd
godot --headless --path sdks/godot --script tests/test_adapter.gd
```

本机无 Godot 二进制时,契约一致性由 tools/contractgen 兜底(fixture↔生成物漂移检查)。

## 模块（能力树，见 docs/architecture.md）

- Identity / Session: 登录、登出、绑定、设备；token 轮换
- Communication: 公告 + messages SSE 流读(sseStreamTransport,chunked 分框+退避重连)
- Support: 工单 / FAQ（面板 UI 可选）
- App: 版本 / 维护 / 远程配置 / 品牌
- Player: 档案与游戏角色
- Assistant: FAQ 检索问答
- Payment: 契约面（下单/轮询；渠道回调是 S2S，不经客户端）
- RealName: 实名（可选，合规，默认关）
- Diagnostics: 诊断（可选包，默认全关）

## 路线图

- M1: Identity / Session + 生命周期状态机 —— **已完成**
- M2–M5 服务域契约面（公告/客服/SSE、实名、App/Config/Branding/Player、Assistant、Payment）—— **已完成**(headless 全绿)
- M2+: 平台适配面（ HTTPClient 传输 / user:// 存储 / messages SSE 流式传输已落地；TLS 真机验证待接(前后台信号已落地) ）+ UI 可选包

## 约定

- DTO 以 `../../docs/contract/` 为唯一事实源；未知字段容忍。
- GDScript 无异常:错误一律 `{ok:false, error}` Dictionary;`await` 为协程必需。
- 服务域结果三态(与 TS `null` / UE `nullopt` 同构):成功 `{ok:true, data}`;能力未接(501)`{ok:true, data:null}`;其余错误 `{ok:false, error}`。
- GDScript `Callable` 弱引用目标:持有回调的对象(如 CourierClient)须由接入方持活。
- HTTPClient poll 语义:响应完整性以 Content-Length 计数(发完即断时 `DISCONNECTED` 直接可达,`CONNECTED` 未必可观察);响应头收完即被清空,须循环内捕获。
- SSE 流响应必须 `Transfer-Encoding: chunked`:HTTPClient 不支持无 Content-Length 的 close 分隔体(收完头即判响应完成);自建网关自动 chunked,天然满足。
- token 存储走 Godot 平台存储；UI 可选，游戏可自绘。
- 生成物注释一律 `#`（GDScript 无 `//`,Go/C 风格头部会被引擎判为语法错误）。
