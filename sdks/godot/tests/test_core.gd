# L2 Core 单测(cocos core.test.ts 同构):wire 形状、scope/Bearer 头、信封解析、
# 退避重试、TOKEN_EXPIRED 自动 refresh 重放、安全事件清场、本地未认证预检、
# 门面装配(auth_flow 状态流 + 守卫)。
# 运行(从仓库根):
#   godot --headless --path sdks/godot --script tests/test_core.gd
extends SceneTree

var failed: int = 0


class FakeTransport extends CourierTransport:
	# 脚本化假传输(unity FakeTransport 同构):记录请求,按队列回放响应。
	# 每次 send 让出一帧(模拟真实网络往返,与 TS Promise 语义对齐);
	# --script 环境无 Engine.get_main_loop(),由测试传入自身(SceneTree)。
	var tree: SceneTree
	var requests: Array = []
	var queue: Array = []
	# fn 处理器(TOKEN_EXPIRED 重放链:refresh/重放按序入队)。
	var fn_requests: Array = []
	var fn_call_count: int = 0
	# 下一请求让出的帧数(默认 1;单飞测试拉长在途窗口)。
	var delay_frames: int = 1


	func _init(t: SceneTree) -> void:
		tree = t


	func enqueue(status_code: int, body: String, headers: Dictionary = {}) -> void:
		queue.append({"kind": "resp", "status_code": status_code, "body": body, "headers": headers})


	func enqueue_fail(message: String) -> void:
		queue.append({"kind": "fail", "message": message})


	func enqueue_fn(status_code: int, body: String) -> void:
		queue.append({"kind": "fn", "status_code": status_code, "body": body})


	func send(request: Dictionary) -> Dictionary:
		for i in range(delay_frames):
			await tree.process_frame  # 网络往返让出(协程交错前提)
		delay_frames = 1
		requests.append(request)
		if queue.is_empty():
			return {"ok": false, "message": "脚本耗尽:" + str(request.get("method", "")) + " " + str(request.get("url", ""))}
		var item: Dictionary = queue.pop_front()
		if item["kind"] == "fn":
			fn_call_count += 1
			fn_requests.append(request)
			return {"ok": true, "status_code": item["status_code"], "headers": {}, "body": item["body"]}
		if item["kind"] == "fail":
			return {"ok": false, "message": item["message"]}
		return {"ok": true, "status_code": item["status_code"], "headers": item.get("headers", {}), "body": item["body"]}


class SleepRecorder:
	# 记录型睡眠(不真等;断言退避间隔)。
	var sleeps: Array = []

	func fn(ms: int) -> void:
		sleeps.append(ms)


class Recorder:
	# 生命周期事件记录器。
	var events: Array = []
	var off: Callable

	func attach(machine: CourierLifecycle) -> void:
		off = machine.on_event(_on_event)

	func _on_event(event: Dictionary) -> void:
		events.append(event)

	func types() -> Array:
		return events.map(func(e): return e["type"])


func check(cond: bool, msg: String) -> void:
	if not cond:
		print("FAIL: " + msg)
		failed += 1


func session_json(access: String = "access-1", refresh: String = "refresh-1") -> String:
	return '{"account":{"id":"acc_1","type":"GUEST","status":"ACTIVE","createdAt":"2026-10-09T12:00:00.000Z"},"accessToken":"%s","accessExpiresAt":"2026-10-09T12:15:00.000Z","refreshToken":"%s","refreshExpiresAt":"2026-10-10T12:00:00.000Z","deviceId":"device-1"}' % [access, refresh]


func success(raw: String) -> String:
	return '{"data":' + raw + '}'  # 裸 DTO 字符串直接包信封


func err_wire(r: Dictionary) -> String:
	return str(r.get("error", {}).get("wire", ""))


func make_client(transport: FakeTransport, sleeps: SleepRecorder = null,
		store: CourierTokenStore = null, cfg: Dictionary = {}) -> CourierClient:
	var options := {
		"config": cfg if not cfg.is_empty() else {"endpoint": "https://api.example.com", "gameId": "game_demo", "env": "prod"},
		"transport": transport,
	}
	if sleeps != null:
		options["sleep"] = Callable(sleeps, "fn")
	if store != null:
		options["tokenStore"] = store
	return CourierClient.new(options)


