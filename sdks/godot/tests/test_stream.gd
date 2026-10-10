# L3 平台流式传输单测:CourierSseStream 对 TCPServer mock 的真 SSE 长连——
# 帧解析上抛/心跳忽略、断线重连(token 每连现取)、501 降级收尾、401 收尾、
# 跨 chunk 行缓冲。运行(从仓库根):
#   godot --headless --path sdks/godot --script tests/test_stream.gd
extends SceneTree

const MockServer = preload("res://tests/mock_server.gd")

var failed: int = 0
var _events: Array = []
var _closed_reason: String = ""
var _stop_flag: bool = false
var _stream_done: bool = false
var _tok_count: int = 0
var _stream: CourierSseStream  # GDScript Callable 弱引用目标:持活被点火协程的宿主


func check(cond: bool, msg: String) -> void:
	if not cond:
		print("FAIL: " + msg)
		failed += 1


func _yield_frame() -> void:
	await process_frame


func _next_token() -> String:
	_tok_count += 1
	return "tok-%d" % _tok_count


func _on_evt(evt: Variant) -> void:
	_events.append(evt)


func _on_closed(reason: String) -> void:
	_closed_reason = reason


func _should_stop() -> bool:
	return _stop_flag


func _start_stream(s: CourierSseStream, url: String) -> void:
	await s.stream(url, {"X-Courier-Game-Id": "game_demo", "X-Courier-Env": "prod"}, Callable(self, "_next_token"), Callable(self, "_on_evt"), Callable(self, "_on_closed"), Callable(self, "_should_stop"))
	_stream_done = true


func pump_until(mock: MockServer, cond: Callable) -> void:
	var guard := 0
	while guard < 900:
		guard += 1
		if bool(cond.call()):
			return
		await mock.pump(self)
	check(bool(cond.call()), "条件应在 900 帧内满足")


func new_stream() -> CourierSseStream:
	_events = []
	_closed_reason = ""
	_stop_flag = false
	_stream_done = false
	_tok_count = 0
	var s := CourierSseStream.new()
	s.frame_f = Callable(self, "_yield_frame")
	s.backoff_base_ms = 0
	_stream = s
	return s


# ---- 帧上抛 + 心跳忽略 + 断线重连(重连现取 token) ----

func t_stream_frames_reconnect() -> void:
	var mock := MockServer.new()
	mock.enqueue_stream("id: 1\nevent: announcement.published\ndata: {\"id\":\"ann_1\",\"title\":\"t\"}\n\n: ping\n\n")
	var s := new_stream()
	_start_stream.call_deferred(s, "http://127.0.0.1:%d/v1/messages/stream" % mock.port)
	await pump_until(mock, func(): return _events.size() >= 1)
	check(_events.size() == 1, "心跳注释帧不应出事件")
	if _events.size() >= 1:
		var e0: Dictionary = _events[0]
		check(str(e0.get("type")) == "announcement.published", "event 字段应成 type")
		check(str(e0.get("id")) == "1", "id 字段应透传")
		check(str(e0.get("data")).contains("ann_1"), "data 应为单行 JSON 原文")
	# 流内推送第二帧(mock 长连 held 不断)
	mock.pushes.append("id: 2\nevent: support.ticket_replied\ndata: {\"ticketId\":\"tkt_1\"}\n\n")
	await pump_until(mock, func(): return _events.size() >= 2)
	if _events.size() >= 2:
		check(str(_events[1].get("type")) == "support.ticket_replied", "第二帧应上抛")
	# 服务端断流 → 退避重连(backoff 0)→ 重连现取 token(契约:新连接按当前 token 校验)
	mock.hangup()
	mock.enqueue_stream("id: 3\nevent: config.updated\ndata: {\"configVersion\":7}\n\n")
	await pump_until(mock, func(): return _events.size() >= 3)
	check(mock.seen.size() == 2, "应恰好重连一次(实得 %d)" % mock.seen.size())
	if mock.seen.size() >= 2:
		var req1: Dictionary = mock.seen[1]
		check(str(req1["headers"].get("authorization", "")) == "Bearer tok-2", "重连应按现取 token 带 Bearer")
		check(str(req1["headers"].get("x-courier-game-id", "")) == "game_demo", "scope 头应随流连接")
	if _events.size() >= 3:
		check(str(_events[2].get("type")) == "config.updated", "重连后帧应续上")
	# 净停:stop 置真,协程收尾,不再重连
	_stop_flag = true
	await pump_until(mock, func(): return _stream_done)
	check(_closed_reason == "", "主动停止不应报终态")
	check(mock.seen.size() == 2, "停止后不应再连(实得 %d)" % mock.seen.size())
	mock.stop()


