# M3/M4/M5 服务域单测(cocos service2.test.ts 同构):app/config/branding/player/
# assistant/payment/realname 的 wire 形状、501 降级语义(data:null)、typed 错误、
# 缓存判据。运行(从仓库根):
#   godot --headless --path sdks/godot --script tests/test_service2.gd
extends SceneTree

var failed: int = 0
# GDScript 语义:Callable 弱引用目标(client 被回收则 token_provider 失效,
# Bearer 预检即失败)。用例帧经此持活当前 client(与接入方持 CourierClient 同构)。
var _client: CourierClient


class FakeTransport extends CourierTransport:
	# 脚本化假传输:记录请求,按队列回放响应;每次 send 让出一帧。
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


func disabled_body() -> String:
	return '{"error":{"code":"COMMON_CAPABILITY_DISABLED","message":"not configured","retryable":false}}'


func err_code(r: Dictionary) -> String:
	return str(r.get("error", {}).get("code", ""))


func sent_body(req: Dictionary) -> Variant:
	return JSON.parse_string(str(req.get("jsonBody", "")))


func new_services(transport: FakeTransport) -> CourierServices:
	var options := {
		"config": {"endpoint": "https://api.example.com", "gameId": "game_demo", "env": "prod"},
		"transport": transport,
	}
	_client = CourierClient.new(options)
	transport.enqueue(200, success(session_json()))
	await _client.identity.guest("device-1")
	return CourierServices.new(_client)


# ---- App 域(匿名可,501 → data:null) ----

func t_app_anonymous_501() -> void:
	var transport := FakeTransport.new(self)
	var svc := await new_services(transport)

	transport.enqueue(200, success('{"latestVersion":"1.2.0","minVersion":"1.0.0","updateUrl":"https://dl","forceUpdate":false}'))
	var version: Dictionary = await svc.app.check_update("1.1.0", "android")

	check(not transport.requests[1]["headers"].has("Authorization"), "app 域应匿名(无 Bearer)")
	check(str(transport.requests[1]["url"]) == "https://api.example.com/v1/app/version?appVersion=1.1.0&platform=android",
			"版本检查 URL 不符:" + str(transport.requests[1]["url"]))
	check(bool(version["data"]["forceUpdate"]) == false, "forceUpdate 应解析")

	transport.enqueue(501, disabled_body())
	transport.enqueue(501, disabled_body())  # 每端点一份(非 retryable,单次)
	var m: Dictionary = await svc.app.check_maintenance()
	var e: Dictionary = await svc.app.get_environment()
	check(bool(m.get("ok", true)) and m["data"] == null, "维护查询 501 应 data:null")
	check(bool(e.get("ok", true)) and e["data"] == null, "环境回显 501 应 data:null")


func t_app_maintenance_typed() -> void:
	var transport := FakeTransport.new(self)
	var svc := await new_services(transport)
	transport.enqueue(503, '{"error":{"code":"APP_MAINTENANCE","message":"维护中","retryable":false}}')

	var r: Dictionary = await svc.app.check_maintenance()
	check(err_code(r) == "AppMaintenance", "维护中应照常 typed(APP_MAINTENANCE,实得 %s)" % err_code(r))
	check(int(r.get("error", {}).get("http", 0)) == 503, "http 应 503")


# ---- Config 域(缓存 + 重拉判据 + 501 → 空结果) ----

func t_config_cache() -> void:
	var transport := FakeTransport.new(self)
	var svc := await new_services(transport)

	check(svc.config.needs_refetch(7), "从未拉到应一律重拉")
	transport.enqueue(200, success('{"configVersion":7,"items":{"shop_banner":"https://cdn/x.png","limit":3}}'))
	var snapshot: Dictionary = await svc.config.fetch("android", "1.1.0")

	check(str(transport.requests[1]["url"]) == "https://api.example.com/v1/app/config?platform=android&appVersion=1.1.0",
			"配置 URL 不符:" + str(transport.requests[1]["url"]))
	check(int(snapshot["data"]["configVersion"]) == 7, "configVersion 应解析")
	check(int(svc.config.cached_config["configVersion"]) == 7, "快照应入缓存")
	check(str(svc.config.get_value("shop_banner")) == "https://cdn/x.png", "取值应命中")
	check(int(svc.config.get_value("limit")) == 3, "数值取值应命中(JSON 浮点收窄)")
	check(svc.config.get_value("missing") == null, "键缺失应为 null")

	check(not svc.config.needs_refetch(7), "相同 version 应忽略")
	check(svc.config.needs_refetch(8), "不同 version 应重拉")

	transport.enqueue(501, disabled_body())
	var empty: Dictionary = await svc.config.fetch()
	check(empty["data"] == {"configVersion": 0, "items": {}}, "501 应 v0 空 items(不进报错路径)")
	check(int(svc.config.cached_config["configVersion"]) == 7, "失败应沿用缓存")


