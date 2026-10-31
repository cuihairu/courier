# F62 兑换码域客户端(契约 cdk.md Frozen v1)。POST /v1/cdk/redeem 全 Bearer。

class_name CourierCdkService

const PREFIX := "/v1/cdk"

var api: CourierApiClient


func _init(a: CourierApiClient) -> void:
	api = a


## 兑换一个码(501 → {ok:true, data:null};未知码 → CDK_NOT_FOUND;限次 → CDK_EXHAUSTED)。
func redeem(code: String) -> Dictionary:
	var r: Dictionary = await api.request("POST", PREFIX + "/redeem", {"code": code}, true)
	if not bool(r.get("ok", false)) and CourierApiError.is_capability_disabled(r.get("error", {})):
		return {"ok": true, "data": null}
	return r
