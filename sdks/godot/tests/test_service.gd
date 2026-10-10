# M2 服务域单测(cocos service.test.ts 同构):SSE 帧解析 + 公告/客服 wire 形状与
# typed 错误。运行(从仓库根):
#   godot --headless --path sdks/godot --script tests/test_service.gd
extends SceneTree

var failed: int = 0
# GDScript 语义:Callable 弱引用目标(client 被回收则 token_provider 失效,
# Bearer 预检即失败)。用例帧经此持活当前 client(与接入方持 CourierClient 同构)。
var _client: CourierClient


class FakeTransport extends CourierTransport:
	# 脚本化假传输:记录请求,按队列回放响应;每次 send 让出一帧
	# (模拟真实网络往返,协程语义与 TS Promise 对齐)。
	var tree: SceneTree
	var requests: Array = []
	var queue: Array = []


	func _init(t: SceneTree) -> void:
		tree = t


	func enqueue(status_code: int, body: String) -> void:
		queue.append({"status_code": status_code, "body": body})


	func send(request: Dictionary) -> Dictionary:
		await tree.process_frame
		requests.append(request)
		if queue.is_empty():
			return {"ok": false, "message": "脚本耗尽:" + str(request.get("method", "")) + " " + str(request.get("url", ""))}
		var item: Dictionary = queue.pop_front()
		return {"ok": true, "status_code": int(item["status_code"]), "headers": {}, "body": str(item["body"])}


func check(cond: bool, msg: String) -> void:
	if not cond:
		print("FAIL: " + msg)
		failed += 1


func session_json() -> String:
	return '{"account":{"id":"acc_1","type":"GUEST","status":"ACTIVE","createdAt":"t"},"accessToken":"access-1","accessExpiresAt":"t","refreshToken":"refresh-1","refreshExpiresAt":"t","deviceId":"device-1"}'


func success(raw: String) -> String:
	return '{"data":' + raw + "}"


func err_wire(r: Dictionary) -> String:
	return str(r.get("error", {}).get("wire", ""))


func new_services(transport: FakeTransport) -> CourierServices:
	var options := {
		"config": {"endpoint": "https://api.example.com", "gameId": "game_demo", "env": "prod"},
		"transport": transport,
	}
	_client = CourierClient.new(options)
	transport.enqueue(200, success(session_json()))
	await _client.identity.guest("device-1")
	return CourierServices.new(_client)


# ---- SSE 帧解析(契约 messages.md) ----

func t_sse_standard() -> void:
	var p := CourierSseParser.new()
	check(p.feed_line(": connected") == null, "连接初始化注释应无事件")
	check(p.feed_line("") == null, "注释后空行应无帧结算")
	check(p.feed_line(": ping") == null, "心跳注释应无事件")

	check(p.feed_line("event: announcement.published") == null, "event 行不结算")
	check(p.feed_line("id: 7") == null, "id 行不结算")
	check(p.feed_line("data: {\"id\":\"ann_1\"}") == null, "data 行不结算")
	var evt: Variant = p.feed_line("")
	check(evt != null, "空行应结算出事件")
	check(str(evt["type"]) == "announcement.published", "type 应解析(实际 %s)" % str(evt.get("type")))
	check(str(evt.get("id", "")) == "7", "id 应解析")
	check(str(evt["data"]) == "{\"id\":\"ann_1\"}", "data 应剥冒号后单个空格")

	# 纯注释帧不结算事件
	check(p.feed_line(": ping") == null, "心跳注释应无事件")
	check(p.feed_line("") == null, "纯注释帧空行不应出事件")


func t_sse_multiline() -> void:
	var p := CourierSseParser.new()
	p.feed_line("event: x.y")
	p.feed_line("data:line1")  # 冒号无空格也解析
	p.feed_line("data: line2")
	var evt: Variant = p.feed_line("")
	check(evt != null, "多行 data 应结算出事件")
	check(str(evt["data"]) == "line1\nline2", "多行 data 应按 \\n 连接(实际 %s)" % str(evt["data"]))


func t_sse_unknown_reset() -> void:
	var p := CourierSseParser.new()
	p.feed_line("event: a.b")
	p.feed_line("retry: 3000")  # 未知字段容忍(契约 versioning.md)
	p.feed_line("data: 1")
	check(p.feed_line("") != null, "正常帧应结算")
	p.reset()  # 半帧残留清空
	p.feed_line("data: leftover-from-old-frame")
	var evt: Variant = p.feed_line("")
	check(evt != null, "reset 后新帧应结算")
	check(str(evt["type"]) == "", "reset 后 type 应为空")
	check(str(evt["data"]) == "leftover-from-old-frame", "reset 应清残留")


# ---- 公告域(契约 announcement.md) ----

func t_announcement_list() -> void:
	var transport := FakeTransport.new(self)
	var svc := await new_services(transport)
	transport.enqueue(200, success('{"items":[{"id":"ann_1","title":"维护公告","body":"今晚维护","severity":"INFO","startAt":"2026-10-09T00:00:00.000Z","endAt":"2026-10-10T00:00:00.000Z","publishedAt":"2026-10-09T08:00:00.000Z"}],"nextCursor":"cur-2"}'))

	var page: Dictionary = await svc.announcements.list(20, "cur-1")

	var req: Dictionary = transport.requests[1]
	check(req["method"] == "GET", "列表应为 GET")
	check(str(req["url"]) == "https://api.example.com/v1/announcements?limit=20&cursor=cur-1",
			"列表 URL 不符:" + str(req["url"]))
	check(str(req["headers"].get("Authorization", "")) == "Bearer access-1", "列表应带 Bearer")
	check(str(page["data"]["items"][0]["title"]) == "维护公告", "分页 items 应解析")
	check(str(page["data"]["nextCursor"]) == "cur-2", "游标应解析")


