# M2 公告域客户端(契约 announcement.md:玩家侧只读投影)。
# 列表/详情经 ApiClient(scope 头、Bearer、信封、重试全部归 L2)。
# GDScript 无异常:返回 {ok:true, data} 或 {ok:false, error}(typed,协程须 await)。

class_name CourierAnnouncementService

const PREFIX := "/v1/announcements"

var api: CourierApiClient


func _init(a: CourierApiClient) -> void:
	api = a


## 可见公告列表(分页;limit 默认 20 最大 100,游标回传 nextCursor)。
func list(limit: int, cursor: String = "") -> Dictionary:
	var path := PREFIX + "?limit=" + str(limit)
	if cursor != "":
		path += "&cursor=" + cursor.uri_encode()
	return await api.request("GET", path, null, true)


## 公告详情;不可见/不存在 → typed ANNOUNCEMENT_NOT_FOUND(404,不重试)。
func get_announcement(announcement_id: String) -> Dictionary:
	return await api.request("GET", PREFIX + "/" + announcement_id.uri_encode(), null, true)
