# M5 支付客户端(契约 payment.md Frozen v1.1)。玩家侧四端点全 Bearer;
# POST /v1/payments/callback 为 S2S 渠道回调(HMAC 签名即认证),不经客户端 SDK 封装。
# 下单只收 sku_id(服务端定价红线:客户端不下发/不计算金额)。
# v1.1 发货感知:推送为提示(payment.paid/delivered 载荷只含 orderId)+ 轮询为准。

class_name CourierPaymentService

const PREFIX := "/v1/payments"

var api: CourierApiClient


func _init(a: CourierApiClient) -> void:
	api = a


## 可购 SKU 列表(价格表投影;金额原样展示)。
func get_skus() -> Dictionary:
	return await _send_or_disabled("GET", PREFIX + "/skus", null)


## 下单:服务端按 sku_id 定价 → CREATED + payToken(仅本次响应出现)。
func create_order(sku_id: String) -> Dictionary:
	return await _send_or_disabled("POST", PREFIX + "/orders", {"skuId": sku_id})


## 我的订单(updatedAt 倒序;不含 payToken)。
func list_orders() -> Dictionary:
	return await _send_or_disabled("GET", PREFIX + "/orders", null)


## 订单详情(发货感知 = 轮询 status;不属当前玩家 → 404 不泄露存在性)。
func get_order(order_id: String) -> Dictionary:
	return await _send_or_disabled("GET", PREFIX + "/orders/" + order_id.uri_encode(), null)


# 降级语义:501 能力关闭 → {ok:true, data:null}(隐藏商城/充值 UI,不进报错路径);其余错误照常。
func _send_or_disabled(method: String, path: String, body: Variant) -> Dictionary:
	var r: Dictionary = await api.request(method, path, body, true)
	if not bool(r.get("ok", false)) and CourierApiError.is_capability_disabled(r.get("error", {})):
		return {"ok": true, "data": null}
	return r
