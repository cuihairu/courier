// 生命周期状态机单测(cocos lifecycle.test.ts 同构):全合法迁移表 + 非法迁移拒绝 +
// 契约事件面 + 挂起恢复回跳 + 会话域接线(init/认证流/切号/登出)。
// 运行:
//   cd sdks/ue/core
//   g++ -std=c++17 -Wall -Wextra -I. -I../contract -o /tmp/courier_ue_lifecycle_test lifecycle_test.cpp && /tmp/courier_ue_lifecycle_test
#include <cstdio>
#include <deque>
#include <string>
#include <vector>

#include "courier_client.hpp"

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

// ---- 状态机:构造到指定态(合法快路径) ----
static bool machine_at(LifecycleState target, LifecycleMachine& m) {
    if (target == LifecycleState::kUninitialized) return true;
    m.fire(LifecycleTrigger::kInitStarted);
    if (target == LifecycleState::kInitializing) return true;
    m.fire(LifecycleTrigger::kInitCompleted);
    if (target == LifecycleState::kReady) return true;
    m.fire(LifecycleTrigger::kAuthStarted);
    if (target == LifecycleState::kAuthenticating) return true;
    m.fire(LifecycleTrigger::kAuthSucceeded);
    if (target == LifecycleState::kAuthenticated) return true;
    m.fire(LifecycleTrigger::kEnterPlayerReady);
    if (target == LifecycleState::kPlayerReady) return true;
    if (target == LifecycleState::kSignedOut) {
        m.fire(LifecycleTrigger::kSignedOut);
        return true;
    }
    m.fire(LifecycleTrigger::kSuspended);
    if (target == LifecycleState::kSuspended) return true;
    m.fire(LifecycleTrigger::kResumeStarted);
    return target == LifecycleState::kResuming;
}

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

static std::string error_envelope(const std::string& code, const std::string& message) {
    return "{\"error\":{\"code\":\"" + code + "\",\"message\":\"" + message +
           "\",\"retryable\":false}}";
}

struct Harness {
    FakeTransport transport;
    std::vector<std::string> events;
    std::unique_ptr<CourierClient> client;

    Harness() {
        CourierClientOptions options;
        options.config = {"https://api.example.com", "game_demo", "prod"};
        options.transport = &transport;
        options.sleep = [](int) {};
        client = std::make_unique<CourierClient>(std::move(options));
        client->lifecycle().add_listener(
            [this](const LifecycleEvent& e) { events.push_back(e.type); });
    }

    void sign_in() {
        transport.enqueue(200, data_envelope(kSessionJson));
        client->init();
        client->guest_login(GuestRequest{"device-1", std::nullopt});
    }
};

