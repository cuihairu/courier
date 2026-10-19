# M2 后段实名域客户端(契约 realname.md Frozen v1;consent 组 v1.1,F31)。
# 红线落地:姓名/证件号只经 submit 传输一次,SDK 不缓存、不落盘、不进日志;
# consent 的出生日期同款红线(只经 declare_age 传输一次,UI 展示仅年份);
# playtime-report 与 consent/s2s/* 是 S2S 端点(F30/v1.1),SDK 不封装——接入方服务端直调。

class_name CourierRealNameService

const PREFIX := "/v1/realname"

var api: CourierApiClient
var consent: Consent  # 嵌套面 RealName.Consent(契约 v1.1 投影)


func _init(a: CourierApiClient) -> void:
	api = a
	consent = Consent.new(a)


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


# 年龄门/家长同意嵌套面(契约 v1.1,F31;挂载 /v1/realname/consent/*,玩家 Bearer)。
# 判定经 200 载荷:同意态进 data,六态 UNDECLARED/ADULT/PENDING_PARENT/GRANTED/
# DENIED/REVOKED 由服务端下发,SDK 不推断、零新增错误码。
# 红线:birthDate 只经 declare_age 传输一次,SDK 不缓存、不落盘、不进诊断采集,
# UI 展示仅年份;consent/s2s/* 是 S2S 端点,SDK 不封装。
# SSE 推送 realname.consent.updated:OnConsentChanged 回调仅预留(事件分发在接入方,
# SDK 不内置订阅);REVOKED 到达后的诊断停发联动是 diagnostics 预留位(注释预留)。
# 默认关闭:501 COMMON_CAPABILITY_DISABLED → data:null,接入方隐藏年龄门 UI,
# 不进报错路径(同实名域 v1);其余错误照常 typed。
class Consent:
	const PATH := "/v1/realname/consent"

	var api: CourierApiClient


	func _init(a: CourierApiClient) -> void:
		api = a


	## 同意状态查询。declaredAt/grantedAt/revokedAt/requestExpiresAt 只在对应态
	## 下发,缺省即缺键。能力未开启(501)→ data:null,接入方隐藏年龄门 UI。
	func status() -> Dictionary:
		return await _send_or_disabled("GET", PATH + "/status", null)


	## 年龄申报(申报即判定,响应 data = 同意状态;防回拨:重复申报仅趋严生效)。
	## 红线:birthDate 只经本调用传输一次,SDK 不缓存、不落盘、不进诊断采集。
	func declare_age(birth_date: String) -> Dictionary:
		return await _send_or_disabled("POST", PATH + "/declare-age",
				{"birthDate": birth_date})


	## 发起 VPC 家长同意请求(空体;仅 PENDING_PARENT 态可发起,请求期内幂等,
	## 未过期返回同一 requestId)。能力未开启(501)→ data:null。
	func request_parent_consent() -> Dictionary:
		return await _send_or_disabled("POST", PATH + "/parent-consent/request", {})


	# consent 面专用降级:501 能力关闭按契约转「未启用」(data null),不走异常路径。
	func _send_or_disabled(method: String, path: String, body: Variant) -> Dictionary:
		var r: Dictionary = await api.request(method, path, body, true)
		if not bool(r.get("ok", false)) and CourierApiError.is_capability_disabled(r.get("error", {})):
			return {"ok": true, "data": null}
		return r
