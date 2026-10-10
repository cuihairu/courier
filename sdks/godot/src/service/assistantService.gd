# M4 小助手客户端(契约 assistant.md Frozen v1)。POST /v1/assistant/query(全 Bearer)。
# 未命中不是错误:返回 matched=false 的结果对象,UI 据此引导转人工;
# 501 能力未接 → data:null(隐藏小助手入口,能力安静地不存在);无本地缓存(即席查询)。

class_name CourierAssistantService

const PREFIX := "/v1/assistant"

var api: CourierApiClient


func _init(a: CourierApiClient) -> void:
	api = a


## 提问(1-500 字符;空/超长 = 使用方错误,服务端 400)。
## 未命中 → matched=false + suggestTransfer=true;UI 据此调 Support 域提单转人工。
## 参数/限流/网络照常 {ok:false, error} 给调用方分支。
func query(text: String) -> Dictionary:
	var r: Dictionary = await api.request("POST", PREFIX + "/query", {"text": text}, true)
	if not bool(r.get("ok", false)) and CourierApiError.is_capability_disabled(r.get("error", {})):
		return {"ok": true, "data": null}
	return r
