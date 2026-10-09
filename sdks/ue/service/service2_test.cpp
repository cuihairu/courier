// L2 Service M3/M4/M5 单测(cocos service2.test.ts 同构):app/config/branding/player/
// assistant/payment/realname 的 wire 形状、501 降级语义、typed 错误、缓存判据。
// 运行:
//   cd sdks/ue/service
//   g++ -std=c++17 -Wall -Wextra -I. -I../core -I../contract -o /tmp/courier_ue_service2_test service2_test.cpp && /tmp/courier_ue_service2_test
#include <deque>
#include <memory>
#include <string>
#include <vector>

#include "../core/courier_client.hpp"
#include "app_config_service.hpp"
#include "app_service.hpp"
#include "assistant_service.hpp"
#include "branding_service.hpp"
#include "payment_service.hpp"
#include "player_service.hpp"
#include "realname_service.hpp"

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

static const char* kDisabled =
    "{\"error\":{\"code\":\"COMMON_CAPABILITY_DISABLED\",\"message\":\"not configured\",\"retryable\":false}}";

class FakeTransport : public Transport {
public:
    std::vector<TransportRequest> requests;
    std::deque<TransportResult> script;

    void enqueue(int status, const std::string& body) {
        TransportResult r;
        r.ok = true;
        r.response.status_code = status;
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
    return "{\"error\":{\"code\":\"" + code + "\",\"message\":\"" + message + "\",\"retryable\":false}}";
}

struct Harness {
    FakeTransport transport;
    std::unique_ptr<CourierClient> client;

    Harness() {
        CourierClientOptions options;
        options.config = {"https://api.example.com", "game_demo", "prod"};
        options.transport = &transport;
        options.sleep = [](int) {};
        client = std::make_unique<CourierClient>(std::move(options));
    }

