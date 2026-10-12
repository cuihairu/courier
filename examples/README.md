# Courier SDK 端到端示例(六端)

六端各一份最小示例,同一条链路:**接入 → 游客登录 → 拉公告 → 提工单 → 拉工单详情**,
全部对着本仓 gateway 实测可跑(2026-10-13 验证,预期输出见各节)。SDK 纯逻辑层
零引擎依赖,引擎端(Unity/UE/Godot)也能在进程内直跑;进引擎只需把传输/存储
换成平台实现,业务代码不变。

## 第 0 步:起本地网关(零凭据)

```sh
cd gateway
go build -o /tmp/courier-gateway ./cmd/gateway
/tmp/courier-gateway                 # 默认 :8080;COURIER_GATEWAY_ADDR=:18099 可覆盖
```

每个请求需 scope 头 `X-Courier-Game-Id` / `X-Courier-Env`,示例统一用
`gameId=game_demo`、`env=prod`(config 即 scope)。

## Cocos Creator(TS)

```sh
COURIER_ENDPOINT=http://127.0.0.1:18099 node examples/cocos/example.ts
```

传输默认 WHATWG fetch(Node 直跑即用);Cocos 工程内把 import 路径换成包导入。

## 微信小程序(TS)

```sh
COURIER_ENDPOINT=http://127.0.0.1:18099 node examples/miniprogram/example.ts
```

同 cocos;工程内把 Transport 换 `src/platform/wechat/transport.ts` 的
`createWechatTransport(wx)`(wx.request 适配)。

## LayaBox(LayaAir 3,TSE)

```sh
COURIER_ENDPOINT=http://127.0.0.1:18099 node examples/laybox/example.ts
```

工程内把 Transport 换 `src/platform/laya/transport.ts` 的
`createLayaboxTransport(factory)`。

## Unity(.NET 直跑纯逻辑层)

```sh
dotnet run --project examples/unity -- http://127.0.0.1:18099
```

示例内实现了 `ITransport`(HttpClient)与 `ITokenStore`(内存)两个适配件。
进引擎:传输换 `UnityWebRequest`,存储换平台安全存储(红线:禁明文
PlayerPrefs),其余代码原样。

> 引擎网络栈注意:早期版本网关对 `/v1/<能力域>` 无斜杠形态回 307,而
> .NET HttpClient 重定向后会丢弃 `Authorization`;网关已改为内部重派不发
> 307,自定义反向代理若开启了规范化重定向需同样注意。

## Unreal(ISO C++17 直跑纯逻辑层)

```sh
g++ -std=c++17 -Wall -Wextra examples/ue/example.cpp \
    -Isdks/ue/service -Isdks/ue/core -Isdks/ue/contract -o /tmp/ue_example
/tmp/ue_example http://127.0.0.1:18099
```

示例内用 POSIX socket 实现了 `courier::Transport`(HTTP/1.1 + chunked 解码)。
进引擎:换 UE Http 模块异步适配(结果桥回游戏线程),其余代码原样。
UE 端暂无 `CourierServices` 门面,域服务直接以 `ApiClient*` 构造。

## Godot 4.3(GDScript,headless 直跑)

```sh
godot --headless --path sdks/godot \
    --script <仓库绝对路径>/examples/godot/example.gd
# 端点可用环境变量 COURIER_ENDPOINT 覆盖(--script 相对路径按 res:// 解析,故用绝对路径)
```

传输用平台件 `CourierHTTPTransport`(Godot HTTPClient,与游戏内同一份代码);
`--script` 模式下注入 `frame_f` 让出帧。存储缺省内存,正式接入换
`FileTokenStore`。

## 预期输出(六端一致)

```text
login: type=GUEST id=acc_...
announcements: 0
ticket: id=tkt_... status=OPEN
detail: messages=1
example: done
```

`announcements: 0` 属正常——本地网关没有公告发布数据;游客账号由 deviceId
确定性生成,同 deviceId 多端跑出同一 `acc_`。
