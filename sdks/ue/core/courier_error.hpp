// L2 Core:契约错误(cocos courierError.ts / unity CourierApiError 同构)。
// 分支判断只认 wire code(errors.md:message 人读可变,不得用于分支);
// 未登记 code 容忍落兜底(契约 versioning.md:未知容忍)。
// UE 无异常口径:错误随 RequestOutcome 返回,不抛。
#ifndef COURIER_CORE_COURIER_ERROR_HPP
#define COURIER_CORE_COURIER_ERROR_HPP

#include <cstdint>
#include <string>

#include "../contract/errors.hpp"

namespace courier {

struct CourierApiError {
    /// wire code 原文(未登记 code 原样保留——容忍未知由调用方兜底)。
    std::string wire;
    /// 登记过的码映射为枚举;未登记 = false(枚举值无意义)。
    bool registered = false;
    ErrorCode code = ErrorCode::kCount;
    int http = 0;
    bool retryable = false;
    std::string trace_id;
    /// Retry-After header(毫秒;契约 errors.md:配合退避重试)。
    bool has_retry_after = false;
    std::int64_t retry_after_ms = 0;
    /// 人读信息(不得用于分支)。
    std::string message;

    static CourierApiError make(std::string wire, std::string message, int http, bool retryable,
                                std::string trace_id = std::string(),
                                std::int64_t retry_after_ms = -1) {
        CourierApiError e;
        e.wire = std::move(wire);
        e.message = std::move(message);
        e.http = http;
        e.retryable = retryable;
        e.trace_id = std::move(trace_id);
        e.registered = parse_error_code(e.wire.c_str(), e.code);
        if (retry_after_ms >= 0) {
            e.has_retry_after = true;
            e.retry_after_ms = retry_after_ms;
        }
        return e;
    }
};

/// 降级信号判断:501 COMMON_CAPABILITY_DISABLED(能力未接,调用方隐藏入口)。
inline bool is_capability_disabled(const CourierApiError& e) {
    return e.wire == "COMMON_CAPABILITY_DISABLED";
}

}  // namespace courier

#endif  // COURIER_CORE_COURIER_ERROR_HPP
