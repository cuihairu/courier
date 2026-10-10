# M2 推送通道帧解析(契约 messages.md:SSE text/event-stream)。
# 纯逻辑、平台无关:流式读入的行喂进来,完整帧析出事件。
# 心跳注释(: ping)忽略;未知 event type 原样吐出——调用方按契约容忍未知。
# GDScript 无异常:feed_line 返回 Variant(事件 {type, data[, id]} 或 null)。

class_name CourierSseParser

var _data: PackedStringArray = PackedStringArray()
var _type: String = ""
var _id: String = ""
var _in_frame: bool = false


## 喂入一行(不含换行符)。结算出完整帧返回事件;否则返回 null。
func feed_line(line: Variant = null) -> Variant:
	var l := "" if line == null else str(line)
	if l.is_empty():
		# 空行 = 帧结束。
		if not _in_frame:
			return null  # 心跳/连接初始化(: connected)后无内容的空行
		var evt := {"type": _type, "data": "\n".join(_data)}
		if _id != "":
			evt["id"] = _id
		_type = ""
		_id = ""
		_data = PackedStringArray()
		_in_frame = false
		# 纯注释帧(心跳)不出事件:注释行不置 inFrame。
		if str(evt["data"]).is_empty() and str(evt["type"]).is_empty():
			return null
		return evt
	if l.begins_with(":"):
		return null  # 注释帧(: ping / : connected)——保活语义,无业务
	_in_frame = true
	var colon := l.find(":")
	var field := l if colon < 0 else l.substr(0, colon)
	var value := "" if colon < 0 else l.substr(colon + 1)
	if value.begins_with(" "):
		value = value.substr(1)  # SSE 规范:冒号后单个前导空格剥除
	match field:
		"event":
			_type = value
		"data":
			_data.append(value)
		"id":
			_id = value
		_:
			pass  # 未知字段容忍(契约 versioning.md)
	return null


## 重置(断线重连后复用)。
func reset() -> void:
	_type = ""
	_id = ""
	_data = PackedStringArray()
	_in_frame = false
