# M3 品牌客户端(契约 branding.md Frozen v1)。SDK 透传原始 JSON——零解释、
# 零渲染、零缓存策略(缓存归宿主/接入方);兜底规则与消费全在接入方 UI。
# 501 能力未接 → data:null(UI 全默认)。匿名可:登录页就要显示品牌。

class_name CourierBrandingService

const PATH := "/v1/app/branding"

var api: CourierApiClient
var _cached: Variant = null


func _init(a: CourierApiClient) -> void:
	api = a


## 最近一次成功拉取;null = 从未拉到。
var cached_branding: Variant:
	get:
		return _cached


## 拉取品牌物料(version + 业务字段透传)。501 → data:null(未启用态,UI 全默认)。
func fetch() -> Dictionary:
	# 匿名(withAuth=false):登录页消费。
	var r: Dictionary = await api.request("GET", PATH, null, false)
	if bool(r.get("ok", false)):
		_cached = r["data"]
		return r
	if CourierApiError.is_capability_disabled(r.get("error", {})):
		return {"ok": true, "data": null}
	return r


## branding.updated 事件到达后的重拉判据:事件 version 与缓存不同才需要重拉
## (相同即忽略,不重渲染);从未拉到过 → 一律重拉。
func needs_refetch(event_version: int) -> bool:
	return _cached == null or int(_cached["version"]) != event_version
