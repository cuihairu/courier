// L2 Service 单测(cocos service.test.ts 同构):SSE 帧解析 + 公告/客服 wire 形状与
// typed 错误。运行:
//   cd sdks/ue/service
//   g++ -std=c++17 -Wall -Wextra -I. -I../core -I../contract -o /tmp/courier_ue_service_test service_test.cpp && /tmp/courier_ue_service_test
#include <deque>
#include <memory>
#include <string>
#include <vector>

#include "../core/courier_client.hpp"
#include "announcement_service.hpp"
#include "service_dto.hpp"
#include "sse_parser.hpp"
#include "support_service.hpp"

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

    /// 登录(游客)到已认证态。
    void sign_in() {
        transport.enqueue(200, data_envelope(kSessionJson));
        client->init();
        client->guest_login(GuestRequest{"device-1", std::nullopt});
    }
};

int main() {
    // ---- SSE 帧解析(契约 messages.md) ----
    {
        SseParser p;
        CHECK(!p.feed_line(": connected"), "comment ignored");
        CHECK(!p.feed_line(""), "empty after comment: no frame");
        CHECK(!p.feed_line(": ping"), "ping ignored");

        CHECK(!p.feed_line("event: announcement.published"), "event field no settle");
        CHECK(!p.feed_line("id: 7"), "id field no settle");
        CHECK(!p.feed_line("data: {\"id\":\"ann_1\"}"), "data field no settle");
        const std::optional<SseEvent> evt = p.feed_line("");
        CHECK(evt.has_value(), "frame settled on empty line");
        CHECK(evt->type == "announcement.published", "event type");
        CHECK(evt->id == "7", "event id");
        CHECK(evt->data == "{\"id\":\"ann_1\"}", "event data");

        CHECK(!p.feed_line(": ping"), "ping after frame ignored");
        CHECK(!p.feed_line(""), "empty after ping: no frame");
    }
    {
        // 多行 data 按规范 \n 连接;冒号无空格也解析
        SseParser p;
        p.feed_line("event: x.y");
        p.feed_line("data:line1");
        p.feed_line("data: line2");
        const std::optional<SseEvent> evt = p.feed_line("");
        CHECK(evt.has_value(), "multi-line frame settled");
        CHECK(evt->data == "line1\nline2", "multi-line data joined with newline");
    }
    {
        // 未知字段容忍;reset 断线重连复用
        SseParser p;
        p.feed_line("event: a.b");
        p.feed_line("retry: 3000");
        p.feed_line("data: 1");
        p.feed_line("");
        p.reset();
        p.feed_line("data: leftover-from-old-frame");
        const std::optional<SseEvent> evt = p.feed_line("");
        CHECK(evt.has_value(), "frame after reset settled");
        CHECK(evt->type.empty(), "type empty after reset");
        CHECK(evt->data == "leftover-from-old-frame", "data after reset");
    }

    // ---- 公告域(契约 announcement.md) ----
    {
        Harness h;
        h.sign_in();
        h.transport.enqueue(200, data_envelope(
            "{\"items\":[{\"id\":\"ann_1\",\"title\":\"维护公告\",\"body\":\"今晚维护\","
            "\"severity\":\"INFO\",\"startAt\":\"2026-10-09T00:00:00.000Z\","
            "\"endAt\":\"2026-10-10T00:00:00.000Z\",\"publishedAt\":\"2026-10-09T08:00:00.000Z\"}],"
            "\"nextCursor\":\"cur-2\"}"));

        AnnouncementService svc(&h.client->api());
        const ServiceOutcome<Page<AnnouncementDto>> page = svc.list(20, "cur-1");

        CHECK(page.ok, "announcement list ok");
        const TransportRequest& req = h.transport.requests[1];
        CHECK(req.method == "GET", "announcement list method");
        CHECK(req.url == "https://api.example.com/v1/announcements?limit=20&cursor=cur-1",
              "announcement list url with cursor");
        CHECK(req.headers.at("Authorization") == "Bearer access-1", "announcement list Bearer");
        CHECK(page.data.items.size() == 1, "announcement list size");
        CHECK(page.data.items[0].title == "维护公告", "announcement title");
        CHECK(page.data.items[0].startAt.has_value(), "announcement startAt present");
        CHECK(page.data.nextCursor == "cur-2", "announcement nextCursor");
    }
    {
        // 公告详情:typed ANNOUNCEMENT_NOT_FOUND(404,不重试)
        Harness h;
        h.sign_in();
        h.transport.enqueue(404, error_envelope("ANNOUNCEMENT_NOT_FOUND", "不可见"));

        AnnouncementService svc(&h.client->api());
        const ServiceOutcome<AnnouncementDto> out = svc.get("ann_x");

        CHECK(!out.ok, "announcement get fails");
        CHECK(out.error.wire == "ANNOUNCEMENT_NOT_FOUND", "typed wire code");
        CHECK(out.error.http == 404, "typed http status");
        CHECK(!out.error.retryable, "not retryable");
        CHECK(h.transport.requests.size() == 2, "no retry on 404");
    }

    // ---- 客服域(契约 support.md) ----
    {
        // 提单:wire 形状(category 缺省不下发)+ 工单解析
        Harness h;
        h.sign_in();
        h.transport.enqueue(200, data_envelope(
            "{\"id\":\"tkt_1\",\"title\":\"充值没到账\",\"status\":\"OPEN\",\"category\":\"PAYMENT\","
            "\"createdAt\":\"2026-10-09T12:00:00.000Z\",\"updatedAt\":\"2026-10-09T12:00:00.000Z\"}"));

        SupportService svc(&h.client->api());
        const ServiceOutcome<TicketDto> ticket =
            svc.create_ticket(CreateTicketRequest{"充值没到账", "订单未发货", std::string("PAYMENT")});

        CHECK(ticket.ok, "create ticket ok");
        const TransportRequest& req = h.transport.requests[1];
        CHECK(req.method == "POST", "create ticket method");
        CHECK(req.url == "https://api.example.com/v1/support/tickets", "create ticket url");
        CHECK(req.json_body ==
                  "{\"title\":\"充值没到账\",\"body\":\"订单未发货\",\"category\":\"PAYMENT\"}",
              "create ticket body with category");
        CHECK(ticket.data.status == "OPEN", "ticket status OPEN");

        // category 缺省:键不下发
        h.transport.enqueue(200, data_envelope(
            "{\"id\":\"tkt_2\",\"title\":\"t\",\"status\":\"OPEN\",\"createdAt\":\"t\",\"updatedAt\":\"t\"}"));
        const ServiceOutcome<TicketDto> t2 =
            svc.create_ticket(CreateTicketRequest{"t", "b", std::nullopt});
        CHECK(t2.ok, "create ticket without category ok");
        CHECK(h.transport.requests[2].json_body == "{\"title\":\"t\",\"body\":\"b\"}",
              "category omitted when empty");
    }
    {
        // 工单详情:ticket + messages 解析;追加消息 CLOSED → typed 409 不重试
        Harness h;
        h.sign_in();
        h.transport.enqueue(200, data_envelope(
            "{\"ticket\":{\"id\":\"tkt_1\",\"title\":\"问题\",\"status\":\"REPLIED\",\"category\":\"OTHER\","
            "\"createdAt\":\"2026-10-09T12:00:00.000Z\",\"updatedAt\":\"2026-10-09T12:01:00.000Z\"},"
            "\"messages\":[{\"senderType\":\"PLAYER\",\"body\":\"充值没到账\","
            "\"createdAt\":\"2026-10-09T12:00:00.000Z\"},{\"senderType\":\"AGENT\",\"body\":\"已补发\","
            "\"createdAt\":\"2026-10-09T12:01:00.000Z\"}]}"));

        SupportService svc(&h.client->api());
        const ServiceOutcome<TicketDetailDto> detail = svc.get_ticket("tkt_1");

        CHECK(detail.ok, "ticket detail ok");
        CHECK(h.transport.requests[1].url == "https://api.example.com/v1/support/tickets/tkt_1",
              "ticket detail url");
        CHECK(detail.data.ticket.status == "REPLIED", "ticket status REPLIED");
        CHECK(detail.data.messages.size() == 2, "messages count");
        CHECK(detail.data.messages[1].senderType == "AGENT", "second message sender");

        // CLOSED → 409 不重试
        h.transport.enqueue(409, error_envelope("SUPPORT_TICKET_CLOSED", "已关单"));
        const ServiceOutcome<TicketMessageDto> append = svc.append_message("tkt_1", "再问一句");
        CHECK(!append.ok, "append message fails");
        CHECK(append.error.wire == "SUPPORT_TICKET_CLOSED", "typed wire code CLOSED");
        CHECK(append.error.http == 409, "typed http status CLOSED");
        CHECK(!append.error.retryable, "not retryable CLOSED");
        CHECK(h.transport.requests.size() == 3, "no retry on 409");
        CHECK(h.transport.requests[2].url ==
                  "https://api.example.com/v1/support/tickets/tkt_1/messages",
              "append message url");
        CHECK(h.transport.requests[2].json_body == "{\"body\":\"再问一句\"}", "append message body");
    }
    {
        // FAQ 检索:关键词 URL 编码;空关键词不带参数
        Harness h;
        h.sign_in();
        h.transport.enqueue(200, data_envelope(
            "{\"items\":[{\"id\":\"faq_1\",\"question\":\"怎么找回账号\",\"answer\":\"点忘记密码\"}],"
            "\"nextCursor\":\"\"}"));

        SupportService svc(&h.client->api());
        const ServiceOutcome<Page<FaqDto>> page = svc.faq("找回 账号", 20);

        CHECK(page.ok, "faq ok");
        CHECK(h.transport.requests[1].url ==
                  "https://api.example.com/v1/support/faq?limit=20&keyword="
                  "%E6%89%BE%E5%9B%9E%20%E8%B4%A6%E5%8F%B7",
              "faq keyword url-encoded");
        CHECK(page.data.items.size() == 1, "faq items count");
        CHECK(page.data.items[0].id == "faq_1", "faq item id");

        // 空关键词不带参数
        h.transport.enqueue(200, data_envelope("{\"items\":[],\"nextCursor\":\"\"}"));
        const ServiceOutcome<Page<FaqDto>> empty = svc.faq("", 20);
        CHECK(empty.ok, "faq empty keyword ok");
        CHECK(h.transport.requests[2].url == "https://api.example.com/v1/support/faq?limit=20",
              "faq empty keyword no param");
    }

    if (gFailed == 0) {
        std::printf("ue service tests: all green\n");
        return 0;
    }
    std::printf("ue service tests: %d FAILED\n", gFailed);
    return 1;
}
