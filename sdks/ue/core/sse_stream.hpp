// L2 Core:messages SSE 流式传输接缝(契约 messages.md;unity SseStream.cs /
// cocos sseStream.ts 同构)。接缝定在 Core(平台接线实现不引 Service),重连编排
// 在 Service 的 SseStreamClient(依赖 SseParser)。与 UE Transport 同口径:本件只
// 定接口与退避纯函数,引擎接线(FlowGraph Http / IHttpRequest 分块读)留 L3
// 集成面;服务端须 chunked 分框(自建网关 Go net/http 缺省满足)。
#ifndef COURIER_CORE_SSE_STREAM_HPP
#define COURIER_CORE_SSE_STREAM_HPP

#include <functional>
#include <map>
#include <memory>
#include <string>

#include "transport.hpp"

namespace courier {

/// 一次流连接尝试的请求。client 已按每次尝试现取 token 拼入 headers 的
/// "Authorization"(契约:新连接按当前 token 校验);source 只透传 headers。
struct SseStreamRequest {
    std::string url;
    std::map<std::string, std::string> headers;      // 基础头,不含 Authorization
    std::function<std::string()> token_provider;      // 每次连接现取;空 = 匿名
};

/// 一行读的结果(ok = 有行;eof = 流尾;均 false = 网络中断)。
struct SseReadResult {
    bool ok = false;
    bool eof = false;
    std::string line;             // ok 时:行内容(不含换行)
    TransportError error;         // 中断时:归一错误信息
};

/// 一次已建立的流连接:逐行读(流尾 = eof)。
class SseStream {
public:
    int status_code = 0;
    std::map<std::string, std::string> headers;      // 键已小写(实现侧归一)

    virtual ~SseStream() = default;
    virtual SseReadResult read_line() = 0;
    virtual void close() = 0;
};

/// 建连结果(ok = 响应头到,按 status_code 分流;非 ok = 网络失败/取消)。
struct SseConnectResult {
    bool ok = false;
    TransportError error;
    std::unique_ptr<SseStream> stream;
};

/// 平台接线面:单次连接尝试。
class SseSource {
public:
    virtual ~SseSource() = default;
    virtual SseConnectResult connect(const SseStreamRequest& request) = 0;
};

/// 通道收尾原因(on_closed 收)。
namespace sse_close_reasons {
inline constexpr char kCapabilityDisabled[] = "capability_disabled";
inline constexpr char kUnauthenticated[] = "unauthenticated";
inline constexpr char kStopped[] = "stopped";
inline constexpr char kDisconnected[] = "disconnected";
}  // namespace sse_close_reasons

/// 重连退避(纯函数):base·2^(attempt-1) 封顶 max;Retry-After(毫秒)优先。
inline int sse_backoff_delay_ms(int attempt, int retry_after_ms, int base_ms, int max_ms) {
    if (retry_after_ms > 0) {
        return retry_after_ms;  // 契约语义规则 3:Retry-After 头优先
    }
    if (attempt < 1) {
        attempt = 1;
    }
    int cap = max_ms > 1 ? max_ms : 1;
    int delay = base_ms < 1 ? 1 : base_ms;
    if (delay > cap) {
        delay = cap;
    }
    for (int i = 0; i < attempt - 1 && delay < cap; i++) {
        delay *= 2;
        if (delay > cap) {
            delay = cap;
        }
    }
    return delay;
}

}  // namespace courier

#endif  // COURIER_CORE_SSE_STREAM_HPP