# ---- 501 能力关:降级收尾,不重试 ----

func t_stream_capability_off() -> void:
	var mock := MockServer.new()
	var body := '{"error":{"code":"COMMON_CAPABILITY_DISABLED","message":"off","retryable":false}}'
	mock.responses.append("HTTP/1.1 501 Not Implemented\r\nContent-Length: %d\r\n\r\n%s" % [body.length(), body])
	var s := new_stream()
	_start_stream.call_deferred(s, "http://127.0.0.1:%d/v1/messages/stream" % mock.port)
	await pump_until(mock, func(): return _stream_done)
	check(_closed_reason == "capability_disabled", "501 应收尾 capability_disabled(实得 %s)" % _closed_reason)
	check(mock.seen.size() == 1, "降级收尾不应重试")
	check(_events.is_empty(), "降级不应出事件")
	mock.stop()


# ---- 401 未认证:收尾不重试 ----

func t_stream_unauthenticated() -> void:
	var mock := MockServer.new()
	var body := '{"error":{"code":"COMMON_UNAUTHENTICATED","message":"anon","retryable":false}}'
	mock.responses.append("HTTP/1.1 401 Unauthorized\r\nContent-Length: %d\r\n\r\n%s" % [body.length(), body])
	var s := new_stream()
	_start_stream.call_deferred(s, "http://127.0.0.1:%d/v1/messages/stream" % mock.port)
	await pump_until(mock, func(): return _stream_done)
	check(_closed_reason == "unauthenticated", "401 应收尾 unauthenticated(实得 %s)" % _closed_reason)
	check(mock.seen.size() == 1, "401 收尾不应重试")
	mock.stop()


# ---- 跨 chunk 行缓冲:字段名被切段也应正确结算 ----

func t_stream_chunked_lines() -> void:
	var mock := MockServer.new()
	mock.enqueue_stream("")
	var s := new_stream()
	_start_stream.call_deferred(s, "http://127.0.0.1:%d/v1/messages/stream" % mock.port)
	mock.pushes.append("id: 9\nev")
	mock.pushes.append("ent: config.updated\ndata: {\"configVersion\":7}\n\n")
	await pump_until(mock, func(): return _events.size() >= 1)
	check(_events.size() == 1, "跨 chunk 两段应结算一帧(实得 %d)" % _events.size())
	if _events.size() >= 1:
		var e0: Dictionary = _events[0]
		check(str(e0.get("type")) == "config.updated", "切段字段名应拼回")
		check(str(e0.get("id")) == "9", "切段 id 行应完整")
		check(str(e0.get("data")) == '{"configVersion":7}', "data 应原文")
	mock.hangup()
	_stop_flag = true
	await pump_until(mock, func(): return _stream_done)
	mock.stop()


func _init() -> void:
	await t_stream_frames_reconnect()
	await t_stream_capability_off()
	await t_stream_unauthenticated()
	await t_stream_chunked_lines()
	print("godot stream tests: %s (%d failed)" % ["all green" if failed == 0 else "FAILED", failed])
	quit(1 if failed > 0 else 0)
