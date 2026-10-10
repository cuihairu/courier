# L3 平台令牌存储(Godot FileAccess):L2 TokenStore 抽象的平台实现,落 user://
# (引擎按平台映射应用专属存储目录)。JSON 序列化整会话;clear 即删文件。

class_name CourierFileTokenStore
extends CourierTokenStore

var path: String = "user://courier/session.json"


func load() -> Dictionary:
	var f := FileAccess.open(path, FileAccess.READ)
	if f == null:
		return {}
	var parsed: Variant = JSON.parse_string(f.get_as_text())
	if typeof(parsed) != TYPE_DICTIONARY:
		return {}
	return parsed


func save(session: Dictionary) -> void:
	DirAccess.make_dir_recursive_absolute(path.get_base_dir())
	var f := FileAccess.open(path, FileAccess.WRITE)
	if f == null:
		push_error("courier: token 存储写入失败:" + path)
		return
	f.store_string(JSON.stringify(session))


func clear() -> void:
	if FileAccess.file_exists(path):
		DirAccess.remove_absolute(path)
