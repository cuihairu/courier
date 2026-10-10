# M3 远程配置客户端(契约 config.md Frozen v1)。
# 客户端要求:启动/进前台拉取;config.updated 事件到达后经 needs_refetch 判重拉
# (相同 version 即忽略);重拉失败沿用缓存(配置是加速器,不是依赖)。

class_name CourierAppConfigService

const PATH := "/v1/app/config"

var api: CourierApiClient
var _cached: Variant = null


func _init(a: CourierApiClient) -> void:
	api = a


## 最近一次成功拉取(缓存);null = 从未拉到。
var cached_config: Variant:
	get:
		return _cached


## 拉取命中当前条件的键值全量(platform/app_version/region 可选;空 = 不参与过滤)。
## 501 能力未接 → 空结果(v0 + 空 items,接入方使用内建默认值,不进报错路径);
## 其余错误(认证/限流/网络)照常 {ok:false, error}。
func fetch(platform: String = "", app_version: String = "", region: String = "") -> Dictionary:
	var q := _param("", "platform", platform)
	q = _param(q, "appVersion", app_version)
	q = _param(q, "region", region)
	var r: Dictionary = await api.request("GET", PATH + q, null, true)
	if bool(r.get("ok", false)):
		_cached = r["data"]
		return r
	if CourierApiError.is_capability_disabled(r.get("error", {})):
		return {"ok": true, "data": {"configVersion": 0, "items": {}}}
	return r


## config.updated 事件到达后的重拉判据:事件 version 与缓存不同才需要重拉
## (相同即忽略,不重渲染);从未拉到过 → 一律重拉。
func needs_refetch(event_config_version: int) -> bool:
	return _cached == null or int(_cached["configVersion"]) != event_config_version


## 取值;键缺失返回 null(值原样,类型由接入方收窄)。
func get_value(key: String) -> Variant:
	if _cached == null:
		return null
	return (_cached as Dictionary)["items"].get(key)


# 查询串拼接(空值参数不参与过滤)。
func _param(q: String, key: String, value: String) -> String:
	if value == "":
		return q
	return q + ("&" if q != "" else "?") + key + "=" + value.uri_encode()
