# Courier Cocos Creator SDK (TypeScript)

> 与 Unity 端同构:core(传输/会话/身份)+ service(域客户端门面);冻结契约(`docs/contract/`)唯一事实源。零运行时依赖,Node 原生 TS(erasable 语法,相对导入带 `.ts`)。

## 结构(已落地)

```text
src/
  contract/             生成物:errors.ts / envelope.ts(tools/contractgen 同步,禁手改)
  core/
    courierClient.ts    入口:new CourierClient({ config, transport?, tokenStore?, retry?, sleep? })
                        init() + guestLoginAsync/loginAsync/registerAsync 认证流(驱动状态机)
    lifecycle.ts        生命周期状态机(九态十触发,契约事件 lifecycle.*;平台无关)
    transport.ts        Transport 接口 + FetchTransport(fetch/超时/错误归一)
    apiClient.ts        scope 头、Bearer 附着、信封解析、空体状态码兜底、退避重试、
                        AUTH_TOKEN_EXPIRED 刷新重放一次
    session.ts          会话(adopt/单飞刷新/REUSED 清库/尽力而为登出/设备管理)
    identity.ts         guest / register / login / bind
    tokenStore.ts       TokenStore 接口 + MemoryTokenStore(平台持久化实现随适配器)
    courierError.ts     CourierApiError(wire/枚举/http/retryable/traceId/retryAfterMs)
    types.ts            auth 域 DTO
  platform/
    wechat/             微信小游戏适配器:TokenStore(wx storage)/ Transport(wx.request)/
                        WechatLifecycleMonitor(onShow/onHide/断网 → 状态机幂等信号)
  service/
    courierServices.ts  门面:new CourierServices(client) → 九域服务
    announcementService.ts / supportService.ts / sseParser.ts   M2 通讯与客服
    appService.ts / appConfigService.ts / brandingService.ts    M3 应用状态与远程配置
    playerService.ts / assistantService.ts                      M3 档案 / M4 小助手
    paymentService.ts / realnameService.ts                      M5 支付 / 实名
    serviceDto.ts / appDto.ts                                   域 DTO
tests/                  零依赖 node --test(契约 6 / core+lifecycle 53 / service 17 / wechat 7,共 83 例)
```

## 约定

- 生命周期:`client.init()` → 登录流 → PlayerReady;切后台/断网经 `WechatLifecycleMonitor` 注入(平台差异归 Adapter,状态机平台无关;非法信号幂等否决)。
- 分支判断只认 wire code(`e.wire`);`e.code` 为生成枚举,未登记码 = undefined 落兜底(容忍未知)。
- 能力未接(501 COMMON_CAPABILITY_DISABLED)→ 域方法返回 `null`(隐藏入口,不进报错路径);app/branding 匿名可。
- 下单只收 `skuId`(服务端定价红线);payToken 仅下单响应出现,消费归渠道 SDK。
- 实名姓名/证件号只经 `submitAsync` 传输一次:不缓存、不落盘、不进日志。
- 推送为提示(SSE 载荷只含提示字段),业务事实以归属校验后的拉取端点为准。
- token 存储按平台隔离(native localStorage / 微信小游戏 storage),适配器随部署面补。
