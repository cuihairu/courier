# 契约错误(错误体 code/message/retryable + traceId,errors.md Frozen v1)。
# 分支判断只认 wire code(errors.md:message 人读可变,不得用于分支);
# 未登记的 code 容忍落兜底(契约 versioning.md:未知容忍)。
# GDScript 无异常:错误对象为 Dictionary,getter 只取字段(缺省安全)。

class_name CourierApiError


# 构造错误对象:{wire, code, message, http, retryable, traceId, retryAfterMs}。
# code 为登记枚举名(未登记 = "" → 调用方落兜底);traceId/retryAfterMs 缺省占位。
static func make(wire: String, message: String, http_code: int, retryable: bool) -> Dictionary:
	return {
		"wire": wire,
		"code": CourierErrorCode.name_of(wire),
		"message": message,
		"http": http_code,
		"retryable": retryable,
		"traceId": "",
		"retryAfterMs": -1,
	}


static func wire(e: Dictionary) -> String:
	return str(e.get("wire", ""))


static func code(e: Dictionary) -> String:
	return str(e.get("code", ""))


static func http_code(e: Dictionary) -> int:
	return int(e.get("http", 0))


static func retryable(e: Dictionary) -> bool:
	return bool(e.get("retryable", false))


static func message(e: Dictionary) -> String:
	return str(e.get("message", ""))


static func trace_id(e: Dictionary) -> String:
	return str(e.get("traceId", ""))


static func retry_after_ms(e: Dictionary) -> int:
	return int(e.get("retryAfterMs", -1))


# 降级信号判断:501 COMMON_CAPABILITY_DISABLED(能力未接,调用方隐藏入口)。
static func is_capability_disabled(e: Variant) -> bool:
	if typeof(e) != TYPE_DICTIONARY:
		return false
	return str((e as Dictionary).get("wire", "")) == "COMMON_CAPABILITY_DISABLED"
