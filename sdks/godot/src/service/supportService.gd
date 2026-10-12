# M2 客服域客户端(契约 support.md:玩家侧工单/消息/FAQ)。
# 坐席回复的实时感知走 messages 通道(见 CourierSseParser);通道关闭 → 拉取详情兜底。

class_name CourierSupportService

const PREFIX := "/v1/support/"

var api: CourierApiClient


func _init(a: CourierApiClient) -> void:
	api = a


## 提单(category 可空 = OTHER);限流超限 typed RATE_LIMITED。
## request 形状 {title, body[, category][, context]};category 缺省键不下发;
## context 为 v1.1 开集 map(键 1–32 位 [a-zA-Z0-9_.],≤10 键,值 string/number/boolean
## ≤256 字符,整体 ≤1KiB),服务端零解释透传回 ticket.context。
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


## 申请附件直传(v1.1):登记即时,返回短时效预签名 URL(PUT 直传存储,不经网关)。
## request 形状 {fileName, contentType, sizeBytes[, kind]};kind 缺省键不下发。
## 超限 typed SUPPORT_ATTACHMENT_TOO_LARGE(413)/ SUPPORT_ATTACHMENT_LIMIT(409);
## 存储未配置 → typed COMMON_CAPABILITY_DISABLED(501)。
## 响应 data 形状 {attachmentId, uploadUrl, method, headers, expiresAt}。
func request_attachment_upload(ticket_id: String, request: Dictionary) -> Dictionary:
	return await api.request("POST",
			PREFIX + "tickets/" + ticket_id.uri_encode() + "/attachments", request, true)


## FAQ 检索(关键词命中为空是常态,不是错误);keyword 空串不带参数。
func faq(limit: int, keyword: String = "") -> Dictionary:
	var path := PREFIX + "faq?limit=" + str(limit)
	if keyword != "":
		path += "&keyword=" + keyword.uri_encode()
	return await api.request("GET", path, null, true)