func t_announcement_not_found() -> void:
	var transport := FakeTransport.new(self)
	var svc := await new_services(transport)
	transport.enqueue(404, '{"error":{"code":"ANNOUNCEMENT_NOT_FOUND","message":"不可见","retryable":false}}')

	var r: Dictionary = await svc.announcements.get_announcement("ann_x")
	var e: Dictionary = r.get("error", {})
	check(err_wire(r) == "ANNOUNCEMENT_NOT_FOUND", "详情错误应为 typed wire")
	check(str(e.get("code", "")) == "AnnouncementNotFound", "枚举名应为 AnnouncementNotFound(实得 %s)" % str(e.get("code")))
	check(int(e.get("http", 0)) == 404, "http 应 404")
	check(transport.requests.size() == 2, "404 不重试(实际 %d)" % transport.requests.size())


# ---- 客服域(契约 support.md) ----

func t_support_create() -> void:
	var transport := FakeTransport.new(self)
	var svc := await new_services(transport)
	transport.enqueue(200, success('{"id":"tkt_1","title":"[助手转人工] 充值没到账","status":"OPEN","category":"PAYMENT","createdAt":"t","updatedAt":"t"}'))

	var ticket: Dictionary = await svc.support.create_ticket(
			{"title": "充值没到账", "body": "订单未发货", "category": "PAYMENT"})

	var req: Dictionary = transport.requests[1]
	check(req["method"] == "POST", "提单应为 POST")
	check(str(req["url"]) == "https://api.example.com/v1/support/tickets", "提单 URL 不符")
	check(JSON.parse_string(str(req.get("jsonBody", ""))) == {"title": "充值没到账", "body": "订单未发货", "category": "PAYMENT"},
			"提单 body 不符")
	check(str(ticket["data"]["status"]) == "OPEN", "工单应解析")

	# category 缺省:键不下发
	transport.enqueue(200, success('{"id":"tkt_2","title":"t","status":"OPEN","createdAt":"t","updatedAt":"t"}'))
	await svc.support.create_ticket({"title": "t", "body": "b"})
	var raw: Dictionary = JSON.parse_string(str(transport.requests[2].get("jsonBody", "")))
	check(not raw.has("category"), "category 缺省键不应下发")


func t_support_detail_append() -> void:
	var transport := FakeTransport.new(self)
	var svc := await new_services(transport)
	transport.enqueue(200, success('{"ticket":{"id":"tkt_1","title":"问题","status":"REPLIED","category":"OTHER","createdAt":"t","updatedAt":"t2"},"messages":[{"senderType":"PLAYER","body":"充值没到账","createdAt":"t"},{"senderType":"AGENT","body":"已补发","createdAt":"t2"}]}'))

	var detail: Dictionary = await svc.support.get_ticket("tkt_1")
	check(str(transport.requests[1]["url"]) == "https://api.example.com/v1/support/tickets/tkt_1",
			"详情 URL 不符")
	check(str(detail["data"]["ticket"]["status"]) == "REPLIED", "工单状态应解析")
	check(detail["data"]["messages"].size() == 2, "消息应解析")
	check(str(detail["data"]["messages"][1]["senderType"]) == "AGENT", "坐席消息应解析")

	transport.enqueue(409, '{"error":{"code":"SUPPORT_TICKET_CLOSED","message":"已关单","retryable":false}}')
	var r: Dictionary = await svc.support.append_message("tkt_1", "再问一句")
	var e: Dictionary = r.get("error", {})
	check(str(e.get("code", "")) == "SupportTicketClosed", "关单后追加应 typed 409(实得 %s)" % str(e.get("code")))
	check(int(e.get("http", 0)) == 409, "http 应 409")
	check(transport.requests.size() == 3, "409 不重试(实际 %d)" % transport.requests.size())
	check(str(transport.requests[2]["url"]) == "https://api.example.com/v1/support/tickets/tkt_1/messages",
			"追加 URL 不符")
	check(JSON.parse_string(str(transport.requests[2].get("jsonBody", ""))) == {"body": "再问一句"},
			"追加 body 不符")


func t_support_faq() -> void:
	var transport := FakeTransport.new(self)
	var svc := await new_services(transport)
	transport.enqueue(200, success('{"items":[{"id":"faq_1","question":"怎么找回账号","answer":"点忘记密码"}],"nextCursor":""}'))

	var page: Dictionary = await svc.support.faq(20, "找回 账号")

	check(str(transport.requests[1]["url"]) == "https://api.example.com/v1/support/faq?limit=20&keyword=" + "找回 账号".uri_encode(),
			"关键词应 URL 编码(实际 %s)" % str(transport.requests[1]["url"]))
	check(str(page["data"]["items"][0]["id"]) == "faq_1", "FAQ 应解析")

	transport.enqueue(200, success('{"items":[],"nextCursor":""}'))
	await svc.support.faq(20)
	check(str(transport.requests[2]["url"]) == "https://api.example.com/v1/support/faq?limit=20",
			"空关键词不带参数")


func _init() -> void:
	await t_sse_standard()
	await t_sse_multiline()
	await t_sse_unknown_reset()
	await t_announcement_list()
	await t_announcement_not_found()
	await t_support_create()
	await t_support_detail_append()
	await t_support_faq()
	print("godot service tests: %s (%d failed)" % ["all green" if failed == 0 else "FAILED", failed])
	quit(1 if failed > 0 else 0)