    void sign_in() {
        transport.enqueue(200, data_envelope(kSessionJson));
        client->init();
        client->guest_login(GuestRequest{"device-1", std::nullopt});
    }
};

int main() {
    // ---- App 域(匿名可,501 → null) ----
    {
        Harness h;
        h.sign_in();
        h.transport.enqueue(200, data_envelope(
            "{\"latestVersion\":\"1.2.0\",\"minVersion\":\"1.0.0\",\"updateUrl\":\"https://dl\",\"forceUpdate\":false}"));

        AppService svc(&h.client->api());
        const auto version = svc.check_update("1.1.0", "android");

        CHECK(version.ok, "app check_update ok");
        CHECK(!version.data.has_value() || !version.data->forceUpdate, "forceUpdate false");
        const TransportRequest& req = h.transport.requests[1];
        CHECK(req.headers.find("Authorization") == req.headers.end(), "app anonymous (no Bearer)");
        CHECK(req.url == "https://api.example.com/v1/app/version?appVersion=1.1.0&platform=android",
              "app version url");

        h.transport.enqueue(501, kDisabled);
        h.transport.enqueue(501, kDisabled);  // 每端点一份(非 retryable,单次)
        const auto maint = svc.check_maintenance();
        CHECK(maint.ok && !maint.data.has_value(), "app maintenance 501 → null");
        const auto env = svc.get_environment();
        CHECK(env.ok && !env.data.has_value(), "app environment 501 → null");
    }
    {
        // 维护中 typed APP_MAINTENANCE(503)照常返,不进降级路径
        Harness h;
        h.sign_in();
        h.transport.enqueue(503, error_envelope("APP_MAINTENANCE", "维护中"));

        AppService svc(&h.client->api());
        const auto out = svc.check_maintenance();
        CHECK(!out.ok, "app maintenance 503 fails");
        CHECK(out.error.wire == "APP_MAINTENANCE", "typed wire code");
        CHECK(out.error.http == 503, "typed http status");
    }

    // ---- Config 域(缓存 + 重拉判据 + 501 → 空结果) ----
    {
        Harness h;
        h.sign_in();

        AppConfigService svc(&h.client->api());
        CHECK(svc.needs_refetch(7), "needs_refetch true when never fetched");
        h.transport.enqueue(200, data_envelope(
            "{\"configVersion\":7,\"items\":{\"shop_banner\":\"https://cdn/x.png\",\"limit\":3}}"));
        const auto snapshot = svc.fetch("android", "1.1.0");

        CHECK(snapshot.ok, "config fetch ok");
        CHECK(h.transport.requests[1].url ==
                  "https://api.example.com/v1/app/config?platform=android&appVersion=1.1.0",
              "config fetch url");
        CHECK(snapshot.data.configVersion == 7, "config version");
        CHECK(svc.cached_config() != nullptr && svc.cached_config()->configVersion == 7,
              "config cached");
        const json::Value* banner = svc.get("shop_banner");
        CHECK(banner != nullptr && banner->as_string() == "https://cdn/x.png", "config get banner");
        const json::Value* limit = svc.get("limit");
        CHECK(limit != nullptr && limit->as_int() == 3, "config get limit");
        CHECK(svc.get("missing") == nullptr, "config get missing → nullptr");

        CHECK(!svc.needs_refetch(7), "needs_refetch false for same version");
        CHECK(svc.needs_refetch(8), "needs_refetch true for different version");

        h.transport.enqueue(501, kDisabled);
        const auto empty = svc.fetch();
        CHECK(empty.ok, "config 501 not an error");
        CHECK(empty.data.configVersion == 0, "config 501 → v0");
        CHECK(svc.cached_config() != nullptr && svc.cached_config()->configVersion == 7,
              "config 501 keeps cache");
    }

    // ---- Branding 域(匿名,501 → null,version 判据) ----
    {
        Harness h;
        h.sign_in();

        BrandingService svc(&h.client->api());
        CHECK(svc.needs_refetch(3), "branding needs_refetch true when never fetched");
        h.transport.enqueue(200, data_envelope(
            "{\"version\":3,\"companyName\":\"Demo\",\"primaryColor\":\"#00FF00\"}"));
        const auto dto = svc.fetch();

        CHECK(dto.ok && dto.data.has_value(), "branding fetch ok");
        CHECK(h.transport.requests[1].headers.find("Authorization") == h.transport.requests[1].headers.end(),
              "branding anonymous");
        CHECK(dto.data->version == 3, "branding version");
        const json::Value* company = dto.data->fields.find("companyName");
        CHECK(company != nullptr && company->as_string() == "Demo", "branding companyName passthrough");
        CHECK(!svc.needs_refetch(3), "branding needs_refetch false for same version");
        CHECK(svc.needs_refetch(4), "branding needs_refetch true for different version");

        h.transport.enqueue(501, kDisabled);
        const auto null_dto = svc.fetch();
        CHECK(null_dto.ok && !null_dto.data.has_value(), "branding 501 → null");
    }

    // ---- Player 域 ----
    {
        Harness h;
        h.sign_in();
        h.transport.enqueue(200, data_envelope(
            "{\"displayName\":\"Player\",\"createdAt\":\"t1\",\"updatedAt\":\"t1\"}"));

        PlayerService svc(&h.client->api());
        const auto profile = svc.get_profile();

        CHECK(profile.ok && profile.data.has_value(), "player profile ok");
        CHECK(profile.data->displayName == "Player", "player displayName");
        CHECK(!profile.data->avatarUrl.has_value(), "player avatarUrl absent");

        h.transport.enqueue(200, data_envelope(
            "{\"displayName\":\"新名字\",\"avatarUrl\":\"https://cdn/a.png\",\"createdAt\":\"t1\",\"updatedAt\":\"t2\"}"));
        const auto updated = svc.update_profile("新名字", "https://cdn/a.png");
        CHECK(updated.ok && updated.data.has_value(), "player update ok");
        CHECK(h.transport.requests[2].json_body ==
                  "{\"displayName\":\"新名字\",\"avatarUrl\":\"https://cdn/a.png\"}",
              "player PATCH body");
        CHECK(updated.data->displayName == "新名字", "player updated displayName");

        h.transport.enqueue(501, kDisabled);
        const auto chars = svc.list_characters();
        CHECK(chars.ok && !chars.data.has_value(), "player list_characters 501 → null");
    }

    // ---- Assistant 域 ----
    {
        Harness h;
        h.sign_in();
        h.transport.enqueue(200, data_envelope(
            "{\"matched\":true,\"answer\":{\"id\":\"faq_1\",\"question\":\"怎么找回账号\","
            "\"answer\":\"点忘记密码\",\"keywords\":[\"找回\"]},\"suggestTransfer\":false}"));

        AssistantService svc(&h.client->api());
        const auto hit = svc.query("账号怎么找回");

        CHECK(hit.ok && hit.data.has_value(), "assistant query ok");
        CHECK(h.transport.requests[1].url == "https://api.example.com/v1/assistant/query",
              "assistant query url");
        CHECK(h.transport.requests[1].json_body == "{\"text\":\"账号怎么找回\"}", "assistant query body");
        CHECK(hit.data->matched, "assistant matched true");
        CHECK(hit.data->answer.has_value() && hit.data->answer->question == "怎么找回账号",
              "assistant answer snapshot");

        h.transport.enqueue(200, data_envelope("{\"matched\":false,\"suggestTransfer\":true}"));
        const auto miss = svc.query("如何下载游戏");
        CHECK(miss.ok && miss.data.has_value(), "assistant miss ok");
        CHECK(!miss.data->matched, "assistant miss matched false");
        CHECK(miss.data->suggestTransfer, "assistant miss suggestTransfer true");
        CHECK(!miss.data->answer.has_value(), "assistant miss no answer");

        h.transport.enqueue(501, kDisabled);
        const auto null_q = svc.query("q");
        CHECK(null_q.ok && !null_q.data.has_value(), "assistant 501 → null");
    }

    // ---- Payment 域 ----
    {
        Harness h;
        h.sign_in();
        h.transport.enqueue(200, data_envelope(
            "{\"items\":[{\"id\":\"sku_gem_60\",\"productId\":\"com.demo.gem60\",\"form\":\"DIRECT_PURCHASE\","
            "\"amountCents\":600,\"currency\":\"CNY\"}],\"nextCursor\":\"\"}"));

        PaymentService svc(&h.client->api());
        const auto skus = svc.get_skus();

        CHECK(skus.ok && skus.data.has_value(), "payment skus ok");
        CHECK(skus.data->items.size() == 1, "payment skus count");
        CHECK(skus.data->items[0].amountCents == 600, "payment sku amountCents");

        h.transport.enqueue(200, data_envelope(
            "{\"id\":\"order_1\",\"status\":\"CREATED\",\"skuId\":\"sku_gem_60\",\"productId\":\"com.demo.gem60\","
            "\"form\":\"DIRECT_PURCHASE\",\"amountCents\":600,\"currency\":\"CNY\",\"payToken\":\"sbox_abc\","
            "\"createdAt\":\"t\",\"updatedAt\":\"t\"}"));
        const auto created = svc.create_order("sku_gem_60");

        CHECK(created.ok && created.data.has_value(), "payment create ok");
        CHECK(h.transport.requests[2].url == "https://api.example.com/v1/payments/orders",
              "payment create url");
        CHECK(h.transport.requests[2].json_body == "{\"skuId\":\"sku_gem_60\"}",
              "payment create body skuId only");
        CHECK(created.data->status == "CREATED", "payment create status");
        CHECK(created.data->payToken.has_value() && *created.data->payToken == "sbox_abc",
              "payment create payToken");

        h.transport.enqueue(200, data_envelope(
            "{\"id\":\"order_1\",\"status\":\"DELIVERED\",\"skuId\":\"sku_gem_60\",\"amountCents\":600,"
            "\"currency\":\"CNY\",\"createdAt\":\"t\",\"updatedAt\":\"t2\",\"paidAt\":\"t1\",\"deliveredAt\":\"t2\"}"));
        const auto detail = svc.get_order("order_1");
        CHECK(detail.ok && detail.data.has_value(), "payment detail ok");
        CHECK(detail.data->status == "DELIVERED", "payment detail status DELIVERED");
        CHECK(!detail.data->payToken.has_value(), "payment detail no payToken");
    }
    {
        // 他人订单 typed PAYMENT_ORDER_NOT_FOUND;501 → null(隐藏商城)
        Harness h;
        h.sign_in();
        h.transport.enqueue(404, error_envelope("PAYMENT_ORDER_NOT_FOUND", "订单不存在"));

        PaymentService svc(&h.client->api());
        const auto out = svc.get_order("order_other");
        CHECK(!out.ok, "payment get_order 404 fails");
        CHECK(out.error.wire == "PAYMENT_ORDER_NOT_FOUND", "typed wire code");
        CHECK(h.transport.requests.size() == 2, "no retry on 404");

        h.transport.enqueue(501, kDisabled);
        const auto skus = svc.get_skus();
        CHECK(skus.ok && !skus.data.has_value(), "payment 501 → null");
    }

    // ---- RealName 域 ----
    {
        Harness h;
        h.sign_in();
        h.transport.enqueue(200, data_envelope("{\"state\":\"VERIFIED\",\"isMinor\":true,\"verifiedAt\":\"t\"}"));

        RealNameService svc(&h.client->api());
        const auto status = svc.submit("张三", "110101199001011234");

        CHECK(status.ok && status.data.has_value(), "realname submit ok");
        CHECK(h.transport.requests[1].url == "https://api.example.com/v1/realname/verify",
              "realname submit url");
        CHECK(h.transport.requests[1].json_body == "{\"name\":\"张三\",\"idNumber\":\"110101199001011234\"}",
              "realname submit body");
        CHECK(status.data->state == "VERIFIED", "realname state VERIFIED");

        h.transport.enqueue(200, data_envelope("{\"playable\":false,\"nextWindowAt\":\"t2\"}"));
        const auto curfew = svc.curfew();
        CHECK(curfew.ok && curfew.data.has_value(), "realname curfew ok");
        CHECK(!curfew.data->playable, "realname curfew playable false");
        CHECK(curfew.data->nextWindowAt.has_value() && *curfew.data->nextWindowAt == "t2",
              "realname curfew nextWindowAt");

        h.transport.enqueue(200, data_envelope(
            "{\"allowed\":true,\"singleLimitCents\":10000,\"monthlyLimitCents\":100000,\"monthlyUsedCents\":600}"));
        const auto charge = svc.charge_check(600);
        CHECK(charge.ok && charge.data.has_value(), "realname charge_check ok");
        CHECK(h.transport.requests[3].json_body == "{\"amountCents\":600}",
              "realname charge_check body");
        CHECK(charge.data->allowed, "realname charge allowed");

        h.transport.enqueue(501, kDisabled);
        const auto null_status = svc.status();
        CHECK(null_status.ok && !null_status.data.has_value(), "realname status 501 → null");
    }

    if (gFailed == 0) {
        std::printf("ue service2 tests: all green\n");
        return 0;
    }
    std::printf("ue service2 tests: %d FAILED\n", gFailed);
    return 1;
}
