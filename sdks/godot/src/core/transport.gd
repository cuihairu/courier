# L2 Core 传输抽象(unity ITransport / ue Transport / cocos Transport 同构):
# 平台各自实现,SDK 其余部分零平台引用。send 为协程(async),调用方用 await。
# 返回 TransportResult Dictionary:
#   ok=true  → { ok, status_code, headers, body }  headers 键一律小写归一
#   ok=false → { ok, message }                      网络失败/超时
# 网络失败统一走 ok=false,由 ApiClient 映射 COMMON_UNAVAILABLE 重试。

class_name CourierTransport


func send(_request: Dictionary) -> Dictionary:
	push_error("CourierTransport.send 未实现(须继承并提供平台实现)")
	return {"ok": false, "message": "not implemented"}
