// L2 Core 传输抽象(cocos transport.ts / unity ITransport 同构):平台各自实现,
// SDK 其余部分零平台引用。UE 侧由 L3 适配器以引擎 Http 模块实现;测试注入脚本化
// fake。传输失败(网络/超时)归一为 TransportError——ApiClient 映射
// COMMON_UNAVAILABLE 走既有重试口径。
#ifndef COURIER_CORE_TRANSPORT_HPP
#define COURIER_CORE_TRANSPORT_HPP

#include <map>
#include <string>

namespace courier {

struct TransportRequest {
    std::string method;
    std::string url;
    std::map<std::string, std::string> headers;
    std::string json_body;  // 空 = 无请求体
    int timeout_ms = 10000;
};

struct TransportResponse {
    int status_code = 0;
    std::map<std::string, std::string> headers;  // 键已小写(实现侧归一)
    std::string body;
};

struct TransportError {
    std::string message;
};

struct TransportResult {
    bool ok = false;
    TransportResponse response;
    TransportError error;
};

struct Transport {
    virtual ~Transport() = default;
    virtual TransportResult send(const TransportRequest& request) = 0;
};

}  // namespace courier

#endif  // COURIER_CORE_TRANSPORT_HPP
