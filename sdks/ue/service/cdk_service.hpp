// L2 Service:兑换码客户端(契约 cdk.md Frozen v1)。POST /v1/cdk/redeem 全 Bearer。
#ifndef COURIER_SERVICE_CDK_SERVICE_HPP
#define COURIER_SERVICE_CDK_SERVICE_HPP

#include <optional>
#include <string>

#include "../core/api_client.hpp"
#include "../core/courier_error.hpp"
#include "../core/types.hpp"
#include "../contract/envelope.hpp"
#include "service_dto.hpp"

namespace courier {

/// 兑换结果(同账号+码重复 → repeated=true,不二次发放)。
struct RedeemResultDto {
    std::string code;
    std::string grant;
    bool repeated = false;
};

inline RedeemResultDto decode_redeem_result(const json::Value& v) {
    RedeemResultDto d;
    d.code = json::get_str(v, "code");
    d.grant = json::get_str(v, "grant");
    d.repeated = json::get_bool(v, "repeated", false);
    return d;
}

class CdkService {
public:
    explicit CdkService(ApiClient* api) : api_(api) {}

    /// 兑换一个码(501 → nullopt;未知码 → CDK_NOT_FOUND;限次 → CDK_EXHAUSTED)。
    ServiceOutcome<std::optional<RedeemResultDto>> redeem(const std::string& code) {
        json::Value body = json::Value::object();
        body.set("code", json::Value::str(code));
        RequestOutcome out = api_->request("POST", "/v1/cdk/redeem", &body, true);
        ServiceOutcome<std::optional<RedeemResultDto>> result;
        if (out.ok) {
            result.ok = true; result.data = decode_redeem_result(out.data);
        } else if (is_capability_disabled(out.error)) {
            result.ok = true; result.data = std::nullopt;
        } else { result.error = out.error; }
        return result;
    }

private:
    ApiClient* api_;
};

}  // namespace courier

#endif  // COURIER_SERVICE_CDK_SERVICE_HPP
