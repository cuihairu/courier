# Courier Godot SDK 最小可跑示例:接入 → 登录 → 收公告 → 发工单。
#
# SDK 纯逻辑层(core/service)零引擎场景依赖,headless 即可实测全链路;
# 传输用平台件 CourierHTTPTransport(Godot HTTPClient),与游戏内同一份代码。
#
# 运行(从仓库根):
#   1) 起本地网关:cd gateway && go build -o /tmp/courier-gateway ./cmd/gateway
#      && /tmp/courier-gateway(默认 :8080,COURIER_GATEWAY_ADDR 可覆盖)
#   2) godot --headless --path sdks/godot \
#        --script <仓库绝对路径>/examples/godot/example.gd
#      (--script 相对路径按 res:// 解析,故示例在工程外须用绝对路径;
#       端点默认 http://127.0.0.1:8080,可用环境变量 COURIER_ENDPOINT 覆盖。)
#
# 接入点只有两个:config(gameId/env = scope 头)+ Transport/TokenStore 适配件。
extends SceneTree


func _yield_frame() -> void:
	await process_frame  # HTTPClient 为 poll 增量模型,帧间让出(--script 无主循环)


func _init() -> void:
	var endpoint := OS.get_environment("COURIER_ENDPOINT")
	if endpoint == "":
		endpoint = "http://127.0.0.1:8080"

	# 1) 接入:config 是 scope(gameId/env),传输为平台件;tokenStore 缺省内存存储,
	#    正式接入换 FileTokenStore(架构红线:禁明文偏好存储落令牌)。
	var transport := CourierHTTPTransport.new()
	transport.frame_f = Callable(self, "_yield_frame")
	var client := CourierClient.new({
		"config": {"endpoint": endpoint, "gameId": "game_demo", "env": "prod"},
		"transport": transport,
	})
	var services := CourierServices.new(client)

	# 初始化:空存储不发请求,只是把生命周期机置 Ready(后续登录编排需要)。
	client.init()

	# 2) 登录:游客静默登录(不阻断进游戏)。
	var login: Dictionary = await client.guest_login("example-device-1")
	if not bool(login.get("ok", false)):
		print("login failed: ", login.get("error", {}).get("wire", "?"))
		quit(1)
		return
	var account: Dictionary = login["data"].get("account", {})
	print("login: type=%s id=%s" % [account.get("type", ""), account.get("id", "")])

	# 3) 收公告:匿名可见;本地网关无发布数据时空列表属正常。
	var page: Dictionary = await services.announcements.list(20)
	if not bool(page.get("ok", false)):
		print("announcements failed: ", page.get("error", {}).get("wire", "?"))
		quit(1)
		return
	var items: Array = page["data"].get("items", [])
	print("announcements: %d" % items.size())
	for item in items:
		print("  [%s] %s" % [item.get("severity", ""), item.get("title", "")])

	# 4) 发工单:提交后拉详情,消息含玩家首条正文。
	var ticket: Dictionary = await services.support.create_ticket({
		"title": "示例:闪退反馈",
		"body": "启动即崩溃,登录账号 %s" % account.get("id", ""),
	})
	if not bool(ticket.get("ok", false)):
		print("create ticket failed: ", ticket.get("error", {}).get("wire", "?"))
		quit(1)
		return
	print("ticket: id=%s status=%s" % [ticket["data"].get("id", ""), ticket["data"].get("status", "")])
	var detail: Dictionary = await services.support.get_ticket(ticket["data"]["id"])
	if not bool(detail.get("ok", false)):
		print("ticket detail failed: ", detail.get("error", {}).get("wire", "?"))
		quit(1)
		return
	print("detail: messages=%d" % detail["data"].get("messages", []).size())
	print("example: done")
	quit(0)
