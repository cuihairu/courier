// L2 Core:API 客户端(cocos apiClient.ts / unity ApiClient 同构)——拼 scope 头、
// 发请求、解析信封、按 retryable 退避重试、AUTH_TOKEN_EXPIRED 时经 refresh_hook
// 换发后重放一次(自动 refresh,契约 auth.md「access 过期」)。
// UE 无异常口径:错误随 RequestOutcome 返回;同步调用(引擎侧异步化由 L3 适配)。
#ifndef COURIER_CORE_API_CLIENT_HPP
#define COURIER_CORE_API_CLIENT_HPP

#include <chrono>
#include <cstdint>
#include <cstdlib>
#include <functional>
#include <map>
#include <string>
#include <thread>

#include "courier_error.hpp"
#include "json.hpp"
#include "transport.hpp"
#include "types.hpp"

namespace courier {

/// scope 头(契约 scope.md:每次请求必带)。
struct ScopeHeaders {
    static constexpr const char* kGameId = "X-Courier-Game-Id";
    static constexpr const char* kEnv = "X-Courier-Env";
};

/// 退避策略(retryable 才重试;Retry-After 秒优先,否则固定间隔)。
struct RetryPolicy {
    int max_attempts = 3;
    int base_delay_ms = 300;
};

/// 请求结果:ok 时 data 有效(可为 null 值);否则 error 有效。
struct RequestOutcome {
    bool ok = false;
    json::Value data;
    CourierApiError error;
};

/// 按 HTTP 状态挑兜底 wire code(网络/网关异常无 JSON body 时的保守映射)。
struct FallbackWire {
    const char* wire;
    bool retryable;
};

inline FallbackWire fallback_wire(int status) {
    if (status == 401) return {"COMMON_UNAUTHENTICATED", false};
    if (status == 403) return {"COMMON_PERMISSION_DENIED", false};
    if (status == 404) return {"COMMON_NOT_FOUND", false};
    if (status == 429) return {"RATE_LIMITED", true};
    if (status == 501) return {"COMMON_CAPABILITY_DISABLED", false};
    if (status >= 500) return {"COMMON_UNAVAILABLE", true};
    return {"COMMON_INTERNAL", status >= 500 || status == 429};
}

/// Retry-After header(契约单位:秒)→ 毫秒;缺省/非法 = -1(无)。
inline std::int64_t retry_after_ms(const std::map<std::string, std::string>& headers) {
    const auto it = headers.find("retry-after");
    if (it == headers.end() || it->second.empty()) {
        return -1;
    }
    const double seconds = std::strtod(it->second.c_str(), nullptr);
    if (seconds < 0) {
        return -1;
    }
    return static_cast<std::int64_t>(seconds * 1000);
}

/// 信封解析结果(data 与 error 互斥;empty/非 JSON body → 状态码兜底 wire)。
struct EnvelopeResult {
    bool has_data = false;
    json::Value data;
    bool has_error = false;
    CourierApiError error;
};

inline EnvelopeResult parse_envelope(const TransportResponse& response) {
    EnvelopeResult out;
    // 网络/网关异常可能没有 JSON body(5xx 网关直返、连接中断):按状态兜底。
    bool all_ws = true;
    for (const char ch : response.body) {
        if (ch != ' ' && ch != '\t' && ch != '\n' && ch != '\r') { all_ws = false; break; }
    }
    if (all_ws) {
        const FallbackWire fb = fallback_wire(response.status_code);
        out.has_error = true;
        out.error = CourierApiError::make(fb.wire,
            "empty response body (HTTP " + std::to_string(response.status_code) + ")",
            response.status_code, fb.retryable || response.status_code >= 500);
        return out;
    }
    json::Value envelope;
    std::string parse_err;
    if (!json::parse(response.body, envelope, parse_err) || !envelope.is_object()) {
        const FallbackWire fb = fallback_wire(response.status_code);
        out.has_error = true;
        out.error = CourierApiError::make(fb.wire,
            "non-JSON response body (HTTP " + std::to_string(response.status_code) + ")",
            response.status_code, fb.retryable || response.status_code >= 500);
        return out;
    }
    if (const json::Value* err = envelope.find("error"); err && err->is_object()) {
        const std::string wire = json::get_str(*err, "code");
        // 信封 retryable 为准;登记码的冻结映射兜底缺省字段。
        bool retryable = false;
        if (const json::Value* r = err->find("retryable"); r && r->is_bool()) {
            retryable = r->as_bool();
        } else {
            ErrorCode code = ErrorCode::kCount;
            if (parse_error_code(wire.c_str(), code)) {
                retryable = error_spec(code).retryable;
            }
        }
        out.has_error = true;
        out.error = CourierApiError::make(wire, json::get_str(*err, "message"),
            response.status_code, retryable, json::get_str(envelope, "traceId"),
            retry_after_ms(response.headers));
        return out;
    }
    out.has_data = true;
    if (const json::Value* data = envelope.find("data"); data != nullptr) {
        out.data = *data;
    }
    return out;
}

struct ApiClientOptions {
    CourierConfig config;
    Transport* transport = nullptr;
    RetryPolicy retry;
    /// 每次请求现取 access token(轮换后自动为新值);空 = 匿名请求。
    std::function<std::string()> access_token_provider;
    /// access 过期(AUTH_TOKEN_EXPIRED)时尝试 refresh;返回 true 则重放一次。
    std::function<bool()> refresh_hook;
    /// 退避睡眠(测试注入;默认真实 sleep)。
    std::function<void(int)> sleep;
};

class ApiClient {
public:
    explicit ApiClient(ApiClientOptions options) : options_(std::move(options)) {
        if (!options_.sleep) {
            options_.sleep = [](int ms) {
                std::this_thread::sleep_for(std::chrono::milliseconds(ms));
            };
        }
    }

