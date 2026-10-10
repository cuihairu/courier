# M2 客服域客户端(契约 support.md:玩家侧工单/消息/FAQ)。
# 坐席回复的实时感知走 messages 通道(见 CourierSseParser);通道关闭 → 拉取详情兜底。

class_name CourierSupportService

const PREFIX := "/v1/support/"

var api: CourierApiClient


func _init(a: CourierApiClient) -> void:
	api = a


## 提单(category 可空 = OTHER);限流超限 typed RATE_LIMITED。
## request 形状 {title, body[, category]};category 缺省键不下发。
func create_ticket(request: Dictionary) -> Dictionary:
	return await api.request("POST", PREFIX + "tickets", request, true)


## 我的工单列表(updatedAt 倒序)。
func list_tickets(limit: int, cursor: String = "") -> Dictionary:
	var path := PREFIX + "tickets?limit=" + str(limit)
	if cursor != "":
		path += "&cursor=" + cursor.uri_encode()
	return await api.request("GET", path, null, true)


## 工单详情(ticket + messages 升序);非本人 → typed SUPPORT_TICKET_NOT_FOUND。
func get_ticket(ticket_id: String) -> Dictionary:
	return await api.request("GET", PREFIX + "tickets/" + ticket_id.uri_encode(), null, true)


## 追加玩家消息;工单 CLOSED → typed SUPPORT_TICKET_CLOSED(409,不重试)。
func append_message(ticket_id: String, body: String) -> Dictionary:
	return await api.request("POST",
			PREFIX + "tickets/" + ticket_id.uri_encode() + "/messages", {"body": body}, true)


## FAQ 检索(关键词命中为空是常态,不是错误);keyword 空串不带参数。
func faq(limit: int, keyword: String = "") -> Dictionary:
	var path := PREFIX + "faq?limit=" + str(limit)
	if keyword != "":
		path += "&keyword=" + keyword.uri_encode()
	return await api.request("GET", path, null, true)
