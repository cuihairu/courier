# L3 平台适配单测(本仓首个真传输面):FileTokenStore user:// 落盘回路 +
# CourierHTTPTransport(Godot HTTPClient)对本仓 TCPServer mock 的真 HTTP/1.1
# 往返(头/体逐字节过 socket),含 client→api→transport→store 全链 guest 落库。
# 运行(从仓库根):
#   godot --headless --path sdks/godot --script tests/test_platform.gd
extends SceneTree

const MockServer = preload("res://tests/mock_server.gd")

var failed: int = 0
var _client: CourierClient  # GDScript Callable 弱引用目标:用例帧须持活 client
var _send_done: bool = false
var _last_result: Dictionary = {}


func check(cond: bool, msg: String) -> void:
	if not cond:
		print("FAIL: " + msg)
		failed += 1


func session_json() -> String:
	return '{"account":{"id":"acc_1","type":"GUEST","status":"ACTIVE","createdAt":"t"},"accessToken":"access-1","accessExpiresAt":"t","refreshToken":"refresh-1","refreshExpiresAt":"t","deviceId":"device-1"}'


func success(raw: String) -> String:
	return '{"data":' + raw + "}"


## 让出一帧(--script 环境 Engine.get_main_loop() 为 null,须由用例注入)。
func _yield_frame() -> void:
	await process_frame


## 协程收尾结果到成员(send/request 均 fire-and-forget 起跑,结果经此可断言)。
func _send_async(transport: CourierTransport, req: Dictionary) -> void:
	_last_result = await transport.send(req)
	_send_done = true


func _run_api(client: CourierClient) -> void:
	_last_result = await client.api.request("GET", "/v1/announcements?limit=20", null, true)
	_send_done = true


## 泵到条件满足或帧数耗尽(耗尽即 FAIL,不悬挂)。
func pump_until(mock: MockServer, cond: Callable) -> void:
	var guard := 0
	while guard < 900:
		guard += 1
		if bool(cond.call()):
			return
		await mock.pump(self)
	check(bool(cond.call()), "条件应在 900 帧内满足")


# ---- FileTokenStore:user:// 落盘回路 ----

func t_token_store_roundtrip() -> void:
	var store := CourierFileTokenStore.new()
	store.path = "user://courier_test/roundtrip.json"
	store.clear()
	check(store.load().is_empty(), "未写入应载出空")
	store.save({"accessToken": "access-1", "refreshToken": "refresh-1"})
	var again := CourierFileTokenStore.new()
	again.path = store.path
	var loaded: Dictionary = again.load()
	check(str(loaded.get("accessToken", "")) == "access-1", "save 后应可跨实例载回")
	check(str(loaded.get("refreshToken", "")) == "refresh-1", "会话字段应完整")
	again.clear()
	check(store.load().is_empty(), "clear 后应载出空")


# ---- HTTP 传输:GET 真往返(自定义头透传 + 响应解析) ----

func t_http_get() -> void:
	var mock := MockServer.new()
	check(mock.port > 0, "mock 端口应可监听")
	var payload := '{"data":{"ok":true}}'
	mock.responses.append("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n%s" % [payload.length(), payload])
	var transport := CourierHTTPTransport.new()
	transport.frame_f = Callable(self, "_yield_frame")
	_send_done = false
	_send_async.call_deferred(transport, {
		"method": "GET",
		"url": "http://127.0.0.1:%d/v1/ping" % mock.port,
		"headers": {"X-Courier-Game-Id": "game_demo", "X-Courier-Env": "prod"},
	})
	await pump_until(mock, func(): return _send_done)
	check(bool(_last_result.get("ok", false)), "GET 应成功")
	check(int(_last_result.get("status_code", 0)) == 200, "状态码应 200(实得 %s)" % str(_last_result.get("status_code")))
	check(str(_last_result.get("body", "")) == payload, "响应体应逐字节一致")
	var hdrs: Dictionary = _last_result.get("headers", {})
	check(str(hdrs.get("content-type", "")) == "application/json", "响应头键应小写归一")
	var req0: Dictionary = mock.seen[0]
	check(str(req0["method"]) == "GET" and str(req0["path"]) == "/v1/ping", "请求行应真到 socket")
	check(str(req0["headers"].get("x-courier-game-id", "")) == "game_demo", "自定义头应透传")
	mock.stop()


