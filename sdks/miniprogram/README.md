# Courier miniprogram SDK

> 状态:core + service 全域落地(2026-10,对齐 cocos 批次 12+13 口径)。TypeScript,零外部依赖,erasure 语法(node ≥ 23.6 原生跑)。

## 目标平台

- 微信小程序

## 结构

```text
sdks/miniprogram/
  src/
    contract/        契约生成物(errors.ts / envelope.ts,tools/contractgen 生成)
    core/            L2 平台无关内核
      transport.ts        Transport 注入接口 + FetchTransport
      sseStream.ts        messages SSE 流接缝(SseSource)+ FetchSseSource 流读 + 退避纯函数
      utf8.ts             增量 UTF-8 解码器(跨块多字节不撕裂,非法输入 U+FFFD 降级)
      tokenStore.ts       TokenStore 注入接口 + 内存实现
      apiClient.ts        scope 头、信封解析、重试退避、TOKEN_EXPIRED 刷新重放
      session.ts          会话域(单飞刷新/安全事件清场/登出)
      identity.ts         身份域(guest/register/login/bind/refresh)
      lifecycle.ts        九态十触发状态机 + 契约事件面
      courierClient.ts    CourierClient 门面(认证流守卫)
    service/         L2 域服务(九域 + SSE 解析器 + 流编排,courierServices.ts 门面)
    platform/wechat/ L3 平台绑定
      wxApi.ts            wx 最小结构类型(零依赖)
      tokenStore.ts       wx storage 实现
      transport.ts        wx.request 实现
      lifecycleMonitor.ts wx.onAppShow/onAppHide/断网 → 状态机信号
      sseSource.ts        createWechatSseSource(wx.request enableChunked 分块流读,
                      头到回填连接,手写 UTF-8 解码;断流/静默归一重连,
                      真机净关流语义待验随部署面)
  tests/           node --test 104 例(契约 6 / core+lifecycle 60 / service 17 / stream 9 / wechat 12)
```

## 运行测试

```sh
cd sdks/miniprogram && node --test tests/*.test.ts
```

## 约定

- DTO 以 `../../docs/contract/` 为唯一事实源;未知字段容忍。
- 三端共享面(src/{contract,core,service} 与同名 tests)逐字节同构,由 `tools/tsync` 守同:改一处须三端同步(cp)。
- core 零平台引用,平台差异收敛在 Transport/TokenStore 注入与 platform/wechat 绑定(SseSource 同口径:fetch 默认 / wx.enableChunked 两实现)。
- token 走微信 storage(`courier.session` 键,损坏数据按未认证清场),不落明文配置文件。
- 生命周期信号:小程序 `wx.onAppShow/onAppHide` + `wx.onNetworkStatusChange`(小游戏端为 onShow/onHide,见 cocos)。
- UI 面板可选,游戏可自绘;501 能力未接按契约降级(null/空结果),不进报错路径。
