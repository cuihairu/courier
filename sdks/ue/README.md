# Courier Unreal Engine SDK (C++)

> M2 跟进端(Unity 稳定后对齐)。纯 ISO C++17 header-only,零外部依赖。

## 结构

```text
sdks/ue/
  contract/    契约生成物(errors.hpp / envelope.hpp,tools/contractgen 生成)
  core/        L2 平台无关内核
    json.hpp            手写 RFC 8259 解析器 + dump
    transport.hpp       Transport 注入接口(引擎侧实现)
    token_store.hpp     TokenStore 注入接口 + 内存实现
    api_client.hpp      scope 头、信封解析、重试退避、TOKEN_EXPIRED 刷新重放
    session.hpp         会话域(刷新/登出/设备列表)
    identity.hpp        身份域(注册/登录/游客/bind/refresh)
    lifecycle.hpp       九态十触发状态机 + 契约事件面
    courier_client.hpp  CourierClient 门面(认证流守卫)
    *_test.cpp          各模块单测(CHECK 宏 + main)
  service/     L2 域服务(九域 + SSE 解析器)
    sse_parser.hpp       SSE 帧解析(注释/多行 data/未知字段容忍)
    service_dto.hpp      M2 DTO(公告/工单/FAQ)
    announcement_service.hpp / support_service.hpp
    app_dto.hpp          App/Config/Branding DTO
    app_service.hpp / app_config_service.hpp / branding_service.hpp
    player_service.hpp / assistant_service.hpp / payment_service.hpp / realname_service.hpp
    service_test.cpp / service2_test.cpp
  run_tests.sh  六套测试独立编译 + 运行(contract/json/core/lifecycle/service/service2)
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