# ---- guest:wire 形状(scope 头/JSON 体/无 Bearer)+ 会话落库 ----
func t_guest_wire() -> void:
	var transport := FakeTransport.new(self)
	var client := make_client(transport)
	transport.enqueue(200, success(session_json()))
	var r: Dictionary = await client.identity.guest("device-1", "android")
	var req: Dictionary = transport.requests[0]
	check(req["method"] == "POST", "guest 应为 POST")
	check(req["url"] == "https://api.example.com/v1/identity/guest", "guest URL 不符:" + str(req["url"]))
	check(req["headers"]["X-Courier-Game-Id"] == "game_demo" and req["headers"]["X-Courier-Env"] == "prod",
			"scope 头缺失或不符")
	check(not req["headers"].has("Authorization"), "guest 应匿名(无 Authorization)")
	var sent: Variant = JSON.parse_string(str(req.get("jsonBody", "")))
	check(sent == {"deviceId": "device-1", "platform": "android"}, "guest body 不符:" + str(sent))
	check(r.get("ok", false) and r["data"]["accessToken"] == "access-1", "guest 返回会话应含 access-1")
	check(str(client.session.current().get("refreshToken", "")) == "refresh-1", "会话应已落库")


# ---- Bearer:认证请求自动附带,匿名请求不带 ----
func t_bearer() -> void:
	var transport := FakeTransport.new(self)
	var client := make_client(transport)
	transport.enqueue(200, success(session_json()))
	await client.identity.guest("device-1")
	transport.enqueue(200, success('{"sessionId":"ses_1","deviceId":"device-1"}'))
	transport.enqueue(200, success(session_json()))
	var info: Dictionary = await client.session.info()
	var refreshed: Dictionary = await client.identity.guest("device-2")
	check(str(transport.requests[1]["headers"].get("Authorization", "")) == "Bearer access-1",
			"认证请求应带 Bearer access-1")
	check(info["data"]["sessionId"] == "ses_1", "info 应返回会话信息")
	check(not transport.requests[2]["headers"].has("Authorization"), "guest 匿名不带 Bearer")
	check(refreshed["data"]["accessToken"] == "access-1", "响应为准(同设备回同账号)")
	check(JSON.parse_string(str(transport.requests[2].get("jsonBody", ""))) == {"deviceId": "device-2"},
			"二次 guest body 应为新设备")


# ---- 未认证预检:withAuth 无会话本地返回,不发包 ----
func t_unauthenticated_precheck() -> void:
	var transport := FakeTransport.new(self)
	var client := make_client(transport)
	var r: Dictionary = await client.session.info()
	var e: Dictionary = r.get("error", {})
	check(err_wire(r) == "COMMON_UNAUTHENTICATED", "未认证预检应为 COMMON_UNAUTHENTICATED")
	check(str(e.get("code", "")) == "CommonUnauthenticated", "枚举名应为 CommonUnauthenticated(实得 %s)" % e.get("code"))
	check(transport.requests.is_empty(), "未认证不应发包")


# ---- 失败信封:typed 错误(wire/枚举/http/retryable/traceId) ----
func t_typed_error_envelope() -> void:
	var transport := FakeTransport.new(self)
	var client := make_client(transport)
	transport.enqueue(404, '{"error":{"code":"ANNOUNCEMENT_NOT_FOUND","message":"gone","retryable":false},"traceId":"4bf92f3577b34da6a3ce929d0e0e4736"}')
	var r: Dictionary = await client.api.request("GET", "/v1/announcements/x", null, false)
	var e: Dictionary = r.get("error", {})
	check(err_wire(r) == "ANNOUNCEMENT_NOT_FOUND", "wire 应为 ANNOUNCEMENT_NOT_FOUND")
	check(str(e.get("code", "")) == "AnnouncementNotFound", "枚举名应为 AnnouncementNotFound")
	check(int(e.get("http", 0)) == 404 and not bool(e.get("retryable", true)), "http/retryable 应 404/false")
	check(str(e.get("traceId", "")) == "4bf92f3577b34da6a3ce929d0e0e4736", "traceId 应透传")
	check(str(e.get("message", "")).contains("gone"), "message 应透传")