int main() {
    // ---- 全合法迁移表(architecture.md 状态图) ----
    {
        struct Row {
            LifecycleState from;
            LifecycleTrigger trigger;
            LifecycleState to;
        };
        const Row table[] = {
            {LifecycleState::kUninitialized, LifecycleTrigger::kInitStarted, LifecycleState::kInitializing},
            {LifecycleState::kInitializing, LifecycleTrigger::kInitCompleted, LifecycleState::kReady},
            {LifecycleState::kReady, LifecycleTrigger::kAuthStarted, LifecycleState::kAuthenticating},
            {LifecycleState::kAuthenticating, LifecycleTrigger::kAuthSucceeded, LifecycleState::kAuthenticated},
            {LifecycleState::kAuthenticating, LifecycleTrigger::kAuthFailed, LifecycleState::kReady},
            {LifecycleState::kAuthenticated, LifecycleTrigger::kEnterPlayerReady, LifecycleState::kPlayerReady},
            {LifecycleState::kAuthenticated, LifecycleTrigger::kSignedOut, LifecycleState::kSignedOut},
            {LifecycleState::kAuthenticated, LifecycleTrigger::kSuspended, LifecycleState::kSuspended},
            {LifecycleState::kPlayerReady, LifecycleTrigger::kSignedOut, LifecycleState::kSignedOut},
            {LifecycleState::kPlayerReady, LifecycleTrigger::kSuspended, LifecycleState::kSuspended},
            {LifecycleState::kSuspended, LifecycleTrigger::kResumeStarted, LifecycleState::kResuming},
            {LifecycleState::kResuming, LifecycleTrigger::kResumeCompleted, LifecycleState::kPlayerReady},
            {LifecycleState::kSignedOut, LifecycleTrigger::kAuthStarted, LifecycleState::kAuthenticating},
        };
        for (const Row& row : table) {
            LifecycleMachine m;
            CHECK(machine_at(row.from, m), "construct state");
            CHECK(m.fire(row.trigger) && m.current() == row.to, "valid transition");
        }
    }

    // ---- 恢复:回到挂起前状态(Authenticated 挂起 → 回 Authenticated) ----
    {
        LifecycleMachine m;
        machine_at(LifecycleState::kAuthenticated, m);
        m.fire(LifecycleTrigger::kSuspended);
        m.fire(LifecycleTrigger::kResumeStarted);
        m.fire(LifecycleTrigger::kResumeCompleted);
        CHECK(m.current() == LifecycleState::kAuthenticated, "resume returns to pre-suspend state");
    }

    // ---- 非法迁移拒绝(状态不被破坏) ----
    {
        struct Row {
            LifecycleState from;
            LifecycleTrigger trigger;
        };
        const Row invalid[] = {
            {LifecycleState::kReady, LifecycleTrigger::kSuspended},          // 未认证无挂起语义
            {LifecycleState::kUninitialized, LifecycleTrigger::kAuthStarted},
            {LifecycleState::kUninitialized, LifecycleTrigger::kInitCompleted},
            {LifecycleState::kReady, LifecycleTrigger::kEnterPlayerReady},   // 必须先认证
            {LifecycleState::kPlayerReady, LifecycleTrigger::kAuthStarted},
            {LifecycleState::kPlayerReady, LifecycleTrigger::kInitCompleted},
            {LifecycleState::kSignedOut, LifecycleTrigger::kSuspended},
            {LifecycleState::kAuthenticating, LifecycleTrigger::kSignedOut},
        };
        for (const Row& row : invalid) {
            LifecycleMachine m;
            machine_at(row.from, m);
            CHECK(!m.fire(row.trigger), "invalid transition rejected");
            CHECK(m.current() == row.from, "state unchanged after invalid trigger");
        }
    }

    // ---- 契约事件面:仅 init/suspend/resume/signout;auth 三触发不发事件 ----
    {
        LifecycleMachine m;
        std::vector<std::string> events;
        m.add_listener([&](const LifecycleEvent& e) { events.push_back(e.type); });
        m.fire(LifecycleTrigger::kInitStarted);
        m.fire(LifecycleTrigger::kInitCompleted);
        m.fire(LifecycleTrigger::kAuthStarted);
        m.fire(LifecycleTrigger::kAuthSucceeded);
        m.fire(LifecycleTrigger::kEnterPlayerReady);
        m.fire(LifecycleTrigger::kSuspended);
        m.fire(LifecycleTrigger::kResumeStarted);
        m.fire(LifecycleTrigger::kResumeCompleted);
        m.fire(LifecycleTrigger::kSignedOut);
        const std::vector<std::string> expected = {
            "lifecycle.initialized", "lifecycle.suspended", "lifecycle.resumed", "lifecycle.signed_out"};
        CHECK(events == expected, "contract events only");
    }

    // ---- token_expired:事件发出但状态不变 ----
    {
        LifecycleMachine m;
        machine_at(LifecycleState::kPlayerReady, m);
        std::vector<std::string> events;
        m.add_listener([&](const LifecycleEvent& e) { events.push_back(e.type); });
        m.raise_token_expired();
        CHECK(events.size() == 1 && events[0] == "lifecycle.token_expired", "token_expired event");
        CHECK(m.current() == LifecycleState::kPlayerReady, "state unchanged");
    }

    // ---- account_switched:首登不发、切号发、同号重登不发 ----
    {
        LifecycleMachine m;
        std::vector<std::string> events;
        m.add_listener([&](const LifecycleEvent& e) { events.push_back(e.type); });
        CHECK(!m.report_account_id("acc_A"), "first login no switch");
        CHECK(events.empty(), "no event on first login");
        CHECK(m.report_account_id("acc_B"), "switch detected");
        CHECK(events.size() == 1 && events[0] == "lifecycle.account_switched", "switch event");
        CHECK(!m.report_account_id("acc_B"), "same account no switch");
    }

    // ---- 监听器退订 ----
    {
        LifecycleMachine m;
        std::vector<std::string> events;
        const int id = m.add_listener([&](const LifecycleEvent& e) { events.push_back(e.type); });
        m.remove_listener(id);
        m.fire(LifecycleTrigger::kInitStarted);
        m.fire(LifecycleTrigger::kInitCompleted);
        CHECK(events.empty(), "unsubscribed listener silent");
    }

    // ---- 客户端接线:init 发 initialized;重复 init 拒绝 ----
    {
        Harness h;
        CHECK(h.client->lifecycle().current() == LifecycleState::kUninitialized, "starts Uninitialized");
        CHECK(h.client->init(), "init ok");
        CHECK(h.client->lifecycle().current() == LifecycleState::kReady, "init → Ready");
        CHECK(h.events.size() == 1 && h.events[0] == "lifecycle.initialized", "initialized event");
        CHECK(!h.client->init(), "double init rejected");
        CHECK(h.client->lifecycle().current() == LifecycleState::kReady, "state kept after double init");
    }

    // ---- 未 init:登录守卫拒绝,不发包 ----
    {
        Harness h;
        const RequestOutcome out = h.client->guest_login(GuestRequest{"device-1", std::nullopt});
        CHECK(!out.ok && out.error.wire == "COMMON_INVALID_ARGUMENT", "guard rejects before init");
        CHECK(h.transport.requests.empty(), "guard sends no packet");
        CHECK(h.client->lifecycle().current() == LifecycleState::kUninitialized, "state unchanged");
    }

    // ---- 登录全流:Ready→PlayerReady;首登只有 initialized ----
    {
        Harness h;
        h.transport.enqueue(200, data_envelope(kSessionJson));
        h.client->init();
        const RequestOutcome out = h.client->guest_login(GuestRequest{"device-1", std::nullopt});
        CHECK(out.ok, "guest login ok");
        CHECK(h.client->lifecycle().current() == LifecycleState::kPlayerReady, "login → PlayerReady");
        CHECK(h.events.size() == 1 && h.events[0] == "lifecycle.initialized",
              "first login: initialized only (no switch event)");
    }

    // ---- 登录失败:AuthFailed 回退 Ready,可重试 ----
    {
        Harness h;
        h.client->init();
        h.transport.enqueue(401, error_envelope("AUTH_INVALID_CREDENTIALS", "bad"));
        const RequestOutcome failed = h.client->login(CredentialsRequest{"a@b.c", "x"});
        CHECK(!failed.ok && failed.error.wire == "AUTH_INVALID_CREDENTIALS", "login failure surfaces");
        CHECK(h.client->lifecycle().current() == LifecycleState::kReady, "AuthFailed → Ready");

        h.transport.enqueue(200, data_envelope(kSessionJson));
        const RequestOutcome retried = h.client->login(CredentialsRequest{"a@b.c", "right"});
        CHECK(retried.ok && h.client->lifecycle().current() == LifecycleState::kPlayerReady,
              "retry after failure → PlayerReady");
    }

    // ---- access 过期自动 refresh:token_expired 事件;轮换后仍 PlayerReady ----
    {
        Harness h;
        h.sign_in();
        h.events.clear();
        const std::string rotated =
            "{\"accountId\":\"acc_1\",\"accessToken\":\"access-2\",\"accessExpiresAt\":\"t\","
            "\"refreshToken\":\"refresh-2\",\"refreshExpiresAt\":\"t\",\"deviceId\":\"device-1\"}";
        h.transport.enqueue(401, error_envelope("AUTH_TOKEN_EXPIRED", "expired"));
        h.transport.enqueue(200, data_envelope(rotated));
        h.transport.enqueue(200, data_envelope("{\"ok\":true}"));
        h.client->session().info();

        CHECK(h.events.size() == 1 && h.events[0] == "lifecycle.token_expired", "token_expired event");
        CHECK(h.client->lifecycle().current() == LifecycleState::kPlayerReady, "still PlayerReady");
        CHECK(h.client->session().access_token() == "access-2", "rotation persisted");
    }

    // ---- REUSED:清场 + signed_out;SignedOut 可重登 ----
    {
        Harness h;
        h.sign_in();
        h.events.clear();
        h.transport.enqueue(401, error_envelope("AUTH_TOKEN_EXPIRED", "expired"));
        h.transport.enqueue(401, error_envelope("AUTH_REFRESH_REUSED", "reused"));
        h.client->session().info();

        CHECK(h.client->lifecycle().current() == LifecycleState::kSignedOut, "REUSED → SignedOut");
        CHECK(h.events.size() == 2 && h.events[1] == "lifecycle.signed_out", "signed_out event");
        CHECK(!h.client->session().current().has_value(), "session cleared");

        h.transport.enqueue(200, data_envelope(kSessionJson));
        const RequestOutcome again = h.client->guest_login(GuestRequest{"device-1", std::nullopt});
        CHECK(again.ok && h.client->lifecycle().current() == LifecycleState::kPlayerReady,
              "relogin from SignedOut");
    }

    // ---- 登出:signed_out;重复登出不重复发事件 ----
    {
        Harness h;
        h.sign_in();
        h.events.clear();
        h.transport.enqueue(200, data_envelope("null"));
        h.client->session().logout();
        CHECK(h.client->lifecycle().current() == LifecycleState::kSignedOut, "logout → SignedOut");
        CHECK(h.events.size() == 1 && h.events[0] == "lifecycle.signed_out", "signed_out once");

        h.transport.enqueue(200, data_envelope("null"));
        h.client->session().logout();
        CHECK(h.events.size() == 1, "duplicate logout no extra event");
    }

    // ---- 切号:登出后换账号登录 → account_switched ----
    {
        Harness h;
        h.sign_in();
        h.events.clear();
        h.transport.enqueue(200, data_envelope("null"));
        h.client->session().logout();  // → SignedOut

        const std::string other =
            "{\"accountId\":\"acc_2\",\"accessToken\":\"access-9\",\"accessExpiresAt\":\"t\","
            "\"refreshToken\":\"refresh-9\",\"refreshExpiresAt\":\"t\",\"deviceId\":\"device-1\"}";
        h.transport.enqueue(200, data_envelope(other));
        const RequestOutcome out = h.client->guest_login(GuestRequest{"device-1", std::nullopt});

        CHECK(out.ok, "relogin ok");
        const std::vector<std::string> expected = {"lifecycle.signed_out", "lifecycle.account_switched"};
        CHECK(h.events == expected, "signed_out then account_switched");
        CHECK(h.client->lifecycle().current() == LifecycleState::kPlayerReady, "PlayerReady");
    }

    if (gFailed > 0) {
        std::printf("ue lifecycle tests: %d failed\n", gFailed);
        return 1;
    }
    std::printf("ue lifecycle tests: all green\n");
    return 0;
}
