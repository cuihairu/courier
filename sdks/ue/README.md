# Courier Unreal Engine SDK (C++)

> M2 跟进端(Unity 稳定后对齐)。纯 ISO C++17 header-only,零外部依赖。

## 快速开始

纯逻辑层 g++ 直跑实测(2026-10-13;完整说明见 `examples/README.md`):

```sh
cd gateway && go build -o /tmp/courier-gateway ./cmd/gateway && /tmp/courier-gateway   # 起本地网关(默认 :8080)
g++ -std=c++17 -Wall -Wextra examples/ue/example.cpp \
    -Isdks/ue/service -Isdks/ue/core -Isdks/ue/contract -o /tmp/ue_example             # 另开终端
/tmp/ue_example http://127.0.0.1:18099
```

输出:

```text
login: type=GUEST id=acc_...
announcements: 0
ticket: id=tkt_... status=OPEN
detail: messages=1
example: done
```

`announcements: 0` 属正常(本地网关无发布数据)。进引擎只换一件:`Transport` 换 UE Http 模块异步适配(结果桥回游戏线程),其余代码原样;域服务直接以 `ApiClient*` 构造。

## 结构

```text
sdks/ue/
  contract/    契约生成物(errors.hpp / envelope.hpp,tools/contractgen 生成)
  core/        L2 平台无关内核
    json.hpp            手写 RFC 8259 解析器 + dump
    transport.hpp       Transport 注入接口(引擎侧实现)
    sse_stream.hpp      messages SSE 流接缝(SseSource/SseStream)+ 退避纯函数(引擎侧实现)
    token_store.hpp     TokenStore 注入接口 + 内存实现
    api_client.hpp      scope 头、信封解析、重试退避、TOKEN_EXPIRED 刷新重放
    session.hpp         会话域(刷新/登出/设备列表)
    identity.hpp        身份域(注册/登录/游客/bind/refresh)
    lifecycle.hpp       九态十触发状态机 + 契约事件面
    courier_client.hpp  CourierClient 门面(认证流守卫)
    *_test.cpp          各模块单测(CHECK 宏 + main)
  service/     L2 域服务(九域 + SSE 解析器 + 流编排)
    sse_parser.hpp       SSE 帧解析(注释/多行 data/未知字段容忍)
    sse_stream_client.hpp  messages 长连编排(501/401 终态/退避重连/should_stop 净停;
                           SseSource 接缝在 core/sse_stream.hpp,引擎接线留 L3 集成面)
    service_dto.hpp      M2 DTO(公告/工单/FAQ)
    announcement_service.hpp / support_service.hpp
    app_dto.hpp          App/Config/Branding DTO
    app_service.hpp / app_config_service.hpp / branding_service.hpp
    player_service.hpp / assistant_service.hpp / payment_service.hpp / realname_service.hpp
    service_test.cpp / service2_test.cpp / sse_stream_test.cpp
  run_tests.sh  七套测试独立编译 + 运行(contract/json/core/lifecycle/service/service2/stream)
```

## 运行测试

```sh
sh sdks/ue/run_tests.sh
```

g++ -std=c++17 -Wall -Wextra,零外部依赖,平台无关逻辑全绿。

## 约定

- 与其他端共享 `../../docs/contract/` 契约,DTO 字段逐一对应。
- 纯 ISO C++17,无异常(错误随 RequestOutcome/ServiceOutcome 返回);同步核心(引擎侧异步化由 L3 适配)。
- Transport / TokenStore 注入:引擎侧实现 HttpModule 传输与平台安全存储,本仓不含引擎依赖。
- UE 引擎集成面(HttpModule 适配、UObject 包装、.uplugin)无编译链未验证——待引擎环境接入。