# ---- 空 body/非 JSON:按状态兜底 typed(5xx → COMMON_UNAVAILABLE) ----
func t_fallback_wire() -> void:
	var transport := FakeTransport.new(self)
	var client := make_client(transport)
	transport.enqueue(503, "")
	transport.enqueue(502, "<html>bad gateway</html>")
	var r1: Dictionary = await client.api.request("GET", "/x", null, false)
	check(err_wire(r1) == "COMMON_UNAVAILABLE", "空 body 503 应兜底 COMMON_UNAVAILABLE")
	check(bool(r1.get("error", {}).get("retryable", false)), "COMMON_UNAVAILABLE 应可重试")
	var r2: Dictionary = await client.api.request("GET", "/x", null, false)
	check(err_wire(r2) == "COMMON_UNAVAILABLE", "非 JSON 502 应兜底 COMMON_UNAVAILABLE")


# ---- 重试:retryable 按 Retry-After 退避后成功;非 retryable 一次即返 ----
func t_retry() -> void:
	var transport := FakeTransport.new(self)
	var sleeps := SleepRecorder.new()
	var client := make_client(transport, sleeps)
	transport.enqueue(429, '{"error":{"code":"RATE_LIMITED","message":"slow down","retryable":true}}', {"retry-after": "2"})
	transport.enqueue(200, success('{"ok":true}'))
	transport.enqueue(404, '{"error":{"code":"COMMON_NOT_FOUND","message":"nope","retryable":false}}')
	var ok_r: Dictionary = await client.api.request("GET", "/x", null, false)
	check(ok_r.get("ok", false) and ok_r["data"] == {"ok": true}, "重试后应成功")
	check(transport.requests.size() == 2, "应共 2 次请求(实际 %d)" % transport.requests.size())
	check(sleeps.sleeps == [2000], "Retry-After(秒)→ 毫秒(实际 %s)" % str(sleeps.sleeps))
	var r: Dictionary = await client.api.request("GET", "/x", null, false)
	check(err_wire(r) == "COMMON_NOT_FOUND", "非 retryable 应一次即返")
	check(transport.requests.size() == 3, "非 retryable 不重试")


# ---- 重试耗尽:maxAttempts 次后返回最后一次错误 ----
func t_retry_exhausted() -> void:
	var transport := FakeTransport.new(self)
	var sleeps := SleepRecorder.new()
	var client := make_client(transport, sleeps)
	for i in range(3):
		transport.enqueue(503, '{"error":{"code":"COMMON_UNAVAILABLE","message":"down","retryable":true}}')
	var r: Dictionary = await client.api.request("GET", "/x", null, false)
	check(err_wire(r) == "COMMON_UNAVAILABLE", "耗尽后应返回最后错误")
	check(transport.requests.size() == 3, "应恰好 3 次尝试")
	check(sleeps.sleeps.size() == 2, "3 次尝试间 2 次退避(实际 %d)" % sleeps.sleeps.size())


# ---- 网络失败:映射 COMMON_UNAVAILABLE 可重试,恢复后成功 ----
func t_network_failure() -> void:
	var transport := FakeTransport.new(self)
	var client := make_client(transport)
	transport.enqueue_fail("ECONNRESET")
	transport.enqueue(200, success('{"ok":1}'))
	var r: Dictionary = await client.api.request("GET", "/x", null, false)
	# JSON 数字一律浮点,而 Dictionary == 逐元素按类型严格比较:逐字段断言。
	check(r.get("ok", false) and int(r["data"].get("ok", 0)) == 1, "网络失败恢复后应成功")
	check(transport.requests.size() == 2, "网络失败应重试一次后成功")


