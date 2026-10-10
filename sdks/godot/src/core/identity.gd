# L2 Core 身份域(契约 auth.md F1–F4):register/login/guest 匿名,bind 需 Bearer。
# 成功响应一律是新会话(bind 除外:返回账号)——有 accessToken 即经 on_session 交还
# 持有方(client 接线为 session.adopt)。refresh/restore/登出归 CourierSessionService。
# 生命周期触发由 CourierClient.auth_flow 统一发起(与 TS authFlow 同构)。

class_name CourierIdentityService

const PREFIX := "/v1/identity/"

var api: CourierApiClient
var on_session: Callable = Callable()


func _init(a: CourierApiClient, on_session_fn: Callable):
	api = a
	on_session = on_session_fn


func _send(method: String, action: String, body: Dictionary, with_auth: bool) -> Dictionary:
	var data: Dictionary = await api.request(method, PREFIX + action, body, with_auth)
	if data.get("ok", false) and data.get("data", {}).has("accessToken"):
		on_session.call(data["data"])
	return data


func register(email: String, password: String) -> Dictionary:
	return await _send("POST", "register", {"email": email, "password": password}, false)


func login(email: String, password: String) -> Dictionary:
	return await _send("POST", "login", {"email": email, "password": password}, false)


func guest(device_id: String, platform: String = "") -> Dictionary:
	var body := {"deviceId": device_id}
	if platform != "":
		body["platform"] = platform
	return await _send("POST", "guest", body, false)


func bind(email: String, password: String) -> Dictionary:
	return await _send("POST", "bind", {"email": email, "password": password}, true)
