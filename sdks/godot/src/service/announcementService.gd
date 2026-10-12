# M2 公告域客户端(契约 announcement.md:玩家侧只读投影)。
# 列表/详情经 ApiClient(scope 头、Bearer、信封、重试全部归 L2)。
# GDScript 无异常:返回 {ok:true, data} 或 {ok:false, error}(typed,协程须 await)。

class_name CourierAnnouncementService

const PREFIX := "/v1/announcements"

var api: CourierApiClient
var default_language: String


func _init(a: CourierApiClient, language: String = "") -> void:
	api = a
	default_language = language


## 可见公告列表(分页;limit 默认 20 最大 100,游标回传 nextCursor)。
## query 为 v1.1 定向参数:{platform, appVersion, region, language},缺省键不过滤;
## 详情被过滤出局 → typed ANNOUNCEMENT_NOT_FOUND。
func list(limit: int, cursor: String = "", query: Dictionary = {}) -> Dictionary:
	var path := PREFIX + "?limit=" + str(limit)
	if cursor != "":
		path += "&cursor=" + cursor.uri_encode()
	for key in ["platform", "appVersion", "region", "language"]:
		var value: String = str(query.get(key, ""))
		if value != "":
			path += "&%s=%s" % [key, value.uri_encode()]
	# F76 多语内容:配置 locale 自动附带 language(仅列表);显式 query.language 覆写,
	# 配置缺省不附带(与 v1 行为一致)。行内 language 元数据不参与匹配。
	if str(query.get("language", "")) == "" and default_language != "":
		path += "&language=" + default_language.uri_encode()
	return await api.request("GET", path, null, true)


## 公告详情;不可见/不存在 → typed ANNOUNCEMENT_NOT_FOUND(404,不重试)。
func get_announcement(announcement_id: String) -> Dictionary:
	return await api.request("GET", PREFIX + "/" + announcement_id.uri_encode(), null, true)