# ---- TOKEN_EXPIRED:自动 refresh 一次后重放(新 Bearer),hook 只走一次 ----
func t_token_expired_replay() -> void:
	var transport := FakeTransport.new(self)
	var store := CourierTokenStore.MemoryTokenStore.new()
	var sleeps := SleepRecorder.new()
	var client := make_client(transport, sleeps, store)
	var rec := Recorder.new()
	rec.attach(client.lifecycle)
	transport.enqueue(200, success(session_json()))
	await client.identity.guest("device-1")
	transport.enqueue(401, '{"error":{"code":"AUTH_TOKEN_EXPIRED","message":"expired","retryable":false}}')
	transport.enqueue_fn(200, success(session_json("access-2", "refresh-2")))
	transport.enqueue_fn(200, success('{"ok":true}'))
	var r: Dictionary = await client.api.request("GET", "/v1/announcements")
	check(r.get("ok", false) and r["data"] == {"ok": true}, "refresh 重放后应成功")
	check(transport.fn_call_count == 2, "refresh+重放共 2 次 fn(实际 %d)" % transport.fn_call_count)
	var refresh_req: Dictionary = transport.fn_requests[0]
	check(str(refresh_req["url"]) == "https://api.example.com/v1/identity/refresh", "refresh URL 不符")
	check(JSON.parse_string(str(refresh_req.get("jsonBody", ""))) == {"refreshToken": "refresh-1"},
			"refresh body 应为 refreshToken=refresh-1")
	check(str(transport.fn_requests[1]["headers"].get("Authorization", "")) == "Bearer access-2",
			"重放应带新 token")
	check(str(store.load().get("accessToken", "")) == "access-2", "轮换应已落库")
	check(sleeps.sleeps.is_empty(), "refresh 重放不占退避次数")
	check(rec.types().has("lifecycle.token_expired"), "自动 refresh 应发 token_expired 契约事件")


# ---- refresh 失败:重放放弃,原 401 返回 ----
func t_refresh_failure_keeps_original() -> void:
	var transport := FakeTransport.new(self)
	var client := make_client(transport)
	transport.enqueue(200, success(session_json()))
	await client.identity.guest("device-1")
	transport.enqueue(401, '{"error":{"code":"AUTH_TOKEN_EXPIRED","message":"expired","retryable":false}}')
	transport.enqueue(401, '{"error":{"code":"AUTH_REFRESH_REUSED","message":"replay","retryable":false}}')
	transport.enqueue(200, success('{"ok":true}'))  # 不会被消费
	var r: Dictionary = await client.api.request("GET", "/x", null, false)
	check(err_wire(r) == "AUTH_TOKEN_EXPIRED", "refresh 失败应返回原 401(实际 %s)" % err_wire(r))
	check(transport.requests.size() == 3, "应共 3 次请求(guest/过期/refresh)")


# ---- 安全事件:refresh REUSED → 本地清场(重登) ----
func t_security_event_clears() -> void:
	var transport := FakeTransport.new(self)
	var store := CourierTokenStore.MemoryTokenStore.new()
	var client := make_client(transport, null, store)
	transport.enqueue(200, success(session_json()))
	await client.identity.guest("device-1")
	transport.enqueue(401, '{"error":{"code":"AUTH_REFRESH_REUSED","message":"replay","retryable":false}}')
	var r: Dictionary = await client.session.refresh()
	check(not bool(r.get("ok", true)), "安全事件后 refresh 应失败")
	check(store.load().is_empty(), "安全事件应本地清场(需重登)")


# ---- refresh 单飞:并发共享一次轮换 ----
func t_refresh_single_flight() -> void:
	var transport := FakeTransport.new(self)
	var store := CourierTokenStore.MemoryTokenStore.new()
	var client := make_client(transport, null, store)
	transport.enqueue(200, success(session_json()))
	await client.identity.guest("device-1")
	transport.enqueue_fn(200, success(session_json("access-9")))
	# GDScript 禁止无 await 调协程(含 Callable.call):用 call_deferred 起第一个,
	# 多帧 transport 延迟拉长在途窗口,第二个并发进入共享分支。
	transport.delay_frames = 4
	client.session.call_deferred("refresh")
	await process_frame  # 自身即 SceneTree
	check(bool(client.session._refresh_in_flight), "refresh#1 应在途")
	client.session.call_deferred("refresh")
	for i in range(12):
		if not bool(client.session._refresh_in_flight):
			break
		await process_frame
	check(transport.requests.size() == 2, "guest + 一次 refresh(实际 %d)" % transport.requests.size())
	check(str(store.load().get("accessToken", "")) == "access-9", "共享轮换应已落库")


