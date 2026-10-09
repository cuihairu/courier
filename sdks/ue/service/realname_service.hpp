// L2 Service:后段实名域客户端(契约 realname.md Frozen v1)。
// 红线落地:姓名/证件号只经 submit 传输一次,SDK 不缓存、不落盘、不进日志;
// playtime-report 是 S2S 端点(F30),SDK 不封装——接入方服务端直调。
#ifndef COURIER_SERVICE_REALNAME_SERVICE_HPP
#define COURIER_SERVICE_REALNAME_SERVICE_HPP

#include <cstdint>
#include <optional>
#include <string>

#include "../core/api_client.hpp"
#include "../core/courier_error.hpp"
#include "service_dto.hpp"

namespace courier {

/// 实名状态(UNVERIFIED/PENDING_REVIEW/VERIFIED/REJECTED;isMinor 仅 VERIFIED 后下发)。
struct RealNameStatusDto {
    std::string state;
    std::optional<bool> isMinor;
    std::optional<std::string> verifiedAt;
};

/// 可玩时段查询(F28):playable=false 时 nextWindowAt 给下一窗口。
struct RealNameCurfewDto {
    bool playable = false;
    std::optional<std::string> nextWindowAt;
};

/// 充值额度校验(F29):限额字段 0 = 未下发。
struct RealNameChargeDto {
    bool allowed = false;
    std::optional<std::int64_t> singleLimitCents;
    std::optional<std::int64_t> monthlyLimitCents;
    std::optional<std::int64_t> monthlyUsedCents;
};

inline RealNameStatusDto decode_realname_status(const json::Value& v) {
    RealNameStatusDto d;
    d.state = json::get_str(v, "state");
    if (const json::Value* m = v.find("isMinor"); m && m->is_bool()) d.isMinor = m->as_bool();
    std::string s;
    if (json::get_opt_str(v, "verifiedAt", s)) d.verifiedAt = s;
    return d;
}

inline RealNameCurfewDto decode_realname_curfew(const json::Value& v) {
    RealNameCurfewDto d;
    d.playable = json::get_bool(v, "playable", false);
    std::string s;
    if (json::get_opt_str(v, "nextWindowAt", s)) d.nextWindowAt = s;
    return d;
}

inline RealNameChargeDto decode_realname_charge(const json::Value& v) {
    RealNameChargeDto d;
    d.allowed = json::get_bool(v, "allowed", false);
    if (const json::Value* f = v.find("singleLimitCents"); f && f->is_number()) {
        d.singleLimitCents = f->as_int();
    }
    if (const json::Value* f = v.find("monthlyLimitCents"); f && f->is_number()) {
        d.monthlyLimitCents = f->as_int();
    }
    if (const json::Value* f = v.find("monthlyUsedCents"); f && f->is_number()) {
        d.monthlyUsedCents = f->as_int();
    }
    return d;
}

class RealNameService {
public:
    explicit RealNameService(ApiClient* api) : api_(api) {}

    /// 提交核验(F27)。REJECTED/PENDING_REVIEW 也是有效提交结果,不是异常。
    /// 能力未开启(501)→ null,调用方隐藏实名 UI,不进报错路径。
    ServiceOutcome<std::optional<RealNameStatusDto>> submit(const std::string& name,
                                                            const std::string& id_number) {
        json::Value body = json::Value::object();
        body.set("name", json::Value::str(name));
        body.set("idNumber", json::Value::str(id_number));
        RequestOutcome out = api_->request("POST", "/v1/realname/verify", &body, true);
        ServiceOutcome<std::optional<RealNameStatusDto>> result;
        if (out.ok) {
            result.ok = true;
            result.data = decode_realname_status(out.data);
        } else if (is_capability_disabled(out.error)) {
            result.ok = true;
            result.data = std::nullopt;
        } else {
            result.error = out.error;
        }
        return result;
    }

    /// 状态查询(F27)。能力未开启 → null。
    ServiceOutcome<std::optional<RealNameStatusDto>> status() {
        RequestOutcome out = api_->request("GET", "/v1/realname/status", nullptr, true);
        ServiceOutcome<std::optional<RealNameStatusDto>> result;
        if (out.ok) {
            result.ok = true;
            result.data = decode_realname_status(out.data);
        } else if (is_capability_disabled(out.error)) {
            result.ok = true;
            result.data = std::nullopt;
        } else {
            result.error = out.error;
        }
        return result;
    }

    /// 可玩时段查询(F28)。能力未开启 → null(接入方自行决定是否限制)。
    ServiceOutcome<std::optional<RealNameCurfewDto>> curfew() {
        RequestOutcome out = api_->request("GET", "/v1/realname/curfew", nullptr, true);
        ServiceOutcome<std::optional<RealNameCurfewDto>> result;
        if (out.ok) {
            result.ok = true;
            result.data = decode_realname_curfew(out.data);
        } else if (is_capability_disabled(out.error)) {
            result.ok = true;
            result.data = std::nullopt;
        } else {
            result.error = out.error;
        }
        return result;
    }

    /// 充值额度校验(F29):支付下单前的前置校验;能力未开启 → null(不拦支付,由接入方决定)。
    ServiceOutcome<std::optional<RealNameChargeDto>> charge_check(std::int64_t amount_cents) {
        json::Value body = json::Value::object();
        body.set("amountCents", json::Value::integer(amount_cents));
        RequestOutcome out = api_->request("POST", "/v1/realname/charge-check", &body, true);
        ServiceOutcome<std::optional<RealNameChargeDto>> result;
        if (out.ok) {
            result.ok = true;
            result.data = decode_realname_charge(out.data);
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

#endif  // COURIER_SERVICE_REALNAME_SERVICE_HPP
