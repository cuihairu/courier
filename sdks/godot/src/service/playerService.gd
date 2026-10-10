# M3 玩家档案客户端(契约 player.md Frozen v1)。四端点全 Bearer;
# 501 能力未接 → data:null(调用方隐藏档案 UI,能力安静地不存在)。
# 红线同契约:SDK 不采集邮箱/手机/设备/行为字段;角色数据归各游戏。

class_name CourierPlayerService

const PREFIX := "/v1/player"

var api: CourierApiClient


func _init(a: CourierApiClient) -> void:
	api = a


## 拉取账号档案(懒建)。
func get_profile() -> Dictionary:
	return await _send_or_disabled("GET", PREFIX + "/profile", null)


## 修改档案:空串 = 不改该字段(空串不下发;UE 端同构约定);
## 响应体为准(服务端修剪后的值),调用方以返回值刷新 UI。
func update_profile(display_name: String = "", avatar_url: String = "") -> Dictionary:
	var body := {}
	if display_name != "":
		body["displayName"] = display_name
	if avatar_url != "":
		body["avatarUrl"] = avatar_url
	return await _send_or_disabled("PATCH", PREFIX + "/profile", body)


## 本游戏已绑定角色映射(boundAt 升序;按 scope 隔离,服务端语义)。
func list_characters() -> Dictionary:
	return await _send_or_disabled("GET", PREFIX + "/characters", null)


## 绑定角色:幂等(重复绑定返回既有记录);上限 50/账号/游戏。
func bind_character(player_id: String) -> Dictionary:
	return await _send_or_disabled("POST", PREFIX + "/characters", {"playerId": player_id})


# 降级语义:501 能力关闭 → {ok:true, data:null}(隐藏档案 UI,不进报错路径);其余错误照常。
func _send_or_disabled(method: String, path: String, body: Variant) -> Dictionary:
	var r: Dictionary = await api.request(method, path, body, true)
	if not bool(r.get("ok", false)) and CourierApiError.is_capability_disabled(r.get("error", {})):
		return {"ok": true, "data": null}
	return r