# ---- 登出:吊销尽力而为,本地必定清场 ----
func t_logout_best_effort() -> void:
	var transport := FakeTransport.new(self)
	var store := CourierTokenStore.MemoryTokenStore.new()
	var client := make_client(transport, null, store)
	transport.enqueue(200, success(session_json()))
	await client.identity.guest("device-1")
	transport.enqueue(200, success("null"))
	await client.session.logout()
	check(store.load().is_empty(), "登出后应清场")
	# 服务端失败也清本地(尽力而为)
	transport.enqueue(200, success(session_json()))
	await client.identity.guest("device-1")
	for i in range(3):
		transport.enqueue(500, '{"error":{"code":"COMMON_INTERNAL","message":"boom","retryable":true}}')
	await client.session.logout()
	check(store.load().is_empty(), "吊销失败也必须本地清场")


# ---- bind:Bearer 请求,返回账号不落会话 ----
func t_bind_keeps_session() -> void:
	var transport := FakeTransport.new(self)
	var store := CourierTokenStore.MemoryTokenStore.new()
	var client := make_client(transport, null, store)
	transport.enqueue(200, success(session_json()))
	await client.identity.guest("device-1")
	transport.enqueue(200, success('{"id":"acc_1","type":"EMAIL","email":"a@b.c","status":"ACTIVE","createdAt":"2026-10-09T12:00:00.000Z"}'))
	var r: Dictionary = await client.identity.bind("a@b.c", "pw")
	var req: Dictionary = transport.requests[1]
	check(req["method"] == "POST", "bind 应为 POST")
	check(str(req["headers"].get("Authorization", "")) == "Bearer access-1", "bind 应带 Bearer")
	check(JSON.parse_string(str(req.get("jsonBody", ""))) == {"email": "a@b.c", "password": "pw"}, "bind body 不符")
	check(r["data"]["type"] == "EMAIL", "bind 应返回转正账号")
	check(str(store.load().get("accessToken", "")) == "access-1", "bind 不应覆盖会话")


# ---- 501 判定:is_capability_disabled 命中降级信号 ----
func t_capability_disabled() -> void:
	var transport := FakeTransport.new(self)
	var client := make_client(transport)
	transport.enqueue(501, '{"error":{"code":"COMMON_CAPABILITY_DISABLED","message":"not configured","retryable":false}}')
	var r: Dictionary = await client.api.request("GET", "/x", null, false)
	check(CourierApiError.is_capability_disabled(r.get("error", {})), "501 应命中降级信号")
	check(not CourierApiError.is_capability_disabled("x"), "非错误对象不命中")
	check(not CourierApiError.is_capability_disabled(null), "null 不命中")


# ---- endpoint 尾斜杠归一 + 配置校验(gameId/env/transport 必填) ----
func t_config_validation() -> void:
	var transport := FakeTransport.new(self)
	var client := make_client(transport, null, null,
			{"endpoint": "https://api.example.com/", "gameId": "game_demo", "env": "prod"})
	transport.enqueue(200, success("{}"))
	await client.api.request("GET", "/x", null, false)
	check(transport.requests[0]["url"] == "https://api.example.com/x", "尾斜杠应归一")
	var bad := CourierClient.new({"config": {"endpoint": "https://api.example.com", "gameId": " ", "env": "prod"}, "transport": transport})
	check(not bad.valid, "gameId 空白应校验失败")
	var no_transport := CourierClient.new({"config": {"endpoint": "https://api.example.com", "gameId": "g", "env": "prod"}})
	check(not no_transport.valid, "缺 transport 应校验失败")


