// L2 Service:支付客户端(契约 payment.md Frozen v1.1)。玩家侧四端点全 Bearer;
// POST /v1/payments/callback 为 S2S 渠道回调(HMAC 签名即认证),不经客户端 SDK 封装。
// 下单只收 skuId(服务端定价红线:客户端不下发/不计算金额)。
// v1.1 发货感知:推送为提示(payment.paid/delivered 载荷只含 orderId)+ 轮询为准。
#ifndef COURIER_SERVICE_PAYMENT_SERVICE_HPP
#define COURIER_SERVICE_PAYMENT_SERVICE_HPP

#include <cstdint>
#include <optional>
#include <string>
#include <vector>

#include "../core/api_client.hpp"
#include "../core/courier_error.hpp"
#include "../core/types.hpp"
#include "../contract/envelope.hpp"
#include "service_dto.hpp"

namespace courier {

/// 可购 SKU(服务端价格表投影;金额原样展示,客户端不做金额计算)。
struct PaymentSkuDto {
    std::string id;
    std::string productId;
    std::string form;
    std::int64_t amountCents = 0;
    std::string currency;
};

/// 订单快照。payToken 仅下单响应出现一次(消费归渠道 SDK,SDK 不解释);
/// 详情/列表不下发。发货感知 = 轮询 status 到 DELIVERED。
struct PaymentOrderDto {
    std::string id;
    std::string status;  // CREATED / PAID / DELIVERED / CLOSED
    std::string skuId;
    std::string productId;
    std::string form;
    std::int64_t amountCents = 0;
    std::string currency;
    std::optional<std::string> payToken;
    std::string createdAt;
    std::string updatedAt;
    std::optional<std::string> paidAt;
    std::optional<std::string> deliveredAt;
};

inline PaymentSkuDto decode_payment_sku(const json::Value& v) {
    PaymentSkuDto d;
    d.id = json::get_str(v, "id");
    d.productId = json::get_str(v, "productId");
    d.form = json::get_str(v, "form");
    d.amountCents = json::get_i64(v, "amountCents", 0);
    d.currency = json::get_str(v, "currency");
    return d;
}

inline PaymentOrderDto decode_payment_order(const json::Value& v) {
    PaymentOrderDto d;
    d.id = json::get_str(v, "id");
    d.status = json::get_str(v, "status");
    d.skuId = json::get_str(v, "skuId");
    d.productId = json::get_str(v, "productId");
    d.form = json::get_str(v, "form");
    d.amountCents = json::get_i64(v, "amountCents", 0);
    d.currency = json::get_str(v, "currency");
    std::string s;
    if (json::get_opt_str(v, "payToken", s)) d.payToken = s;
    d.createdAt = json::get_str(v, "createdAt");
    d.updatedAt = json::get_str(v, "updatedAt");
    if (json::get_opt_str(v, "paidAt", s)) d.paidAt = s;
    if (json::get_opt_str(v, "deliveredAt", s)) d.deliveredAt = s;
    return d;
}

class PaymentService {
public:
    explicit PaymentService(ApiClient* api) : api_(api) {}

    /// 可购 SKU 列表(价格表投影;金额原样展示)。
    ServiceOutcome<std::optional<Page<PaymentSkuDto>>> get_skus() {
        RequestOutcome out = api_->request("GET", "/v1/payments/skus", nullptr, true);
        ServiceOutcome<std::optional<Page<PaymentSkuDto>>> result;
        if (out.ok) {
            result.ok = true;
            result.data = decode_page(out.data, decode_payment_sku);
        } else if (is_capability_disabled(out.error)) {
            result.ok = true;
            result.data = std::nullopt;
        } else {
            result.error = out.error;
        }
        return result;
    }

    /// 下单:服务端按 skuId 定价 → CREATED + payToken(仅本次响应出现)。
    ServiceOutcome<std::optional<PaymentOrderDto>> create_order(const std::string& sku_id) {
        json::Value body = json::Value::object();
        body.set("skuId", json::Value::str(sku_id));
        RequestOutcome out = api_->request("POST", "/v1/payments/orders", &body, true);
        ServiceOutcome<std::optional<PaymentOrderDto>> result;
        if (out.ok) {
            result.ok = true;
            result.data = decode_payment_order(out.data);
        } else if (is_capability_disabled(out.error)) {
            result.ok = true;
            result.data = std::nullopt;
        } else {
            result.error = out.error;
        }
        return result;
    }

    /// 我的订单(updatedAt 倒序;不含 payToken)。
    ServiceOutcome<std::optional<Page<PaymentOrderDto>>> list_orders() {
        RequestOutcome out = api_->request("GET", "/v1/payments/orders", nullptr, true);
        ServiceOutcome<std::optional<Page<PaymentOrderDto>>> result;
        if (out.ok) {
            result.ok = true;
            result.data = decode_page(out.data, decode_payment_order);
        } else if (is_capability_disabled(out.error)) {
            result.ok = true;
            result.data = std::nullopt;
        } else {
            result.error = out.error;
        }
        return result;
    }

    /// 订单详情(发货感知 = 轮询 status;不属当前玩家 → 404 不泄露存在性)。
    ServiceOutcome<std::optional<PaymentOrderDto>> get_order(const std::string& order_id) {
        RequestOutcome out =
            api_->request("GET", "/v1/payments/orders/" + url_encode(order_id), nullptr, true);
        ServiceOutcome<std::optional<PaymentOrderDto>> result;
        if (out.ok) {
            result.ok = true;
            result.data = decode_payment_order(out.data);
        } else if (is_capability_disabled(out.error)) {
            result.ok = true;
            result.data = std::nullopt;
        } else {
            result.error = out.error;
        }
        return result;
    }

private:
    ApiClient* api_;
};

}  // namespace courier

#endif  // COURIER_SERVICE_PAYMENT_SERVICE_HPP
