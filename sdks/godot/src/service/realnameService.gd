# M2 后段实名域客户端(契约 realname.md Frozen v1)。
# 红线落地:姓名/证件号只经 submit 传输一次,SDK 不缓存、不落盘、不进日志;
# playtime-report 是 S2S 端点(F30),SDK 不封装——接入方服务端直调。

class_name CourierRealNameService

const PREFIX := "/v1/realname"

var api: CourierApiClient


func _init(a: CourierApiClient) -> void:
	api = a


## 提交核验(F27)。REJECTED/PENDING_REVIEW 也是有效提交结果,不是异常。
## 能力未开启(501)→ data:null,调用方隐藏实名 UI,不进报错路径。
func submit(player_name: String, id_number: String) -> Dictionary:
	return await _send_or_disabled("POST", PREFIX + "/verify",
			{"name": player_name, "idNumber": id_number})


## 状态查询(F27)。能力未开启 → data:null。
func status() -> Dictionary:
	return await _send_or_disabled("GET", PREFIX + "/status", null)


## 可玩时段查询(F28)。能力未开启 → data:null(接入方自行决定是否限制)。
func curfew() -> Dictionary:
	return await _send_or_disabled("GET", PREFIX + "/curfew", null)


## 充值额度校验(F29):支付下单前的前置校验;能力未开启 → data:null(不拦支付,由接入方决定)。
func charge_check(amount_cents: int) -> Dictionary:
	return await _send_or_disabled("POST", PREFIX + "/charge-check", {"amountCents": amount_cents})


# 实名域专用降级:501 能力关闭按契约转「未启用」(data null),不走异常路径。
func _send_or_disabled(method: String, path: String, body: Variant) -> Dictionary:
	var r: Dictionary = await api.request(method, path, body, true)
	if not bool(r.get("ok", false)) and CourierApiError.is_capability_disabled(r.get("error", {})):
		return {"ok": true, "data": null}
	return r
