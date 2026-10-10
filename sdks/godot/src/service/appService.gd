# M3 应用状态客户端(契约 app.md Frozen v1)。三端点匿名可(维护中仍需可查);
# 501 能力未接 → data:null(TS null / UE nullopt 同构:跳过检查直接登录,能力安静地不存在)。
# 503 APP_MAINTENANCE / 426 APP_VERSION_UNSUPPORTED 照常返 typed 错误(信封已登记),
# 调用方据此走维护页/更新引导;SDK 不自动跳转商店。

class_name CourierAppService

const PREFIX := "/v1/app"

var api: CourierApiClient


func _init(a: CourierApiClient) -> void:
	api = a


## 版本检查:app_version 上报参与 forceUpdate 判定(空 = 不判定);platform 可选。
func check_update(app_version: String = "", platform: String = "") -> Dictionary:
	var q := _param("", "appVersion", app_version)
	q = _param(q, "platform", platform)
	return await _send_or_disabled("GET", PREFIX + "/version" + q)


## 维护状态查询:维护页数据源;estimatedRecoveryAt 供轮询节奏。
func check_maintenance() -> Dictionary:
	return await _send_or_disabled("GET", PREFIX + "/maintenance")


## 环境回显:初始化后校验 scope 与网关一致(排查接错环境)。
func get_environment() -> Dictionary:
	return await _send_or_disabled("GET", PREFIX + "/environment")


# App 域降级:501 能力关闭 → {ok:true, data:null}(未启用态);其余错误照常。
# 本域匿名(withAuth=false):维护中未登录也要可查。
func _send_or_disabled(method: String, path: String) -> Dictionary:
	var r: Dictionary = await api.request(method, path, null, false)
	if not bool(r.get("ok", false)) and CourierApiError.is_capability_disabled(r.get("error", {})):
		return {"ok": true, "data": null}
	return r


# 查询串拼接(空值参数不参与过滤;GDScript Dictionary 迭代按插入序,顺序即调用序)。
func _param(q: String, key: String, value: String) -> String:
	if value == "":
		return q
	return q + ("&" if q != "" else "?") + key + "=" + value.uri_encode()
