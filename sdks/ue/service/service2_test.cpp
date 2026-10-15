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
    "{\"account\":{\"id\":\"acc_1\",\"type\":\"GUEST\",\"status\":\"ACTIVE\",\"createdAt\":\"t\"},\"accessToken\":\"access-1\",\"accessExpiresAt\":\"t\","
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

    // ---- App 域:维护/环境成功解码(可选字段缺省即缺键) ----
    {
        Harness h;
        h.sign_in();
        AppService svc(&h.client->api());

        h.transport.enqueue(200, data_envelope(
            "{\"inMaintenance\":true,\"estimatedRecoveryAt\":\"2026-10-16T00:00:00.000Z\","
            "\"message\":\"rolling\"}"));
        const auto maint = svc.check_maintenance();
        CHECK(maint.ok && maint.data.has_value(), "app maintenance ok");
        CHECK(maint.data->inMaintenance, "maintenance flag true");
        CHECK(maint.data->estimatedRecoveryAt.has_value() &&
                  *maint.data->estimatedRecoveryAt == "2026-10-16T00:00:00.000Z",
              "maintenance recoveryAt decoded");
        CHECK(maint.data->message.has_value() && *maint.data->message == "rolling",
              "maintenance message decoded");

        h.transport.enqueue(200, data_envelope("{}"));
        const auto quiet = svc.check_maintenance();
        CHECK(quiet.ok && quiet.data.has_value(), "app maintenance empty payload ok");
        CHECK(!quiet.data->inMaintenance && !quiet.data->estimatedRecoveryAt.has_value() &&
                  !quiet.data->message.has_value(),
              "maintenance absent fields stay absent");

        h.transport.enqueue(200, data_envelope("{\"gameId\":\"game_demo\",\"env\":\"prod\"}"));
        const auto env = svc.get_environment();
        CHECK(env.ok && env.data.has_value(), "app environment ok");
        CHECK(env.data->gameId == "game_demo" && env.data->env == "prod", "environment echo");

        h.transport.enqueue(400, error_envelope("COMMON_INVALID_ARGUMENT", "bad appVersion"));
        const auto bad = svc.check_update("x y", "");
        CHECK(!bad.ok && bad.error.wire == "COMMON_INVALID_ARGUMENT", "app check_update typed error");
    }

    // ---- Player 域:角色分页解码 + 绑定 + 档案/更新 501 + typed 错误 ----
    {
        Harness h;
        h.sign_in();
        PlayerService svc(&h.client->api());

        h.transport.enqueue(200, data_envelope(
            "{\"items\":[{\"playerId\":\"p_1\",\"boundAt\":\"2026-10-01T00:00:00.000Z\"},"
            "{\"playerId\":\"p_2\",\"boundAt\":\"2026-10-02T00:00:00.000Z\"}],\"nextCursor\":\"c2\"}"));
        const auto chars = svc.list_characters();
        CHECK(chars.ok && chars.data.has_value(), "player list_characters ok");
        CHECK(chars.data->items.size() == 2, "characters decoded count");
        CHECK(chars.data->items[0].playerId == "p_1", "characters[0] playerId");
        CHECK(chars.data->items[1].boundAt == "2026-10-02T00:00:00.000Z", "characters[1] boundAt");
        CHECK(chars.data->nextCursor == "c2", "characters nextCursor");

        h.transport.enqueue(200, data_envelope("{\"items\":[]}"));
        const auto empty_page = svc.list_characters();
        CHECK(empty_page.ok && empty_page.data->items.empty() && empty_page.data->nextCursor.empty(),
              "characters empty page");

        h.transport.enqueue(200, data_envelope("{\"playerId\":\"p_9\",\"boundAt\":\"t\"}"));
        const auto bound = svc.bind_character("p_9");
        CHECK(bound.ok && bound.data.has_value() && bound.data->playerId == "p_9",
              "player bind ok");
        CHECK(h.transport.requests[3].json_body == "{\"playerId\":\"p_9\"}", "player bind body");

        h.transport.enqueue(501, kDisabled);
        CHECK(!svc.get_profile().data.has_value(), "player profile 501 → null");
        h.transport.enqueue(501, kDisabled);
        CHECK(!svc.update_profile("n", "").data.has_value(), "player update 501 → null");

        h.transport.enqueue(400, error_envelope("COMMON_INVALID_ARGUMENT", "character limit"));
        const auto bad = svc.bind_character("p_9");
        CHECK(!bad.ok && bad.error.wire == "COMMON_INVALID_ARGUMENT", "player bind typed error");
        CHECK(!bad.data.has_value(), "player bind error carries no data");
    }

    // ---- RealName 域:状态查询成功 + 提交错误面 + 未核验态字段缺省 ----
    {
        Harness h;
        h.sign_in();
        RealNameService svc(&h.client->api());

        h.transport.enqueue(200, data_envelope(
            "{\"state\":\"VERIFIED\",\"isMinor\":false,\"verifiedAt\":\"2026-10-10T00:00:00.000Z\"}"));
        const auto st = svc.status();
        CHECK(st.ok && st.data.has_value() && st.data->state == "VERIFIED", "realname status ok");
        CHECK(st.data->isMinor.has_value() && !*st.data->isMinor, "realname isMinor false decoded");
        CHECK(st.data->verifiedAt.has_value(), "realname verifiedAt decoded");

        h.transport.enqueue(200, data_envelope("{\"state\":\"UNVERIFIED\"}"));
        const auto none = svc.status();
        CHECK(none.ok && !none.data->isMinor.has_value() && !none.data->verifiedAt.has_value(),
              "UNVERIFIED payload carries no extra fields");

        h.transport.enqueue(400, error_envelope("COMMON_INVALID_ARGUMENT", "bad id number"));
        const auto bad = svc.submit("张三", "x");
        CHECK(!bad.ok && bad.error.wire == "COMMON_INVALID_ARGUMENT", "realname submit typed error");

        h.transport.enqueue(200, data_envelope("{\"allowed\":false}"));
        const auto no_limit = svc.charge_check(100);
        CHECK(no_limit.ok && !no_limit.data->singleLimitCents.has_value() &&
                  !no_limit.data->monthlyLimitCents.has_value(),
              "charge limits omitted stay absent");
    }

    // ---- Payment 域:下单/列表 501 + SKU typed 错误 ----
    {
        Harness h;
        h.sign_in();
        PaymentService svc(&h.client->api());

        h.transport.enqueue(501, kDisabled);
        CHECK(!svc.create_order("sku_1").data.has_value(), "payment create_order 501 → null");
        h.transport.enqueue(501, kDisabled);
        CHECK(!svc.list_orders().data.has_value(), "payment list_orders 501 → null");
        h.transport.enqueue(501, kDisabled);
        CHECK(!svc.get_order("o_1").data.has_value(), "payment get_order 501 → null");

        h.transport.enqueue(404, error_envelope("PAYMENT_SKU_NOT_FOUND", "sku gone"));
        const auto gone = svc.create_order("sku_gone");
        CHECK(!gone.ok && gone.error.wire == "PAYMENT_SKU_NOT_FOUND", "payment sku typed error");
        CHECK(h.transport.requests.size() == 5, "no retry on sku 404");
    }

    // ---- 各域剩余错误面:typed 码原样上抛 ----
    {
        Harness h;
        h.sign_in();
        AppService app(&h.client->api());
        AppConfigService cfg(&h.client->api());
        BrandingService brand(&h.client->api());
        AssistantService assistant(&h.client->api());
        PlayerService player(&h.client->api());
        RealNameService realname(&h.client->api());
        PaymentService pay(&h.client->api());

        h.transport.enqueue(503, error_envelope("COMMON_UNAVAILABLE", "down"));
        CHECK(!app.check_maintenance().ok, "app maintenance error surfaces");
        CHECK(app.get_environment().error.wire == "COMMON_UNAVAILABLE", "app environment error");
        h.transport.enqueue(503, error_envelope("COMMON_UNAVAILABLE", "down"));
        CHECK(!cfg.fetch("android", "1.0.0").ok, "config fetch error surfaces");
        h.transport.enqueue(503, error_envelope("COMMON_UNAVAILABLE", "down"));
        CHECK(!brand.fetch().ok, "branding fetch error surfaces");
        h.transport.enqueue(503, error_envelope("COMMON_UNAVAILABLE", "down"));
        CHECK(assistant.query("q").error.wire == "COMMON_UNAVAILABLE", "assistant error");
        h.transport.enqueue(503, error_envelope("COMMON_UNAVAILABLE", "down"));
        CHECK(player.get_profile().error.wire == "COMMON_UNAVAILABLE", "player profile error");
        h.transport.enqueue(503, error_envelope("COMMON_UNAVAILABLE", "down"));
        CHECK(player.list_characters().error.wire == "COMMON_UNAVAILABLE", "player characters error");
        h.transport.enqueue(503, error_envelope("COMMON_UNAVAILABLE", "down"));
        CHECK(realname.status().error.wire == "COMMON_UNAVAILABLE", "realname status error");
        h.transport.enqueue(503, error_envelope("COMMON_UNAVAILABLE", "down"));
        CHECK(realname.curfew().error.wire == "COMMON_UNAVAILABLE", "realname curfew error");
        h.transport.enqueue(503, error_envelope("COMMON_UNAVAILABLE", "down"));
        CHECK(realname.charge_check(1).error.wire == "COMMON_UNAVAILABLE", "realname charge error");
        h.transport.enqueue(503, error_envelope("COMMON_UNAVAILABLE", "down"));
        CHECK(pay.get_skus().error.wire == "COMMON_UNAVAILABLE", "payment skus error");
    }

    // ---- 匿名游客登录带 platform:请求体下发 platform(可选字段) ----
    {
        Harness h;
        h.client->init();
        h.transport.enqueue(200, data_envelope(kSessionJson));
        const RequestOutcome out = h.client->guest_login(
            GuestRequest{"device-1", std::string("ios")});
        CHECK(out.ok, "guest login with platform ok");
        CHECK(h.transport.requests[0].json_body ==
                  "{\"deviceId\":\"device-1\",\"platform\":\"ios\"}", "guest platform in body");
    }

    // ---- OAuth 换取:可选字段(tokenType/deviceId/platform)原样下发 ----
    {
        Harness h;
        h.transport.enqueue(200, data_envelope(kSessionJson));
        h.client->init();
        h.transport.enqueue(200, data_envelope(kSessionJson));
        OAuthExchangeRequest req;
        req.provider = "google";
        req.token = "gtok";
        req.token_type = std::string("ID_TOKEN");
        req.device_id = std::string("device-1");
        req.platform = std::string("android");
        const RequestOutcome out = h.client->identity().oauth_exchange(req);
        CHECK(out.ok, "oauth exchange ok");
        CHECK(h.transport.requests[0].json_body ==
                  "{\"provider\":\"google\",\"token\":\"gtok\",\"tokenType\":\"ID_TOKEN\","
                  "\"deviceId\":\"device-1\",\"platform\":\"android\"}",
              "oauth exchange body carries optional fields");
    }

    // ---- Config:region 维度入查询 + 未取过缓存时 get 返回 nullptr ----
    {
        Harness h;
        h.sign_in();
        AppConfigService cfg(&h.client->api());
        CHECK(cfg.get("anything") == nullptr, "config get before any fetch → nullptr");

        h.transport.enqueue(200, data_envelope(
            "{\"configVersion\":3,\"items\":{\"k\":\"v\"}}"));
        const auto snap = cfg.fetch("android", "1.0.0", "cn");
        CHECK(snap.ok && snap.data.configVersion == 3, "config fetch with region ok");
        CHECK(h.transport.requests[1].url ==
                  "https://api.example.com/v1/app/config?platform=android&appVersion=1.0.0&region=cn",
              "config url carries region dimension");
        CHECK(cfg.get("k") != nullptr && cfg.get("k")->as_string() == "v", "config get after fetch");
    }

    // ---- App 版本 501 + 维护 501(匿名域的安静降级) ----
    {
        Harness h;
        h.sign_in();
        AppService svc(&h.client->api());
        h.transport.enqueue(501, kDisabled);
        const auto v501 = svc.check_update("1.0.0", "android");
        CHECK(v501.ok && !v501.data.has_value(), "app version 501 → null");
        h.transport.enqueue(501, kDisabled);
        const auto m501 = svc.check_maintenance();
        CHECK(m501.ok && !m501.data.has_value(), "app maintenance 501 → null");
        h.transport.enqueue(501, kDisabled);
        CHECK(!svc.get_environment().data.has_value(), "app environment 501 → null");
    }

    // ---- 订单列表成功解码 + 错误面;档案更新错误;绑定 501 ----
    {
        Harness h;
        h.sign_in();
        PaymentService pay(&h.client->api());
        PlayerService player(&h.client->api());

        h.transport.enqueue(200, data_envelope(
            "{\"items\":[{\"id\":\"o_1\",\"status\":\"PAID\",\"skuId\":\"sku_1\","
            "\"amountCents\":600,\"currency\":\"CNY\",\"createdAt\":\"t\",\"updatedAt\":\"t\"}],"
            "\"nextCursor\":\"c2\"}"));
        const auto orders = pay.list_orders();
        CHECK(orders.ok && orders.data.has_value() && orders.data->items.size() == 1,
              "order list decoded");
        CHECK(orders.data->items[0].status == "PAID" && orders.data->nextCursor == "c2",
              "order list fields");

        h.transport.enqueue(503, error_envelope("COMMON_UNAVAILABLE", "down"));
        CHECK(pay.list_orders().error.wire == "COMMON_UNAVAILABLE", "order list error");

        h.transport.enqueue(400, error_envelope("COMMON_INVALID_ARGUMENT", "bad name"));
        CHECK(player.update_profile("", "x").error.wire == "COMMON_INVALID_ARGUMENT",
              "player update typed error");

        h.transport.enqueue(501, kDisabled);
        CHECK(!player.bind_character("p_1").data.has_value(), "player bind 501 → null");
    }

    // ---- RealName:提交/时段/额度三处 501 降级 ----
    {
        Harness h;
        h.sign_in();
        RealNameService svc(&h.client->api());
        h.transport.enqueue(501, kDisabled);
        CHECK(!svc.submit("n", "i").data.has_value(), "realname submit 501 → null");
        h.transport.enqueue(501, kDisabled);
        CHECK(!svc.curfew().data.has_value(), "realname curfew 501 → null");
        h.transport.enqueue(501, kDisabled);
        CHECK(!svc.charge_check(1).data.has_value(), "realname charge 501 → null");
    }

    if (gFailed == 0) {
        std::printf("ue service2 tests: all green\n");
        return 0;
    }
    std::printf("ue service2 tests: %d FAILED\n", gFailed);
    return 1;
}
