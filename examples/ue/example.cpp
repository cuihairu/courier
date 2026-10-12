// Courier UE SDK 最小可跑示例:接入 → 登录 → 收公告 → 发工单。
//
// SDK 纯逻辑层(contract/core/service)零引擎依赖,ISO C++17 独立编译,故本示例
// 在 g++ 下即可实测全链路;进引擎时只换一件:Transport 换引擎 Http 模块适配,
// 形态不变(收 TransportRequest,回 TransportResult,网络异常归一 TransportError)。
//
// 运行:
//   1) 起本地网关:cd gateway && go build -o /tmp/courier-gateway ./cmd/gateway
//      && /tmp/courier-gateway(默认 :8080,COURIER_GATEWAY_ADDR 可覆盖)
//   2) g++ -std=c++17 -Wall -Wextra examples/ue/example.cpp
//        -Isdks/ue/service -Isdks/ue/core -Isdks/ue/contract -o /tmp/ue_example
//      /tmp/ue_example <endpoint>   # 例:/tmp/ue_example http://127.0.0.1:18099
//
// 接入点只有两个:CourierConfig(gameId/env = scope 头)+ Transport 适配。
// 注:UE 端暂无 CourierServices 门面,域服务直接以 ApiClient* 构造(同 TS 构造法)。
#include <arpa/inet.h>
#include <netdb.h>
#include <sys/socket.h>

#include <cctype>
#include <cstdlib>
#include <cstring>
#include <iostream>
#include <map>
#include <string>
#include <unistd.h>

#include "courier_client.hpp"
#include "announcement_service.hpp"
#include "support_service.hpp"

namespace {

using namespace courier;

// 示例用 POSIX socket 传输:HTTP/1.1 + Connection: close,支持 chunked 响应。
// 引擎内换成 UE Http 模块(异步回调桥到游戏线程),本类即弃。
class SocketTransport final : public Transport {
public:
    TransportResult send(const TransportRequest& request) override {
        TransportResult result;
        std::string host, port, path;
        if (!split_url(request.url, host, port, path)) {
            result.error.message = "bad url: " + request.url;
            return result;
        }
        int fd = ::socket(AF_INET, SOCK_STREAM, 0);
        if (fd < 0) {
            result.error.message = "socket failed";
            return result;
        }
        timeval tv{};
        tv.tv_sec = request.timeout_ms / 1000;
        tv.tv_usec = (request.timeout_ms % 1000) * 1000;
        ::setsockopt(fd, SOL_SOCKET, SO_RCVTIMEO, &tv, sizeof(tv));
        ::setsockopt(fd, SOL_SOCKET, SO_SNDTIMEO, &tv, sizeof(tv));

        addrinfo hints{};
        hints.ai_family = AF_INET;
        hints.ai_socktype = SOCK_STREAM;
        addrinfo* list = nullptr;
        if (::getaddrinfo(host.c_str(), port.c_str(), &hints, &list) != 0 || list == nullptr) {
            ::close(fd);
            result.error.message = "resolve failed: " + host;
            return result;
        }
        bool connected = ::connect(fd, list->ai_addr, list->ai_addrlen) == 0;
        ::freeaddrinfo(list);
        if (!connected) {
            ::close(fd);
            result.error.message = "connect failed: " + host + ":" + port;
            return result;
        }

        // 请求行 + 头(SDK 已带 scope/Authorization/UA)+ 可选 JSON 体。
        std::string wire = request.method + " " + path + " HTTP/1.1\r\nHost: " + host + "\r\n";
        for (const auto& kv : request.headers) {
            wire += kv.first + ": " + kv.second + "\r\n";
        }
        if (!request.json_body.empty()) {
            wire += "Content-Type: application/json\r\n";
            wire += "Content-Length: " + std::to_string(request.json_body.size()) + "\r\n";
        }
        wire += "Connection: close\r\n\r\n";
        if (!request.json_body.empty()) {
            wire += request.json_body;
        }
        if (!send_all(fd, wire)) {
            ::close(fd);
            result.error.message = "write failed";
            return result;
        }

        std::string raw;
        char buf[4096];
        ssize_t n;
        while ((n = ::recv(fd, buf, sizeof(buf), 0)) > 0) {
            raw.append(buf, static_cast<size_t>(n));
        }
        ::close(fd);
        if (!parse_response(raw, result)) {
            result.ok = false;
            if (result.error.message.empty()) result.error.message = "bad response";
        }
        return result;
    }

private:
    static bool split_url(const std::string& url, std::string& host, std::string& port,
                          std::string& path) {
        const std::string scheme = "http://";
        if (url.rfind(scheme, 0) != 0) return false;  // 示例只走明文;线上网关挂 TLS 反代
        const size_t slash = url.find('/', scheme.size());
        const std::string authority =
            slash == std::string::npos ? url.substr(scheme.size()) : url.substr(scheme.size(), slash - scheme.size());
        path = slash == std::string::npos ? "/" : url.substr(slash);
        const size_t colon = authority.find(':');
        if (colon == std::string::npos) {
            host = authority;
            port = "80";
        } else {
            host = authority.substr(0, colon);
            port = authority.substr(colon + 1);
        }
        return !host.empty();
    }

