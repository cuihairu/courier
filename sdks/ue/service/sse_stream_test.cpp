// L2 Service 单测(cocos stream.test.ts 同构):SSE 流编排——帧上抛/心跳静默/
// 断线退避重连(Authorization 每连现取)/501/401 终态不重试/Retry-After 优先/
// 健康连重置退避/连接失败重试/净停;退避纯函数封顶/钳制。运行:
//   cd sdks/ue/service
//   g++ -std=c++17 -Wall -Wextra -I. -I../core -I../contract -o /tmp/courier_ue_sse_stream_test sse_stream_test.cpp && /tmp/courier_ue_sse_stream_test
#include <cstdio>
#include <deque>
#include <map>
#include <memory>
#include <string>
#include <vector>

#include "../core/sse_stream.hpp"
#include "sse_stream_client.hpp"

static int gFailed = 0;

#define CHECK(cond, msg)                                     \
    do {                                                     \
        if (!(cond)) {                                       \
            std::printf("FAIL: %s\n", msg);                  \
            gFailed++;                                       \
        }                                                    \
    } while (0)

using namespace courier;

/// 脚本化连接:按序回放行;行尽 = 流尾;failure 非空 = 读时网络中断。
class FakeStream : public SseStream {
public:
    std::vector<std::string> lines;
    std::string failure;
    std::size_t read_idx = 0;
    bool closed = false;

    SseReadResult read_line() override {
        if (!failure.empty()) {
            SseReadResult r;
            r.error.message = failure;
            return r;
        }
        if (read_idx >= lines.size()) {
            SseReadResult r;
            r.eof = true;
            return r;
        }
        SseReadResult r;
        r.ok = true;
        r.line = lines[read_idx++];
        return r;
    }

    void close() override { closed = true; }
};

/// 脚本化接缝:按序回放连接(FakeStream / 失败消息);记录每次请求头。
class FakeSource : public SseSource {
public:
    std::vector<SseStreamRequest> requests;
    std::deque<std::unique_ptr<SseStream>> streams;
    std::deque<std::string> failures;
    int connect_count = 0;

    SseConnectResult connect(const SseStreamRequest& request) override {
        connect_count++;
        requests.push_back(request);
        if (streams.empty()) {
            SseConnectResult r;
            r.error.message = "script exhausted";
            return r;
        }
        if (!failures.empty()) {
            SseConnectResult r;
            r.error.message = failures.front();
            failures.pop_front();
            return r;
        }
        SseConnectResult r;
        r.ok = true;
        r.stream = std::move(streams.front());
        streams.pop_front();
        return r;
    }
};

static SseStreamRequest MakeRequest() {
    SseStreamRequest req;
    req.url = "http://gw.example/v1/messages/stream";
    req.headers["X-Courier-Game-Id"] = "game_demo";
    return req;
}

// ---- 退避纯函数(契约 messages.md 语义规则 3) ----

static void TestBackoff() {
    CHECK(sse_backoff_delay_ms(1, 0, 1000, 60000) == 1000, "base");
    CHECK(sse_backoff_delay_ms(2, 0, 1000, 60000) == 2000, "double");
    CHECK(sse_backoff_delay_ms(3, 0, 1000, 60000) == 4000, "double 2");
    CHECK(sse_backoff_delay_ms(4, 0, 1000, 60000) == 8000, "double 3");
    CHECK(sse_backoff_delay_ms(7, 0, 1000, 60000) == 60000, "cap");
    CHECK(sse_backoff_delay_ms(99, 0, 1000, 60000) == 60000, "cap 2");
    CHECK(sse_backoff_delay_ms(1, 7000, 1000, 60000) == 7000, "retry-after wins");
    CHECK(sse_backoff_delay_ms(1, 0, 500, 60000) == 500, "base clamp low");
    CHECK(sse_backoff_delay_ms(1, 0, 90000, 60000) == 60000, "base clamp high");
    CHECK(sse_backoff_delay_ms(0, 0, 0, 0) == 1, "zero config");
}

// ---- 编排(契约 messages.md 客户端要求) ----

static void TestRunFramesReconnectToken() {
    FakeSource source;
    int token = 0;
    auto conn1 = std::make_unique<FakeStream>();
    conn1->status_code = 200;
    conn1->lines = {"id: 1", "event: announcement.published", "data: {\"id\":\"ann_1\"}", "", ": ping", ""};
    auto conn2 = std::make_unique<FakeStream>();
    conn2->status_code = 200;
    conn2->lines = {"id: 2", "event: config.updated", "data: {\"configVersion\":7}", ""};
    source.streams.push_back(std::move(conn1));
    source.streams.push_back(std::move(conn2));

    std::vector<int> sleeps;
    SseStreamClientOptions options;
    options.source = &source;
    options.sleep = [&sleeps](int ms) { sleeps.push_back(ms); };
    SseStreamClient client(options);

    std::vector<SseEvent> events;
    std::vector<std::string> reasons;
    SseStreamRequest req = MakeRequest();
    req.token_provider = [&token]() { return "tok-" + std::to_string(++token); };
    client.run(req, [&](const SseEvent& e) { events.push_back(e); },
        [&](const std::string& r) { reasons.push_back(r); },
        [&]() { return sleeps.size() >= 4; });  // 四次退避后净停

    CHECK(events.size() == 2, "two events");
    CHECK(events[0].type == "announcement.published", "event 1 type");
    CHECK(events[0].id == "1", "event 1 id");
    CHECK(events[0].data == "{\"id\":\"ann_1\"}", "event 1 data");
    CHECK(events[1].type == "config.updated", "event 2 type");
    CHECK(source.requests.size() == 4, "four connects");
    CHECK(source.requests[0].headers.at("Authorization") == "Bearer tok-1", "first token");
    CHECK(source.requests[1].headers.at("Authorization") == "Bearer tok-2", "reconnect token");
    CHECK(source.requests[1].headers.at("X-Courier-Game-Id") == "game_demo", "scope header");
    CHECK(reasons.empty(), "no terminal reason");
    CHECK(sleeps.size() == 4, "four backoff waits");
    CHECK(sleeps[0] == 1000, "healthy reset 1");
    CHECK(sleeps[1] == 1000, "healthy reset 2");
    CHECK(sleeps[2] == 1000, "first failure base");
    CHECK(sleeps[3] == 2000, "second failure doubles");
    CHECK(source.streams.empty(), "streams consumed");
}

