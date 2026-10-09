// L2 Core 单测(cocos core.test.ts 同构):传输 wire 形状、匿名预检、信封解析与
// 状态码兜底、退避重试(Retry-After 优先)、TOKEN_EXPIRED 刷新重放、安全事件清场、
// 登出尽力而为、bind 不换会话、未知 wire code 容忍。
// 运行:
//   cd sdks/ue/core
//   g++ -std=c++17 -Wall -Wextra -I. -I../contract -o /tmp/courier_ue_core_test core_test.cpp && /tmp/courier_ue_core_test
#include <deque>
#include <string>
#include <vector>

#include "courier_client.hpp"

#include <cstdio>

static int gFailed = 0;

#define CHECK(cond, msg)                                     \
    do {                                                     \
        if (!(cond)) {                                       \
            std::printf("FAIL: %s\n", msg);                  \
            gFailed++;                                       \
        }                                                    \
    } while (0)

using namespace courier;

static const char* kSessionJson =
    "{\"accountId\":\"acc_1\",\"accessToken\":\"access-1\",\"accessExpiresAt\":\"t\","
    "\"refreshToken\":\"refresh-1\",\"refreshExpiresAt\":\"t\",\"deviceId\":\"device-1\"}";

class FakeTransport : public Transport {
public:
    std::vector<TransportRequest> requests;
    std::deque<TransportResult> script;

    void enqueue(int status, const std::string& body,
                 std::map<std::string, std::string> headers = {}) {
        TransportResult r;
        r.ok = true;
        r.response.status_code = status;
        r.response.headers = std::move(headers);
        r.response.body = body;
        script.push_back(std::move(r));
    }

    void enqueue_fail(const std::string& message) {
        TransportResult r;
        r.ok = false;
        r.error.message = message;
        script.push_back(std::move(r));
    }

    TransportResult send(const TransportRequest& request) override {
        requests.push_back(request);
        if (script.empty()) {
            TransportResult r;
            r.ok = false;
            r.error.message = "script exhausted";
            return r;
        }
        TransportResult next = script.front();
        script.pop_front();
        return next;
    }
};

static std::string data_envelope(const std::string& raw) {
    return "{\"data\":" + raw + "}";
}

static std::string error_envelope(const std::string& code, const std::string& message,
                                  const char* retryable = nullptr) {
    std::string body = "{\"error\":{\"code\":\"" + code + "\",\"message\":\"" + message + "\"";
    if (retryable != nullptr) {
        body += std::string(",\"retryable\":") + retryable;
    }
    body += "}}";
    return body;
}

struct Harness {
    FakeTransport transport;
    std::vector<int> sleeps;
    std::unique_ptr<CourierClient> client;

    Harness() {
        CourierClientOptions options;
        options.config = {"https://api.example.com", "game_demo", "prod"};
        options.transport = &transport;
        options.sleep = [this](int ms) { sleeps.push_back(ms); };
        client = std::make_unique<CourierClient>(std::move(options));
    }

    /// 登录(游客)到已认证态。
    void sign_in() {
        transport.enqueue(200, data_envelope(kSessionJson));
        client->init();
        client->guest_login(GuestRequest{"device-1", std::nullopt});
    }
};

