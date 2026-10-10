# 会话令牌存储抽象(unity ITokenStore / cocos TokenStore 同构):接入方按平台
# 隔离实现(Godot 平台存储;禁止明文全局缓存)。空字典 = 未认证。

class_name CourierTokenStore


func load() -> Dictionary:
	return {}


func save(_session: Dictionary) -> void:
	pass


func clear() -> void:
	pass


class MemoryTokenStore extends CourierTokenStore:
	## 内存实现(测试与不落盘场景默认;进程结束即失效)。
	var session: Dictionary = {}

	func load() -> Dictionary:
		return session

	func save(s: Dictionary) -> void:
		session = s

	func clear() -> void:
		session = {}