static void TestRun501() {
    FakeSource source;
    auto conn = std::make_unique<FakeStream>();
    conn->status_code = 501;
    source.streams.push_back(std::move(conn));
    SseStreamClientOptions options;
    options.source = &source;
    options.sleep = [](int) {};
    SseStreamClient client(options);
    std::vector<std::string> reasons;
    client.run(MakeRequest(), [](const SseEvent&) {}, [&](const std::string& r) { reasons.push_back(r); });
    CHECK(reasons.size() == 1 && reasons[0] == sse_close_reasons::kCapabilityDisabled, "501 terminal");
    CHECK(source.connect_count == 1, "no retry");
}

static void TestRun401() {
    FakeSource source;
    auto conn = std::make_unique<FakeStream>();
    conn->status_code = 401;
    source.streams.push_back(std::move(conn));
    SseStreamClientOptions options;
    options.source = &source;
    options.sleep = [](int) {};
    SseStreamClient client(options);
    std::vector<std::string> reasons;
    client.run(MakeRequest(), [](const SseEvent&) {}, [&](const std::string& r) { reasons.push_back(r); });
    CHECK(reasons.size() == 1 && reasons[0] == sse_close_reasons::kUnauthenticated, "401 terminal");
    CHECK(source.connect_count == 1, "no retry");
}

static void TestRunRetryAfter() {
    FakeSource source;
    auto conn = std::make_unique<FakeStream>();
    conn->status_code = 429;
    conn->headers["retry-after"] = "7";  // 契约:头键小写归一(实现侧)
    source.streams.push_back(std::move(conn));
    std::vector<int> sleeps;
    SseStreamClientOptions options;
    options.source = &source;
    options.sleep = [&sleeps](int ms) { sleeps.push_back(ms); };
    SseStreamClient client(options);
    client.run(MakeRequest(), [](const SseEvent&) {}, [](const std::string&) {},
        [&]() { return sleeps.size() >= 2; });
    CHECK(sleeps.size() == 2, "two backoff waits");
    CHECK(sleeps[0] == 7000, "retry-after seconds to ms");
    CHECK(sleeps[1] == 2000, "retry-after applies once");
}

static void TestRunConnectFailureRetries() {
    FakeSource source;
    source.failures.push_back("net down");
    auto conn = std::make_unique<FakeStream>();
    conn->status_code = 200;
    conn->lines = {"id: 9", "event: support.ticket_replied", "data: {}", ""};
    source.streams.push_back(std::move(conn));
    std::vector<int> sleeps;
    SseStreamClientOptions options;
    options.source = &source;
    options.sleep = [&sleeps](int ms) { sleeps.push_back(ms); };
    SseStreamClient client(options);
    std::vector<SseEvent> events;
    client.run(MakeRequest(), [&](const SseEvent& e) { events.push_back(e); },
        [](const std::string&) {}, [&]() { return !events.empty(); });
    CHECK(events.size() == 1, "one event after retry");
    CHECK(events[0].type == "support.ticket_replied", "event type");
    CHECK(source.connect_count == 2, "retried once");
    // 事件后下一读循环顶即触发净停(should_stop 以事件为据):不再重连,仅一次失败退避。
    CHECK(sleeps.size() == 1, "one backoff wait");
    CHECK(sleeps[0] == 1000, "failure backoff base");
}

static void TestRunStoppedBeforeStart() {
    FakeSource source;
    SseStreamClientOptions options;
    options.source = &source;
    options.sleep = [](int) {};
    SseStreamClient client(options);
    client.run(MakeRequest(), [](const SseEvent&) {}, [](const std::string&) {}, []() { return true; });
    CHECK(source.connect_count == 0, "no connect");
}

int main() {
    TestBackoff();
    TestRunFramesReconnectToken();
    TestRun501();
    TestRun401();
    TestRunRetryAfter();
    TestRunConnectFailureRetries();
    TestRunStoppedBeforeStart();
    if (gFailed == 0) {
        std::printf("sse_stream_test: all passed\n");
        return 0;
    }
    std::printf("sse_stream_test: %d failed\n", gFailed);
    return 1;
}