# ---- Branding 域(匿名,501 → data:null,version 判据) ----

func t_branding() -> void:
	var transport := FakeTransport.new(self)
	var svc := await new_services(transport)

	check(svc.branding.needs_refetch(3), "从未拉到应一律重拉")
	transport.enqueue(200, success('{"version":3,"companyName":"Demo","primaryColor":"#00FF00"}'))
	var r: Dictionary = await svc.branding.fetch()

	check(not transport.requests[1]["headers"].has("Authorization"), "branding 应匿名")
	check(int(r["data"]["version"]) == 3, "version 应解析")
	check(str(r["data"]["companyName"]) == "Demo", "业务字段应透传(SDK 零解释)")
	check(not svc.branding.needs_refetch(3), "相同 version 应忽略")
	check(svc.branding.needs_refetch(4), "不同 version 应重拉")

	transport.enqueue(501, disabled_body())
	var off: Dictionary = await svc.branding.fetch()
	check(bool(off.get("ok", true)) and off["data"] == null, "501 应 data:null(UI 全默认)")


# ---- Player 域 ----

func t_player() -> void:
	var transport := FakeTransport.new(self)
	var svc := await new_services(transport)
	transport.enqueue(200, success('{"displayName":"Player","createdAt":"t1","updatedAt":"t1"}'))

	var profile: Dictionary = await svc.player.get_profile()
	check(str(profile["data"]["displayName"]) == "Player", "档案应解析(懒建)")
	check(not profile["data"].has("avatarUrl"), "可选字段缺省即缺键")

	transport.enqueue(200, success('{"displayName":"新名字","avatarUrl":"https://cdn/a.png","createdAt":"t1","updatedAt":"t2"}'))
	var updated: Dictionary = await svc.player.update_profile("新名字", "https://cdn/a.png")
	check(sent_body(transport.requests[2]) == {"displayName": "新名字", "avatarUrl": "https://cdn/a.png"},
			"PATCH body 不符")
	check(str(updated["data"]["displayName"]) == "新名字", "以响应体为准刷新 UI")

	transport.enqueue(501, disabled_body())
	var off: Dictionary = await svc.player.list_characters()
	check(bool(off.get("ok", true)) and off["data"] == null, "501 应 data:null(隐藏档案 UI)")


# ---- Assistant 域 ----

func t_assistant() -> void:
	var transport := FakeTransport.new(self)
	var svc := await new_services(transport)

	transport.enqueue(200, success('{"matched":true,"answer":{"id":"faq_1","question":"怎么找回账号","answer":"点忘记密码","keywords":["找回"]},"suggestTransfer":false}'))
	var hit: Dictionary = await svc.assistant.query("账号怎么找回")

	check(str(transport.requests[1]["url"]) == "https://api.example.com/v1/assistant/query", "查询 URL 不符")
	check(sent_body(transport.requests[1]) == {"text": "账号怎么找回"}, "查询 body 应只含 text")
	check(bool(hit["data"]["matched"]), "命中应 matched")
	check(str(hit["data"]["answer"]["question"]) == "怎么找回账号", "命中原样快照")

	transport.enqueue(200, success('{"matched":false,"suggestTransfer":true}'))
	var miss: Dictionary = await svc.assistant.query("如何下载游戏")
	check(not bool(miss["data"]["matched"]), "未命中非错误")
	check(bool(miss["data"]["suggestTransfer"]), "未命中应建议转人工")
	check(not miss["data"].has("answer"), "未命中 answer 缺省")

	transport.enqueue(501, disabled_body())
	var off: Dictionary = await svc.assistant.query("q")
	check(bool(off.get("ok", true)) and off["data"] == null, "501 应 data:null(隐藏入口)")


# ---- Payment 域 ----

