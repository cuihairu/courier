// L2 Service:后段实名域客户端(契约 realname.md Frozen v1;consent 组 v1.1,F31)。
// 红线落地:姓名/证件号只经 submit 传输一次,SDK 不缓存、不落盘、不进日志;
// consent 的出生日期同款红线(只经 declare_age 传输一次,UI 展示仅年份);
// playtime-report 与 consent/s2s/* 是 S2S 端点(F30/v1.1),SDK 不封装——接入方服务端直调。
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

/// 同意状态(consent v1.1 六态:UNDECLARED/ADULT/PENDING_PARENT/GRANTED/DENIED/REVOKED;
/// 时间戳字段只在对应态下发,判定由服务端下发,客户端不得自行推断)。
struct ConsentStatusDto {
    std::string state;
    std::optional<std::string> declaredAt;
    std::optional<std::string> grantedAt;
    std::optional<std::string> revokedAt;
    std::optional<std::string> requestExpiresAt;
};

/// VPC 家长验证请求回执(PENDING_PARENT 期间幂等:未过期返回同一 requestId)。
struct ConsentRequestDto {
    std::string requestId;
    std::string requestExpiresAt;
};

inline ConsentStatusDto decode_consent_status(const json::Value& v) {
    ConsentStatusDto d;
    d.state = json::get_str(v, "state");
    std::string s;
    if (json::get_opt_str(v, "declaredAt", s)) d.declaredAt = s;
    if (json::get_opt_str(v, "grantedAt", s)) d.grantedAt = s;
    if (json::get_opt_str(v, "revokedAt", s)) d.revokedAt = s;
    if (json::get_opt_str(v, "requestExpiresAt", s)) d.requestExpiresAt = s;
    return d;
}

inline ConsentRequestDto decode_consent_request(const json::Value& v) {
    ConsentRequestDto d;
    d.requestId = json::get_str(v, "requestId");
    d.requestExpiresAt = json::get_str(v, "requestExpiresAt");
    return d;
}

/// consent 域专用降级:501 能力关闭按契约转「未启用」(nullopt),不走报错路径;
/// 其余错误(参数/限流/网络)照常随 result.error 返回,调用方分支(零新增错误码)。
template <typename T>
inline ServiceOutcome<std::optional<T>> consent_or_disabled(
    ApiClient* api, const char* method, const std::string& path, const json::Value* body,
    T (*decode)(const json::Value&)) {
    RequestOutcome out = api->request(method, path, body, true);
    ServiceOutcome<std::optional<T>> result;
    if (out.ok) {
        result.ok = true;
        result.data = decode(out.data);
    } else if (is_capability_disabled(out.error)) {
        result.ok = true;
        result.data = std::nullopt;
    } else {
        result.error = out.error;
    }
    return result;
}

/// RealName.Consent 年龄门/家长同意(契约 realname.md v1.1,F31;挂载 /v1/realname/consent/*)。
/// 默认关闭:501 → nullopt,接入方隐藏年龄门 UI,不进报错路径(同实名域 v1)。
/// OnConsentChanged 回调预留:消费 SSE realname.consent.updated(同 config.updated 帧形状),
/// 事件分发在接入方,SDK 不内置订阅;REVOKED 到达后的诊断停发联动为 diagnostics 预留位。
class ConsentService {
public:
    explicit ConsentService(ApiClient* api) : api_(api) {}

    /// 同意状态查询。能力未开启 → nullopt。
    ServiceOutcome<std::optional<ConsentStatusDto>> status() {
        return consent_or_disabled(api_, "GET", "/v1/realname/consent/status", nullptr,
                                   &decode_consent_status);
    }

    /// 年龄申报(响应即判定;防回拨:重复申报仅趋严生效)。
    /// 红线:birthDate 只在本调用传输一次,SDK 不缓存、不落盘、不进诊断采集。
    ServiceOutcome<std::optional<ConsentStatusDto>> declare_age(const std::string& birth_date) {
        json::Value body = json::Value::object();
        body.set("birthDate", json::Value::str(birth_date));
        return consent_or_disabled(api_, "POST", "/v1/realname/consent/declare-age", &body,
                                   &decode_consent_status);
    }

    /// 发起 VPC 家长同意请求(仅 PENDING_PARENT 态可发起;请求期内幂等:未过期返回同一 requestId)。
    ServiceOutcome<std::optional<ConsentRequestDto>> request_parent_consent() {
        json::Value body = json::Value::object();  // 契约空体 {}
        return consent_or_disabled(api_, "POST", "/v1/realname/consent/parent-consent/request",
                                   &body, &decode_consent_request);
    }

private:
    ApiClient* api_;
};

class RealNameService {
public:
    /// 嵌套 consent 面(契约 v1.1 投影 RealName.Consent)。
    ConsentService consent;

    explicit RealNameService(ApiClient* api) : consent(api), api_(api) {}

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
