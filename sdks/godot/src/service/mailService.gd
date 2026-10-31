# F62 邮件域客户端(契约 mail.md Frozen v1)。玩家侧三端点全 Bearer;
# 邮件只是附件载体;mail.updated 推送为提示,列表为准。

class_name CourierMailService

const PREFIX := "/v1/mail"

var api: CourierApiClient


func _init(a: CourierApiClient) -> void:
	api = a


## 我的邮件列表(个人+全量,最新在前;501 → {ok:true, data:null})。
func list(limit: int = 20, cursor: String = "") -> Dictionary:
	var path := PREFIX + "?limit=" + str(limit)
	if cursor != "":
		path += "&cursor=" + cursor.uri_encode()
	return await _send_or_disabled("GET", path, null)


## 邮件详情;不存在/非本人 → typed MAIL_NOT_FOUND。
func get(mail_id: String) -> Dictionary:
	return await _send_or_disabled("GET", PREFIX + "/" + mail_id.uri_encode(), null)


## 领取附件(幂等:重复领取同结果,不二次发放)。
func claim(mail_id: String) -> Dictionary:
	return await _send_or_disabled("POST", PREFIX + "/" + mail_id.uri_encode() + "/claim", {})


func _send_or_disabled(method: String, path: String, body: Variant) -> Dictionary:
	var r: Dictionary = await api.request(method, path, body, true)
	if not bool(r.get("ok", false)) and CourierApiError.is_capability_disabled(r.get("error", {})):
		return {"ok": true, "data": null}
	return r
