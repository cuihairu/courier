// L2 messages 流式传输编排(纯逻辑,无平台引用):契约 messages.md 的
// GET /v1/messages/stream 长连客户端。与 unity SseStreamClient / cocos
// sseStreamClient.ts / godot CourierSseStream 同构。平台网络细节在 SseSource
// 实现(引擎接线留 L3 集成面);本件管重连编排:501 → capability_disabled
// (降级转纯拉取,不重试)、401 → unauthenticated,两者终态经 on_closed 收尾;
// 断线指数退避重连(1s 起、上限 60s,Retry-After 秒头优先);收过事件的连接
// 断开即重置退避(健康连≠失败连);净停靠 should_stop 谓词(引擎侧接
// 主循环/暂停信号);Authorization 每次尝试按 token_provider 现取;逐行喂
// SseParser,未知 event 容忍。
#ifndef COURIER_SERVICE_SSE_STREAM_CLIENT_HPP
#define COURIER_SERVICE_SSE_STREAM_CLIENT_HPP

#include <functional>
#include <map>
#include <string>

#include "../core/sse_stream.hpp"
#include "sse_parser.hpp"

namespace courier {

/// 头键小写归一(契约 transport 键规范;与 unity Lowercased / cocos lowerKeys 同构)。
inline std::map<std::string, std::string> lowercased(const std::map<std::string, std::string>& headers) {
    std::map<std::string, std::string> out;
    for (const auto& kv : headers) {
        std::string key = kv.first;
        for (char& c : key) {
            if (c >= 'A' && c <= 'Z') {
                c += 'a' - 'A';
            }
        }
        out[key] = kv.second;
    }
    return out;
}

/// Retry-After(契约单位:秒)→ 毫秒;缺省/非法 = 0。头键须已小写归一。
inline int parse_retry_after_ms(const std::map<std::string, std::string>& headers) {
    const auto it = headers.find("retry-after");
    if (it == headers.end()) {
        return 0;
    }
    int seconds = 0;
    try {
        seconds = std::stoi(it->second);
    } catch (...) {
        return 0;
    }
    return seconds > 0 ? seconds * 1000 : 0;
}

/// 一次连接尝试的结局。
struct SseAttemptOutcome {
    std::string reason;           // sse_close_reasons::*
    bool healthy = false;         // 收过事件(健康连:重置退避)
    int retry_after_ms = 0;
};

struct SseStreamClientOptions {
    SseSource* source = nullptr;  // 非拥有;生命周期由接入方持有
    int backoff_base_ms = 1000;
    int backoff_max_ms = 60000;
    std::function<void(int)> sleep;  // 退避睡眠(测试注入;默认真实 sleep)
};

class SseStreamClient {
public:
    using OnEvent = std::function<void(const SseEvent&)>;
    using OnClosed = std::function<void(const std::string&)>;

    /// 上一次重连前算得的退避毫秒(诊断/测试观察面;含 Retry-After 优先值)。
    int last_delay_ms = 0;

    explicit SseStreamClient(const SseStreamClientOptions& options) : options_(options) {
        if (!options_.sleep) {
            options_.sleep = [](int) {};  // 缺省占位:调用方一般需注入(见 run 注释)
        }
    }

    /// 长连循环:直到 should_stop 返回真或终态(501/401)。on_event 收解析帧,
    /// on_closed 收收尾原因。同步阻塞,退避等待经 sleep(测试注入;引擎侧应由
    /// 接入方注入分帧安全的让出,避免在 Tick 内真实阻塞)。
    void run(const SseStreamRequest& request, const OnEvent& on_event, const OnClosed& on_closed,
             const std::function<bool()>& should_stop = {}) {
        int attempt = 0;
        while (!should_stop || !should_stop()) {
            const SseAttemptOutcome outcome = attempt_once(request, on_event, should_stop);
            if (outcome.reason == sse_close_reasons::kStopped) {
                return;
            }
            if (outcome.reason == sse_close_reasons::kCapabilityDisabled ||
                outcome.reason == sse_close_reasons::kUnauthenticated) {
                if (on_closed) {
                    on_closed(outcome.reason);
                }
                return;
            }
            attempt = outcome.healthy ? 0 : attempt + 1;
            last_delay_ms = sse_backoff_delay_ms(attempt, outcome.retry_after_ms,
                options_.backoff_base_ms, options_.backoff_max_ms);
            if (last_delay_ms > 0) {
                options_.sleep(last_delay_ms);
                if (should_stop && should_stop()) {
                    return;  // 退避中被取消:净停
                }
            }
        }
    }

private:
    SseStreamClientOptions options_;

    SseAttemptOutcome attempt_once(const SseStreamRequest& request, const OnEvent& on_event,
        const std::function<bool()>& should_stop) {
        const SseConnectResult result = connect_safe(request);
        if (!result.ok) {
            return should_stop && should_stop()
                ? SseAttemptOutcome{sse_close_reasons::kStopped}
                : SseAttemptOutcome{sse_close_reasons::kDisconnected};
        }
        const int retry_after_ms = parse_retry_after_ms(lowercased(result.stream->headers));
        if (result.stream->status_code == 501) {
            result.stream->close();
            return SseAttemptOutcome{sse_close_reasons::kCapabilityDisabled};
        }
        if (result.stream->status_code == 401) {
            result.stream->close();
            return SseAttemptOutcome{sse_close_reasons::kUnauthenticated};
        }
        bool healthy = false;
        if (result.stream->status_code == 200) {
            SseParser parser;
            for (;;) {
                if (should_stop && should_stop()) {
                    result.stream->close();
                    return SseAttemptOutcome{sse_close_reasons::kStopped};
                }
                const SseReadResult read = result.stream->read_line();
                if (read.eof) {
                    break;  // 流尾 = 断流,走重连
                }
                if (!read.ok) {
                    break;  // 网络中断:走重连
                }
                const auto event = parser.feed_line(read.line);
                if (event && on_event) {
                    healthy = true;
                    on_event(*event);
                }
            }
        }
        result.stream->close();
        return SseAttemptOutcome{sse_close_reasons::kDisconnected, healthy, retry_after_ms};
    }

    SseConnectResult connect_safe(const SseStreamRequest& request) {
        SseStreamRequest attempt_request = request;
        const std::string token =
            request.token_provider ? request.token_provider() : std::string();
        if (!token.empty()) {
            attempt_request.headers["Authorization"] = "Bearer " + token;  // 每连现取
        }
        return options_.source ? options_.source->connect(attempt_request)
                               : SseConnectResult{};
    }
};

}  // namespace courier

#endif  // COURIER_SERVICE_SSE_STREAM_CLIENT_HPP