int main() {
    // ---- 游客登录 wire 形状:scope 头、无 Bearer、JSON body、会话落库 ----
    {
        Harness h;
        h.transport.enqueue(200, data_envelope(kSessionJson));
        CHECK(h.client->init(), "init ok");
        const RequestOutcome out = h.client->guest_login(GuestRequest{"device-1", std::string("android")});

        CHECK(out.ok, "guest ok");
        const TransportRequest& req = h.transport.requests[0];
        CHECK(req.method == "POST", "guest method POST");
        CHECK(req.url == "https://api.example.com/v1/identity/guest", "guest url");
        CHECK(req.headers.at("X-Courier-Game-Id") == "game_demo", "scope gameId");
        CHECK(req.headers.at("X-Courier-Env") == "prod", "scope env");
        CHECK(req.headers.find("Authorization") == req.headers.end(), "guest anonymous (no Bearer)");
        CHECK(req.headers.at("Content-Type") == "application/json", "guest content-type");
        CHECK(req.json_body == "{\"deviceId\":\"device-1\",\"platform\":\"android\"}", "guest body");
        CHECK(h.client->session().access_token() == "access-1", "session adopted");
        CHECK(h.client->session().current()->refresh_token == "refresh-1", "session persisted");
    }

    // ---- Bearer 附着 vs 匿名请求 ----
    {
        Harness h;
        h.sign_in();
        h.transport.enqueue(200, data_envelope("{}"));
        h.client->api().request("GET", "/v1/app/environment", nullptr, false);  // 匿名
        CHECK(h.transport.requests[1].headers.find("Authorization") == h.transport.requests[1].headers.end(),
              "anonymous request has no Bearer");

        h.transport.enqueue(200, data_envelope("{}"));
        h.client->api().request("GET", "/v1/player/profile", nullptr, true);
        CHECK(h.transport.requests[2].headers.at("Authorization") == "Bearer access-1",
              "authenticated request has Bearer");
    }

    // ---- 未认证本地预检:不发包 ----
    {
        Harness h;
        h.client->init();
        const RequestOutcome out = h.client->api().request("GET", "/v1/player/profile", nullptr, true);
        CHECK(!out.ok && out.error.wire == "COMMON_UNAUTHENTICATED", "precheck wire");
        CHECK(out.error.http == 401 && !out.error.retryable, "precheck 401 non-retryable");
        CHECK(h.transport.requests.empty(), "precheck sends no packet");
    }

    // ---- typed 错误信封:wire/枚举/http/retryable/traceId ----
    {
        Harness h;
        h.sign_in();
        h.transport.enqueue(409, "{\"error\":{\"code\":\"SUPPORT_TICKET_CLOSED\",\"message\":\"closed\","
                                 "\"retryable\":false},\"traceId\":\"tr-9\"}");
        const RequestOutcome out = h.client->api().request("POST", "/v1/support/tickets/t1/messages",
                                                           nullptr, true);
        CHECK(!out.ok, "typed error fails");
        CHECK(out.error.wire == "SUPPORT_TICKET_CLOSED", "typed wire");
        CHECK(out.error.registered && out.error.code == ErrorCode::SupportTicketClosed, "typed enum");
        CHECK(out.error.http == 409 && !out.error.retryable, "typed http/retryable");
        CHECK(out.error.trace_id == "tr-9", "typed traceId");
        CHECK(h.transport.requests.size() == 2, "non-retryable single call");
    }

    // ---- 空体/非 JSON 体:状态码兜底 ----
    {
        Harness h;
        h.sign_in();
        h.transport.enqueue(503, "");
        const RequestOutcome empty = h.client->api().request("GET", "/v1/x", nullptr, true);
        CHECK(!empty.ok && empty.error.wire == "COMMON_UNAVAILABLE" && empty.error.retryable,
              "empty body 503 → UNAVAILABLE retryable");

        h.transport.enqueue(200, "<html>gateway</html>");
        const RequestOutcome nonjson = h.client->api().request("GET", "/v1/x", nullptr, true);
        CHECK(!nonjson.ok && nonjson.error.wire == "COMMON_INTERNAL", "non-JSON 200 → fallback INTERNAL");

        h.transport.enqueue(404, "not found");
        const RequestOutcome nf = h.client->api().request("GET", "/v1/x", nullptr, true);
        CHECK(!nf.ok && nf.error.wire == "COMMON_NOT_FOUND" && !nf.error.retryable, "404 fallback");
    }

    // ---- 重试:Retry-After(秒)优先于 baseDelay ----
    {
        Harness h;
        h.sign_in();
        h.transport.enqueue(503, error_envelope("COMMON_UNAVAILABLE", "busy"),
                            {{"retry-after", "2"}});
        h.transport.enqueue(200, data_envelope("{\"ok\":true}"));
        const RequestOutcome out = h.client->api().request("GET", "/v1/x", nullptr, true);
        CHECK(out.ok, "retry then success");
        CHECK(h.sleeps.size() == 1 && h.sleeps[0] == 2000, "Retry-After 2s → sleep 2000ms");
    }

    // ---- 重试耗尽:3 次尝试 2 次睡眠 ----
    {
        Harness h;
        h.sign_in();
        for (int i = 0; i < 3; i++) {
            h.transport.enqueue(503, error_envelope("COMMON_UNAVAILABLE", "busy"));
        }
        const RequestOutcome out = h.client->api().request("GET", "/v1/x", nullptr, true);
        CHECK(!out.ok && out.error.wire == "COMMON_UNAVAILABLE", "retry exhaustion error");
        CHECK(h.transport.requests.size() == 4, "3 attempts (1 login + 3)");
        CHECK(h.sleeps.size() == 2 && h.sleeps[0] == 300 && h.sleeps[1] == 300, "base delay sleeps");
    }

    // ---- 网络失败归一 UNAVAILABLE 可重试 ----
    {
        Harness h;
        h.sign_in();
        h.transport.enqueue_fail("connection reset");
        h.transport.enqueue(200, data_envelope("{\"ok\":true}"));
        const RequestOutcome out = h.client->api().request("GET", "/v1/x", nullptr, true);
        CHECK(out.ok, "transport failure retried then success");
        CHECK(h.sleeps.size() == 1, "one backoff sleep");
    }

    // ---- TOKEN_EXPIRED:刷新重放一次(新 Bearer + 轮换落库) ----
    {
        Harness h;
        h.sign_in();
        const std::string rotated =
            "{\"accountId\":\"acc_1\",\"accessToken\":\"access-2\",\"accessExpiresAt\":\"t\","
            "\"refreshToken\":\"refresh-2\",\"refreshExpiresAt\":\"t\",\"deviceId\":\"device-1\"}";
        h.transport.enqueue(401, error_envelope("AUTH_TOKEN_EXPIRED", "expired"));
        h.transport.enqueue(200, data_envelope(rotated));            // refresh
        h.transport.enqueue(200, data_envelope("{\"ok\":true}"));    // replay
        const RequestOutcome out = h.client->api().request("GET", "/v1/x", nullptr, true);

        CHECK(out.ok, "replay after refresh ok");
        // [0]=guest login, [1]=原请求(401), [2]=refresh(匿名), [3]=重放。
        CHECK(h.transport.requests[1].url == "https://api.example.com/v1/x", "original request first");
        CHECK(h.transport.requests[2].url == "https://api.example.com/v1/identity/refresh", "refresh url");
        CHECK(h.transport.requests[2].headers.find("Authorization") == h.transport.requests[2].headers.end(),
              "refresh anonymous");
        CHECK(h.transport.requests[3].headers.at("Authorization") == "Bearer access-2",
              "replay carries rotated Bearer");
        CHECK(h.sleeps.empty(), "refresh replay not counted as retry (no sleep)");
        CHECK(h.client->session().access_token() == "access-2", "rotation persisted");
    }

    // ---- 刷新失败:原 401 照返(无重放) ----
    {
        Harness h;
        h.sign_in();
        h.transport.enqueue(401, error_envelope("AUTH_TOKEN_EXPIRED", "expired"));
        h.transport.enqueue(503, error_envelope("COMMON_UNAVAILABLE", "down"));
        const RequestOutcome out = h.client->api().request("GET", "/v1/x", nullptr, true);
        CHECK(!out.ok && out.error.wire == "AUTH_TOKEN_EXPIRED", "refresh failure returns original 401");
        CHECK(h.client->session().access_token() == "access-1", "session kept on transient refresh failure");
    }

    // ---- 安全事件 REUSED:清场 + signed_out ----
    {
        Harness h;
        h.sign_in();
        std::vector<std::string> events;
        h.client->lifecycle().add_listener([&](const LifecycleEvent& e) { events.push_back(e.type); });
        h.transport.enqueue(401, error_envelope("AUTH_TOKEN_EXPIRED", "expired"));
        h.transport.enqueue(401, error_envelope("AUTH_REFRESH_REUSED", "reused"));
        h.client->api().request("GET", "/v1/x", nullptr, true);

        CHECK(!h.client->session().current().has_value(), "session cleared on REUSED");
        CHECK(events.size() == 2 && events[0] == "lifecycle.token_expired" &&
                  events[1] == "lifecycle.signed_out",
              "token_expired then signed_out");
        CHECK(h.client->lifecycle().current() == LifecycleState::kSignedOut, "signed out");
    }

    // ---- 登出尽力而为:服务端失败仍清本地 ----
    {
        Harness h;
        h.sign_in();
        h.transport.enqueue_fail("network down");
        h.client->session().logout();
        CHECK(!h.client->session().current().has_value(), "logout clears locally on failure");
        CHECK(h.client->lifecycle().current() == LifecycleState::kSignedOut, "logout → SignedOut");
    }

    // ---- bind:Bearer,返回账号不换会话 ----
    {
        Harness h;
        h.sign_in();
        h.transport.enqueue(200, data_envelope(
            "{\"id\":\"acc_1\",\"type\":\"EMAIL\",\"email\":\"a@b.c\",\"status\":\"ACTIVE\","
            "\"createdAt\":\"t\"}"));
        const RequestOutcome out = h.client->identity().bind(BindRequest{"a@b.c", "pw"});

        CHECK(out.ok, "bind ok");
        const TransportRequest& req = h.transport.requests[1];
        CHECK(req.url == "https://api.example.com/v1/identity/bind", "bind url");
        CHECK(req.headers.at("Authorization") == "Bearer access-1", "bind Bearer");
        CHECK(decode_account(out.data).email.value_or("") == "a@b.c", "bind returns account");
        CHECK(h.client->session().access_token() == "access-1", "bind does not overwrite session");
    }

    // ---- 未知 wire code 容忍(registered=false,wire 原样) ----
    {
        Harness h;
        h.sign_in();
        h.transport.enqueue(418, error_envelope("FUTURE_CODE_X", "teapot"));
        const RequestOutcome out = h.client->api().request("GET", "/v1/x", nullptr, true);
        CHECK(!out.ok && out.error.wire == "FUTURE_CODE_X" && !out.error.registered, "unknown wire tolerated");
        CHECK(out.error.code == ErrorCode::kCount, "unknown code sentinel");
    }

    // ---- endpoint 尾斜杠归一 + 配置校验 ----
    {
        FakeTransport transport;
        CourierClientOptions options;
        options.config = {"https://api.example.com///", "game_demo", "prod"};
        options.transport = &transport;
        options.sleep = [](int) {};
        CourierClient client(std::move(options));
        transport.enqueue(200, data_envelope(kSessionJson));
        CHECK(client.init(), "init with slash endpoint");
        client.guest_login(GuestRequest{"device-1", std::nullopt});
        CHECK(transport.requests[0].url == "https://api.example.com/v1/identity/guest",
              "trailing slashes trimmed");
    }
    {
        FakeTransport transport;
        CourierClientOptions options;
        options.config = {"https://api.example.com", "game_demo", "  "};
        options.transport = &transport;
        CourierClient client(std::move(options));
        CHECK(!client.config_valid() && !client.init(), "blank env rejected");
        CHECK(client.lifecycle().current() == LifecycleState::kUninitialized, "invalid init no transition");
    }

    // ---- 会话 DTO 全 wire 键 ----
    {
        json::Value v;
        std::string err;
        CHECK(json::parse(data_envelope(kSessionJson), v, err) &&
                  json::parse(v.find("data")->dump(), v, err),
              "session fixture parses");
        const SessionDto s = decode_session(v);
        CHECK(s.account_id == "acc_1" && s.access_token == "access-1" &&
                  s.access_expires_at == "t" && s.refresh_token == "refresh-1" &&
                  s.refresh_expires_at == "t" && s.device_id == "device-1",
              "session DTO full wire keys");
    }

    if (gFailed > 0) {
        std::printf("ue core tests: %d failed\n", gFailed);
        return 1;
    }
    std::printf("ue core tests: all green\n");
    return 0;
}