    /// 发送请求并解信封;失败随 outcome.error 返回(分支只认 wire)。
    /// body 为 nullptr = 无请求体。
    RequestOutcome request(const std::string& method, const std::string& path,
                           const json::Value* body, bool with_auth) {
        std::map<std::string, std::string> headers{
            {ScopeHeaders::kGameId, options_.config.game_id},
            {ScopeHeaders::kEnv, options_.config.env},
        };
        std::string token;
        if (with_auth && options_.access_token_provider) {
            token = options_.access_token_provider();
        }
        if (with_auth && token.empty()) {
            // 本地预检:未认证不发包(契约 COMMON_UNAUTHENTICATED)。
            RequestOutcome out;
            out.error = CourierApiError::make("COMMON_UNAUTHENTICATED", "not authenticated", 401, false);
            return out;
        }
        if (!token.empty()) {
            headers["Authorization"] = "Bearer " + token;
        }
        if (body != nullptr) {
            headers["Content-Type"] = "application/json";
        }

        TransportRequest request;
        request.method = method;
        request.url = trimmed_endpoint() + path;
        request.headers = headers;
        request.json_body = body != nullptr ? body->dump() : std::string();
        request.timeout_ms = 10000;

        bool refreshed = false;
        for (int attempt = 1;; attempt++) {
            CourierApiError error;
            if (TransportResult tr = options_.transport->send(request); !tr.ok) {
                // 网络失败/超时:依赖不可用(可重试)。
                error = CourierApiError::make("COMMON_UNAVAILABLE",
                    "transport failed: " + tr.error.message, 503, true);
            } else {
                EnvelopeResult parsed = parse_envelope(tr.response);
                if (!parsed.has_error) {
                    RequestOutcome out;
                    out.ok = true;
                    out.data = std::move(parsed.data);
                    return out;
                }
                error = std::move(parsed.error);
            }

            // access 过期:refresh 一次后重放(不计入退避重试次数)。
            if (error.wire == "AUTH_TOKEN_EXPIRED" && options_.refresh_hook && !refreshed) {
                refreshed = true;
                if (options_.refresh_hook()) {
                    token = options_.access_token_provider ? options_.access_token_provider() : std::string();
                    if (!token.empty()) {
                        request.headers["Authorization"] = "Bearer " + token;
                    }
                    attempt--;  // 重放不计入退避重试次数
                    continue;
                }
                RequestOutcome out;
                out.error = std::move(error);
                return out;
            }

            if (error.retryable && attempt < options_.retry.max_attempts) {
                options_.sleep(retry_delay_ms(error));
                continue;
            }
            RequestOutcome out;
            out.error = std::move(error);
            return out;
        }
    }

private:
    std::string trimmed_endpoint() const {
        std::string endpoint = options_.config.endpoint;
        while (!endpoint.empty() && endpoint.back() == '/') {
            endpoint.pop_back();
        }
        return endpoint;
    }

    /// 重试间隔:Retry-After(秒)优先,否则 base_delay_ms。
    int retry_delay_ms(const CourierApiError& error) const {
        return error.has_retry_after ? static_cast<int>(error.retry_after_ms)
                                     : options_.retry.base_delay_ms;
    }

    ApiClientOptions options_;
};

}  // namespace courier

#endif  // COURIER_CORE_API_CLIENT_HPP