    static bool send_all(int fd, const std::string& data) {
        size_t off = 0;
        while (off < data.size()) {
            const ssize_t n = ::send(fd, data.data() + off, data.size() - off, 0);
            if (n <= 0) return false;
            off += static_cast<size_t>(n);
        }
        return true;
    }

    static std::string lower(std::string s) {
        for (char& c : s) c = static_cast<char>(std::tolower(static_cast<unsigned char>(c)));
        return s;
    }

    static bool parse_response(const std::string& raw, TransportResult& result) {
        const size_t head_end = raw.find("\r\n\r\n");
        if (head_end == std::string::npos) return false;
        const std::string head = raw.substr(0, head_end);
        std::string body = raw.substr(head_end + 4);

        size_t line_end = head.find("\r\n");
        const std::string status_line = line_end == std::string::npos ? head : head.substr(0, line_end);
        // "HTTP/1.1 200 OK" → 取中间段
        const size_t sp1 = status_line.find(' ');
        const size_t sp2 = status_line.find(' ', sp1 + 1);
        if (sp1 == std::string::npos || sp2 == std::string::npos) return false;
        result.response.status_code = std::atoi(status_line.c_str() + sp1 + 1);

        bool chunked = false;
        while (line_end != std::string::npos) {
            const size_t start = line_end + 2;
            line_end = head.find("\r\n", start);
            const std::string line = line_end == std::string::npos
                                         ? head.substr(start)
                                         : head.substr(start, line_end - start);
            const size_t colon = line.find(':');
            if (colon == std::string::npos) continue;
            result.response.headers[lower(line.substr(0, colon))] = line.substr(colon + 1);
        }
        const auto te = result.response.headers.find("transfer-encoding");
        if (te != result.response.headers.end() && te->second.find("chunked") != std::string::npos) {
            chunked = true;
        }
        result.response.body = chunked ? decode_chunked(body) : body;
        result.ok = true;
        return true;
    }

    static std::string decode_chunked(const std::string& in) {
        std::string out;
        size_t pos = 0;
        while (pos < in.size()) {
            const size_t eol = in.find("\r\n", pos);
            if (eol == std::string::npos) break;
            const size_t size = std::strtoul(in.substr(pos, eol - pos).c_str(), nullptr, 16);
            if (size == 0) break;  // 末块
            out.append(in, eol + 2, size);
            pos = eol + 2 + size + 2;  // 跳过块体 + CRLF
        }
        return out;
    }
};

}  // namespace

int main(int argc, char** argv) {
    const std::string endpoint = argc > 1 ? argv[1] : "http://127.0.0.1:8080";

    // 1) 接入:config 是 scope(gameId/env),传输为接入方适配件;
    //    token_store 传 nullptr = 内置内存存储,正式接入换平台安全存储。
    SocketTransport transport;
    CourierClientOptions opts;
    opts.config = CourierConfig{endpoint, "game_demo", "prod"};
    opts.transport = &transport;
    opts.sleep = [](int) {};  // 示例不等重试间隔
    CourierClient client(opts);

    // 初始化:空存储不发请求,只是把生命周期机置 Ready(后续登录编排需要)。
    if (!client.init()) {
        std::cerr << "init failed" << std::endl;
        return 1;
    }

    // 2) 登录:游客静默登录(不阻断进游戏);响应 data 为会话(account + tokens)。
    GuestRequest guest;
    guest.device_id = "example-device-1";
    const RequestOutcome login = client.guest_login(guest);
    if (!login.ok) {
        std::cerr << "login failed: " << login.error.wire << std::endl;
        return 1;
    }
    const json::Value* account = login.data.find("account");
    const std::string acc_id = account != nullptr ? json::get_str(*account, "id") : "";
    std::cout << "login: type=" << (account != nullptr ? json::get_str(*account, "type") : "")
              << " id=" << acc_id << std::endl;

    // 3) 收公告:匿名可见;本地网关无发布数据时空列表属正常。
    AnnouncementService announcements(&client.api());
    const auto page = announcements.list(20);
    if (!page.ok) {
        std::cerr << "announcements failed: " << page.error.wire << std::endl;
        return 1;
    }
    std::cout << "announcements: " << page.data.items.size() << std::endl;
    for (const AnnouncementDto& item : page.data.items) {
        std::cout << "  [" << item.severity << "] " << item.title << std::endl;
    }

    // 4) 发工单:提交后拉详情,消息含玩家首条正文。
    SupportService support(&client.api());
    CreateTicketRequest create;
    create.title = "示例:闪退反馈";
    create.body = "启动即崩溃,登录账号 " + acc_id;
    const auto ticket = support.create_ticket(create);
    if (!ticket.ok) {
        std::cerr << "create ticket failed: " << ticket.error.wire << std::endl;
        return 1;
    }
    std::cout << "ticket: id=" << ticket.data.id << " status=" << ticket.data.status << std::endl;

    const auto detail = support.get_ticket(ticket.data.id);
    if (!detail.ok) {
        std::cerr << "ticket detail failed: " << detail.error.wire << std::endl;
        return 1;
    }
    std::cout << "detail: messages=" << detail.data.messages.size() << std::endl;
    std::cout << "example: done" << std::endl;
    return 0;
}