# ---- HTTP 传输:POST 体 + 501 状态与头 ----

func t_http_post() -> void:
	var mock := MockServer.new()
	mock.responses.append('HTTP/1.1 501 Not Implemented\r\nContent-Length: 79\r\n\r\n{"error":{"code":"COMMON_CAPABILITY_DISABLED","message":"off","retryable":false}}')
	var transport := CourierHTTPTransport.new()
	transport.frame_f = Callable(self, "_yield_frame")
	_send_done = false
	_send_async.call_deferred(transport, {
		"method": "POST",
		"url": "http://127.0.0.1:%d/v1/identity/guest" % mock.port,
		"headers": {"Content-Type": "application/json"},
		"jsonBody": '{"deviceId":"device-1"}',
	})
	await pump_until(mock, func(): return _send_done)
	check(int(_last_result.get("status_code", 0)) == 501, "501 应原样上抛给信封解析")
	var req0: Dictionary = mock.seen[0]
	check(str(req0["method"]) == "POST", "POST 应真到 socket")
	check(str(req0["body"]) == '{"deviceId":"device-1"}', "请求体应真到 socket(实得 %s)" % str(req0["body"]))
	check(str(req0["headers"].get("content-type", "")) == "application/json", "Content-Type 应透传")
	check(str(req0["headers"].get("content-length", "")) == str(String('{"deviceId":"device-1"}').length()), "Content-Length 应匹配")
	mock.stop()


# ---- 全链:e2e guest 经真传输落库,第二跳带 Bearer ----

func t_http_e2e_guest() -> void:
	var mock := MockServer.new()
	mock.responses.append(success(session_json()))
	mock.responses.append('{"data":{"items":[],"nextCursor":""}}')
	var transport := CourierHTTPTransport.new()
	transport.frame_f = Callable(self, "_yield_frame")
	var store := CourierFileTokenStore.new()
	store.path = "user://courier_test/e2e.json"
	store.clear()
	var options := {
		"config": {"endpoint": "http://127.0.0.1:%d" % mock.port, "gameId": "game_demo", "env": "prod"},
		"transport": transport,
		"tokenStore": store,
	}
	_client = CourierClient.new(options)
	check(_client.valid, "client 应装配成功")
	# 协程不能无 await 起跑:call_deferred 点火,泵至会话落库
	_client.identity.call_deferred("guest", "device-1", "android")
	await pump_until(mock, func(): return not _client.session.current().is_empty())
	check(mock.seen.size() >= 1, "guest 请求应真到 mock")
	if mock.seen.size() >= 1:
		var req0: Dictionary = mock.seen[0]
		check(str(req0["method"]) == "POST" and str(req0["path"]) == "/v1/identity/guest", "guest 请求行不符")
		check(str(req0["headers"].get("x-courier-game-id", "")) == "game_demo"
				and str(req0["headers"].get("x-courier-env", "")) == "prod", "scope 头应经真传输")
		check(str(req0["body"]) == '{"deviceId":"device-1","platform":"android"}', "guest body 应真到 socket")
	check(str(store.load().get("accessToken", "")) == "access-1", "会话应持久化于 user://")
	# 第二跳:认证请求自动带 Bearer(access 经 token_provider 取自已落库会话)
	_send_done = false
	_run_api.call_deferred(_client)
	await pump_until(mock, func(): return _send_done)
	check(bool(_last_result.get("ok", false)), "公告列表应成功")
	check(mock.seen.size() >= 2, "第二跳应真到 mock")
	if mock.seen.size() >= 2:
		var req1: Dictionary = mock.seen[1]
		check(str(req1["path"]) == "/v1/announcements?limit=20", "公告路径不符")
		check(str(req1["headers"].get("authorization", "")) == "Bearer access-1", "第二跳应带 Bearer(真传输)")
	store.clear()
	mock.stop()


func _init() -> void:
	await t_token_store_roundtrip()
	await t_http_get()
	await t_http_post()
	await t_http_e2e_guest()
	DirAccess.remove_absolute("user://courier_test")
	print("godot platform tests: %s (%d failed)" % ["all green" if failed == 0 else "FAILED", failed])
	quit(1 if failed > 0 else 0)