# ---- 未登记 wire code 容忍:原样透传 + 兜底枚举名空 ----
func t_unknown_wire_tolerated() -> void:
	var transport := FakeTransport.new(self)
	var client := make_client(transport)
	transport.enqueue(400, '{"error":{"code":"FUTURE_CODE_X","message":"new server","retryable":false}}')
	var r: Dictionary = await client.api.request("GET", "/x", null, false)
	var e: Dictionary = r.get("error", {})
	check(err_wire(r) == "FUTURE_CODE_X", "未知 wire 应原样透传")
	check(str(e.get("code", "")) == "", "未知码枚举名应落兜底空串")
	check(not bool(e.get("retryable", true)), "未知码 retryable 应按信封 false")


# ---- 会话 DTO 全字段 wire 对齐(auth.md) ----
func t_session_dto_shape() -> void:
	var transport := FakeTransport.new(self)
	var client := make_client(transport)
	transport.enqueue(200, success(session_json()))
	var r: Dictionary = await client.identity.guest("device-1")
	var s: Dictionary = r["data"]
	var keys: Array = s.keys()
	keys.sort()
	check(keys == ["accessExpiresAt", "accessToken", "account", "deviceId", "refreshExpiresAt", "refreshToken"],
			"会话 DTO 键应与契约对齐(实际 %s)" % str(keys))
	check(str(s.get("account", {}).get("type", "")) == "GUEST", "账号类型应为 GUEST")
	check(not s.get("account", {}).has("email"), "GUEST 时 email 缺省")


# ---- 门面:guest_login 全流(init → Ready → PlayerReady) ----
func t_facade_auth_flow() -> void:
	var transport := FakeTransport.new(self)
	var client := make_client(transport)
	var rec := Recorder.new()
	rec.attach(client.lifecycle)
	client.init()
	check(client.lifecycle.state == CourierLifecycle.READY, "init 后应为 Ready")
	check(rec.types() == ["lifecycle.initialized"], "init 应发 initialized 事件")
	transport.enqueue(200, success(session_json()))
	var r: Dictionary = await client.guest_login("device-1", "android")
	check(r.get("ok", false), "guest_login 应成功")
	check(client.lifecycle.state == CourierLifecycle.PLAYER_READY,
			"登录成功应到 PlayerReady(实际 %s)" % client.lifecycle.state)
	var bad: Dictionary = await client.guest_login("device-2")
	check(not bool(bad.get("ok", true)), "PlayerReady 上再登录应被守卫拒绝")
	check(client.lifecycle.state == CourierLifecycle.PLAYER_READY, "守卫拒绝不应改状态")


# ---- 门面:认证失败回退(AuthFailed → Ready) ----
func t_facade_auth_failure() -> void:
	var transport := FakeTransport.new(self)
	var client := make_client(transport)
	client.init()
	transport.enqueue(401, '{"error":{"code":"AUTH_INVALID_CREDENTIALS","message":"bad credentials","retryable":false}}')
	var r: Dictionary = await client.guest_login("device-1")
	check(not bool(r.get("ok", true)), "凭证错误应失败")
	check(client.lifecycle.state == CourierLifecycle.READY, "认证失败应回退 Ready(实际 %s)" % client.lifecycle.state)


func _init() -> void:
	await t_guest_wire()
	await t_bearer()
	await t_unauthenticated_precheck()
	await t_typed_error_envelope()
	await t_fallback_wire()
	await t_retry()
	await t_retry_exhausted()
	await t_network_failure()
	await t_token_expired_replay()
	await t_refresh_failure_keeps_original()
	await t_security_event_clears()
	await t_refresh_single_flight()
	await t_logout_best_effort()
	await t_bind_keeps_session()
	await t_capability_disabled()
	await t_config_validation()
	await t_unknown_wire_tolerated()
	await t_session_dto_shape()
	await t_facade_auth_flow()
	await t_facade_auth_failure()
	print("godot core tests: %s (%d failed)" % ["all green" if failed == 0 else "FAILED", failed])
	quit(1 if failed > 0 else 0)
