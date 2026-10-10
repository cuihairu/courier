# L2 Core 会话域(契约 auth.md F5–F8):冷启动恢复、refresh 单飞轮换、
# 登出吊销、设备列表。安全事件(REFRESH_REUSED/TOKEN_REVOKED)→ 本地清场。
# 两段装配:session 先建,api 建成后回填(环只能延迟接线)。

class_name CourierSessionService

signal _refresh_completed(result: Dictionary)

var api: CourierApiClient
var store: CourierTokenStore
var lifecycle: CourierLifecycle
var _refresh_in_flight: bool = false


func _init(s: CourierTokenStore, lc: CourierLifecycle):
	store = s
	lifecycle = lc


## 两段装配(内部接线点)。
func attach_api(a: CourierApiClient):
	api = a


## 当前会话(冷启动尝试从 store 恢复)。
func current() -> Dictionary:
	var s: Dictionary = store.load()
	return s if not s.is_empty() else {}


## 当前 access token。
func access_token() -> String:
	var s: Dictionary = store.load()
	return str(s.get("accessToken", "")) if not s.is_empty() else ""


## 落库新会话。
func adopt(session: Dictionary):
	store.save(session)
	var aid := str(session.get("account", {}).get("id", ""))
	lifecycle.report_account_id(aid)


## 本地清场。
func clear():
	store.clear()


## refresh 轮换(单飞:并发调用共享一次请求)。返回 {ok} 或 {ok:false, error}。
func refresh() -> Dictionary:
	if _refresh_in_flight:
		var r: Variant = await _refresh_completed
		return r if typeof(r) == TYPE_DICTIONARY else {"ok": false}
	_refresh_in_flight = true
	var result: Dictionary = await _try_refresh()
	_refresh_in_flight = false
	_refresh_completed.emit(result)
	return result


func _try_refresh() -> Dictionary:
	var cur: Dictionary = store.load()
	if cur.is_empty():
		return {"ok": false}
	lifecycle.raise_token_expired()
	var r: Dictionary = await api.request("POST", "/v1/identity/refresh",
			{"refreshToken": str(cur.get("refreshToken", ""))}, false)
	if r.get("ok", false):
		store.save(r["data"])
		return {"ok": true}
	var err: Dictionary = r.get("error", {})
	var wire := CourierApiError.wire(err)
	if wire == "AUTH_REFRESH_REUSED" or wire == "AUTH_TOKEN_REVOKED":
		# 安全事件:本地清场 + 生命周期登出(重登;契约 auth.md「重放检测」)。
		store.clear()
		lifecycle.try_fire(CourierLifecycle.SIGNED_OUT_TRIGGER)
		return {"ok": false}
	# 其他刷新失败向上抛(返回 refresh 错误,由 ApiClient 透传给调用方)。
	return {"ok": false, "error": err}


## 登出:吊销当前会话(尽力而为)+ 本地清场。
func logout() -> Dictionary:
	var r: Dictionary = await api.request("POST", "/v1/identity/logout", {}, true)
	clear()
	lifecycle.try_fire(CourierLifecycle.SIGNED_OUT_TRIGGER)
	return r


## 当前会话信息。
func info() -> Dictionary:
	return await api.request("GET", "/v1/identity/session", {}, true)


## 已绑定设备列表(F8)。
func list_devices() -> Dictionary:
	return await api.request("GET", "/v1/identity/devices", {}, true)


## 解绑设备并吊销其全部会话(F9;deviceId 需 URL 编码)。
func unbind_device(device_id: String) -> Dictionary:
	return await api.request("DELETE", "/v1/identity/devices/" + device_id.uri_encode(), {}, true)