func t_payment_flow() -> void:
	var transport := FakeTransport.new(self)
	var svc := await new_services(transport)

	transport.enqueue(200, success('{"items":[{"id":"sku_gem_60","productId":"com.demo.gem60","form":"DIRECT_PURCHASE","amountCents":600,"currency":"CNY"}],"nextCursor":""}'))
	var skus: Dictionary = await svc.payments.get_skus()
	check(int(skus["data"]["items"][0]["amountCents"]) == 600, "SKU 价格应解析")

	transport.enqueue(200, success('{"id":"order_1","status":"CREATED","skuId":"sku_gem_60","productId":"com.demo.gem60","form":"DIRECT_PURCHASE","amountCents":600,"currency":"CNY","payToken":"sbox_abc","createdAt":"t","updatedAt":"t"}'))
	var created: Dictionary = await svc.payments.create_order("sku_gem_60")

	check(str(transport.requests[2]["url"]) == "https://api.example.com/v1/payments/orders", "下单 URL 不符")
	check(sent_body(transport.requests[2]) == {"skuId": "sku_gem_60"}, "下单只发 skuId(服务端定价红线)")
	check(str(created["data"]["status"]) == "CREATED", "订单应 CREATED")
	check(str(created["data"]["payToken"]) == "sbox_abc", "payToken 仅下单响应出现")

	transport.enqueue(200, success('{"id":"order_1","status":"DELIVERED","skuId":"sku_gem_60","amountCents":600,"currency":"CNY","createdAt":"t","updatedAt":"t2","paidAt":"t1","deliveredAt":"t2"}'))
	var detail: Dictionary = await svc.payments.get_order("order_1")
	check(str(detail["data"]["status"]) == "DELIVERED", "轮询详情应 DELIVERED")
	check(not detail["data"].has("payToken"), "详情不下发 payToken")


func t_payment_typed() -> void:
	var transport := FakeTransport.new(self)
	var svc := await new_services(transport)
	transport.enqueue(404, '{"error":{"code":"PAYMENT_ORDER_NOT_FOUND","message":"订单不存在","retryable":false}}')

	var r: Dictionary = await svc.payments.get_order("order_other")
	check(err_code(r) == "PaymentOrderNotFound", "他人订单应 typed 不泄露存在性(实得 %s)" % err_code(r))
	check(transport.requests.size() == 2, "404 不重试(实际 %d)" % transport.requests.size())

	transport.enqueue(501, disabled_body())
	var off: Dictionary = await svc.payments.get_skus()
	check(bool(off.get("ok", true)) and off["data"] == null, "501 应 data:null(隐藏商城)")


# ---- RealName 域 ----

func t_realname() -> void:
	var transport := FakeTransport.new(self)
	var svc := await new_services(transport)

	transport.enqueue(200, success('{"state":"VERIFIED","isMinor":true,"verifiedAt":"t"}'))
	var status_r: Dictionary = await svc.realname.submit("张三", "110101199001011234")

	check(str(transport.requests[1]["url"]) == "https://api.example.com/v1/realname/verify", "核验 URL 不符")
	check(sent_body(transport.requests[1]) == {"name": "张三", "idNumber": "110101199001011234"},
			"核验 body 不符(字段只在传输链)")
	check(str(status_r["data"]["state"]) == "VERIFIED", "核验状态应解析")

	transport.enqueue(200, success('{"playable":false,"nextWindowAt":"t2"}'))
	var curfew_r: Dictionary = await svc.realname.curfew()
	check(not bool(curfew_r["data"]["playable"]), "可玩时段应解析")
	check(str(curfew_r["data"]["nextWindowAt"]) == "t2", "下一窗口应解析")

	transport.enqueue(200, success('{"allowed":true,"singleLimitCents":10000,"monthlyLimitCents":100000,"monthlyUsedCents":600}'))
	var charge_r: Dictionary = await svc.realname.charge_check(600)
	var charge_body: Variant = sent_body(transport.requests[3])
	check(charge_body != null and int(charge_body["amountCents"]) == 600, "充值校验 body 应只含 amountCents")
	check(bool(charge_r["data"]["allowed"]), "额度校验应解析")

	transport.enqueue(501, disabled_body())
	var off: Dictionary = await svc.realname.status()
	check(bool(off.get("ok", true)) and off["data"] == null, "501 应 data:null(隐藏实名 UI)")


func _init() -> void:
	await t_app_anonymous_501()
	await t_app_maintenance_typed()
	await t_config_cache()
	await t_branding()
	await t_player()
	await t_assistant()
	await t_payment_flow()
	await t_payment_typed()
	await t_realname()
	print("godot service2 tests: %s (%d failed)" % ["all green" if failed == 0 else "FAILED", failed])
	quit(1 if failed > 0 else 0)
