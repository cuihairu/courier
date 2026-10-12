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
    "{\"account\":{\"id\":\"acc_1\",\"type\":\"GUEST\",\"status\":\"ACTIVE\",\"createdAt\":\"t\"},\"accessToken\":\"access-1\",\"accessExpiresAt\":\"t\","
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

    explicit Harness(const std::string& language = std::string()) {
        CourierClientOptions options;
        options.config = {"https://api.example.com", "game_demo", "prod"};
        if (!language.empty()) {
            options.config.language = language;
        }
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
            svc.create_ticket(CreateTicketRequest{"充值没到账", "订单未发货", std::string("PAYMENT"), {}});

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
            svc.create_ticket(CreateTicketRequest{"t", "b", std::nullopt, {}});
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
        // 附件直传(v1.1):申请响应 wire 形状 + 详情 attachments 加法字段 + 413 typed
        Harness h;
        h.sign_in();
        h.transport.enqueue(200, data_envelope(
            "{\"attachmentId\":\"att_1\",\"uploadUrl\":\"https://storage.local/support/uploads/acc_1/att_1\","
            "\"method\":\"PUT\",\"headers\":{\"X-Storage-Signature\":\"sig-acc_1/att_1\"},"
            "\"expiresAt\":\"2026-10-12T00:10:00.000Z\"}"));

        SupportService svc(&h.client->api());
        const ServiceOutcome<AttachmentUploadResult> up = svc.request_attachment_upload(
            "tkt_1", AttachmentUploadRequest{"crash.log", "text/plain", 1200, std::nullopt});

        CHECK(up.ok, "attachment upload ok");
        const TransportRequest& req = h.transport.requests[1];
        CHECK(req.method == "POST", "attachment upload method");
        CHECK(req.url == "https://api.example.com/v1/support/tickets/tkt_1/attachments",
              "attachment upload url");
        CHECK(req.json_body ==
                  "{\"fileName\":\"crash.log\",\"contentType\":\"text/plain\",\"sizeBytes\":1200}",
              "attachment upload body without kind");
        CHECK(up.data.attachmentId == "att_1", "attachment id");
        CHECK(up.data.method == "PUT", "upload method PUT");
        CHECK(up.data.headers.at("X-Storage-Signature") == "sig-acc_1/att_1", "upload headers");
        CHECK(up.data.expiresAt == "2026-10-12T00:10:00.000Z", "expiresAt");

        // kind 非缺省:键下发
        h.transport.enqueue(200, data_envelope(
            "{\"attachmentId\":\"att_2\",\"uploadUrl\":\"u\",\"method\":\"PUT\",\"headers\":{},"
            "\"expiresAt\":\"t\"}"));
        const ServiceOutcome<AttachmentUploadResult> up2 = svc.request_attachment_upload(
            "tkt_1", AttachmentUploadRequest{"a.png", "image/png", 100, std::string("IMAGE")});
        CHECK(up2.ok, "attachment upload with kind ok");
        CHECK(h.transport.requests[2].json_body ==
                  "{\"fileName\":\"a.png\",\"contentType\":\"image/png\",\"sizeBytes\":100,\"kind\":\"IMAGE\"}",
              "attachment upload body with kind");

        // 详情 attachments[] 加法字段
        h.transport.enqueue(200, data_envelope(
            "{\"ticket\":{\"id\":\"tkt_1\",\"title\":\"问题\",\"status\":\"OPEN\",\"createdAt\":\"t\","
            "\"updatedAt\":\"t\"},\"messages\":[],\"attachments\":[{\"id\":\"att_1\","
            "\"fileName\":\"crash.log\",\"contentType\":\"text/plain\",\"sizeBytes\":1200,"
            "\"kind\":\"LOG\",\"createdAt\":\"2026-10-12T00:00:00.000Z\","
            "\"downloadUrl\":\"https://storage.local/d\"}]}"));
        const ServiceOutcome<TicketDetailDto> detail = svc.get_ticket("tkt_1");
        CHECK(detail.ok, "detail with attachments ok");
        CHECK(detail.data.attachments.size() == 1, "attachments count");
        CHECK(detail.data.attachments[0].id == "att_1", "attachment id");
        CHECK(detail.data.attachments[0].sizeBytes == 1200, "attachment sizeBytes");
        CHECK(detail.data.attachments[0].kind == "LOG", "attachment kind");
        CHECK(detail.data.attachments[0].downloadUrl.has_value() &&
                  *detail.data.attachments[0].downloadUrl == "https://storage.local/d",
              "attachment downloadUrl");

        // 超限 → 413 不重试
        h.transport.enqueue(413, error_envelope("SUPPORT_ATTACHMENT_TOO_LARGE", "超限"));
        const ServiceOutcome<AttachmentUploadResult> big = svc.request_attachment_upload(
            "tkt_1", AttachmentUploadRequest{"big.zip", "application/zip", 10485761, std::nullopt});
        CHECK(!big.ok, "attachment too large fails");
        CHECK(big.error.wire == "SUPPORT_ATTACHMENT_TOO_LARGE", "typed wire code TOO_LARGE");
        CHECK(big.error.http == 413, "typed http status TOO_LARGE");
        CHECK(!big.error.retryable, "not retryable TOO_LARGE");
        CHECK(h.transport.requests.size() == 5, "no retry on 413");
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

    // ---- v1.1 F45:举报与处罚申诉 ----
    {
        // 举报:受理返回 rpt_ id;detail 缺省键不下发
        Harness h;
        h.sign_in();
        h.transport.enqueue(200, data_envelope("{\"id\":\"rpt_1\"}"));

        SupportService svc(&h.client->api());
        ReportRequest report;
        report.targetType = "PLAYER";
        report.targetId = "acc_evil";
        report.reason = "CHEATING";
        report.detail = std::string("自瞄");
        const ServiceOutcome<ReportResult> rep = svc.create_report(report);

        CHECK(rep.ok, "report ok");
        const TransportRequest& req = h.transport.requests[1];
        CHECK(req.method == "POST", "report method");
        CHECK(req.url == "https://api.example.com/v1/support/reports", "report url");
        CHECK(req.json_body ==
                  "{\"targetType\":\"PLAYER\",\"targetId\":\"acc_evil\",\"reason\":\"CHEATING\","
                  "\"detail\":\"自瞄\"}",
              "report body with detail");
        CHECK(rep.data.id == "rpt_1", "report id");

        ReportRequest bare;
        bare.targetType = "TICKET";
        bare.targetId = "tkt_1";
        bare.reason = "SPAM";
        h.transport.enqueue(200, data_envelope("{\"id\":\"rpt_2\"}"));
        const ServiceOutcome<ReportResult> rep2 = svc.create_report(bare);
        CHECK(rep2.ok, "report without detail ok");
        CHECK(h.transport.requests[2].json_body ==
                  "{\"targetType\":\"TICKET\",\"targetId\":\"tkt_1\",\"reason\":\"SPAM\"}",
              "detail omitted when empty");
    }
    {
        // 处罚:列表分页 + 详情申诉字段 + 申诉 409/404 typed 不重试
        Harness h;
        h.sign_in();
        h.transport.enqueue(200, data_envelope(
            "{\"items\":[{\"id\":\"pen_2\",\"type\":\"SUSPENSION\",\"reason\":\"停权 30 天\","
            "\"state\":\"ACTIVE\",\"issuedAt\":\"2026-10-13T10:00:00.000Z\","
            "\"expiresAt\":\"2026-11-12T10:00:00.000Z\"}],\"nextCursor\":\"2\"}"));

        SupportService svc(&h.client->api());
        const ServiceOutcome<Page<PenaltyDto>> page = svc.list_penalties(10, "2");
        CHECK(page.ok, "penalty list ok");
        CHECK(h.transport.requests[1].url ==
                  "https://api.example.com/v1/support/penalties?limit=10&cursor=2",
              "penalty list url with cursor");
        CHECK(page.data.items.size() == 1, "penalty list size");
        CHECK(page.data.items[0].id == "pen_2", "penalty id");
        CHECK(!page.data.items[0].appeal.has_value(), "no appeal by default");
        CHECK(page.data.nextCursor == "2", "penalty nextCursor");

        h.transport.enqueue(200, data_envelope(
            "{\"id\":\"pen_2\",\"type\":\"SUSPENSION\",\"reason\":\"停权 30 天\",\"state\":\"ACTIVE\","
            "\"issuedAt\":\"2026-10-13T10:00:00.000Z\","
            "\"appeal\":{\"state\":\"PENDING\",\"submittedAt\":\"2026-10-13T11:00:00.000Z\"}}"));
        const ServiceOutcome<PenaltyDto> pen = svc.get_penalty("pen_2");
        CHECK(pen.ok, "penalty detail ok");
        CHECK(h.transport.requests[2].url == "https://api.example.com/v1/support/penalties/pen_2",
              "penalty detail url");
        CHECK(pen.data.appeal.has_value() && pen.data.appeal->state == "PENDING",
              "appeal pending");
        CHECK(!pen.data.appeal->decidedAt.has_value(), "no decidedAt before ruling");

        h.transport.enqueue(200, data_envelope(
            "{\"id\":\"pen_2\",\"type\":\"SUSPENSION\",\"reason\":\"停权 30 天\",\"state\":\"ACTIVE\","
            "\"issuedAt\":\"2026-10-13T10:00:00.000Z\","
            "\"appeal\":{\"state\":\"PENDING\",\"submittedAt\":\"2026-10-13T11:00:00.000Z\"}}"));
        const ServiceOutcome<PenaltyDto> appealed = svc.appeal_penalty("pen_2");
        CHECK(appealed.ok, "appeal ok");
        const TransportRequest& ap = h.transport.requests[3];
        CHECK(ap.method == "POST", "appeal method");
        CHECK(ap.url == "https://api.example.com/v1/support/penalties/pen_2/appeal",
              "appeal url");
        CHECK(ap.json_body.empty(), "appeal has no body");
        CHECK(appealed.data.appeal.has_value() && appealed.data.appeal->state == "PENDING",
              "appeal state echoed");

        h.transport.enqueue(409, error_envelope("SUPPORT_APPEAL_EXISTS", "已有申诉"));
        const ServiceOutcome<PenaltyDto> dup = svc.appeal_penalty("pen_2");
        CHECK(!dup.ok, "duplicate appeal fails");
        CHECK(dup.error.wire == "SUPPORT_APPEAL_EXISTS", "typed wire code APPEAL_EXISTS");
        CHECK(dup.error.http == 409, "typed http status APPEAL_EXISTS");
        CHECK(!dup.error.retryable, "not retryable APPEAL_EXISTS");

        h.transport.enqueue(404, error_envelope("SUPPORT_PENALTY_NOT_FOUND", "不存在"));
        const ServiceOutcome<PenaltyDto> miss = svc.get_penalty("pen_nope");
        CHECK(!miss.ok, "absent penalty fails");
        CHECK(miss.error.wire == "SUPPORT_PENALTY_NOT_FOUND", "typed wire code PENALTY_NOT_FOUND");
        CHECK(miss.error.http == 404, "typed http status PENALTY_NOT_FOUND");
        CHECK(!miss.error.retryable, "not retryable PENALTY_NOT_FOUND");
        CHECK(h.transport.requests.size() == 6, "no retry on 409/404");
    }

    // ---- v1.1:公告定向过滤 + 工单上下文 ----
    {
        // 定向参数进查询串;缺省维度不带参数
        Harness h;
        h.sign_in();
        h.transport.enqueue(200, data_envelope("{\"items\":[],\"nextCursor\":\"\"}"));

        AnnouncementQuery q;
        q.platform = "ios";
        q.region = "CN";
        AnnouncementService svc(&h.client->api());
        const ServiceOutcome<Page<AnnouncementDto>> page = svc.list(20, "cur-1", q);

        CHECK(page.ok, "v1.1 announcement list ok");
        CHECK(h.transport.requests[1].url ==
                  "https://api.example.com/v1/announcements?limit=20&cursor=cur-1&platform=ios&region=CN",
              "announcement list v1.1 filter query");

        h.transport.enqueue(200, data_envelope("{\"items\":[],\"nextCursor\":\"\"}"));
        const ServiceOutcome<Page<AnnouncementDto>> plain = svc.list(20);
        CHECK(plain.ok, "v1.1 list default ok");
        CHECK(h.transport.requests[2].url == "https://api.example.com/v1/announcements?limit=20",
              "announcement list default no filter params");
    }
    {
        // 响应 filters 解码为 维度 → 值集合
        Harness h;
        h.sign_in();
        h.transport.enqueue(200, data_envelope(
            "{\"items\":[{\"id\":\"ann_2\",\"title\":\"t\",\"body\":\"b\",\"severity\":\"INFO\","
            "\"publishedAt\":\"t\",\"filters\":{\"platform\":[\"ios\",\"android\"],\"region\":[\"CN\"]}}],"
            "\"nextCursor\":\"\"}"));

        AnnouncementService svc(&h.client->api());
        const ServiceOutcome<Page<AnnouncementDto>> page = svc.list(20);

        CHECK(page.ok, "filters parse ok");
        CHECK(page.data.items.size() == 1, "filters item count");
        CHECK(page.data.items[0].filters.has_value(), "filters present");
        CHECK(page.data.items[0].filters->at("platform").size() == 2, "filters platform size");
        CHECK(page.data.items[0].filters->at("region").at(0) == "CN", "filters region value");
    }
    {
        // F76 多语内容:配置 locale 自动附带 language(仅列表);显式 query.language 覆写
        Harness h("zh-CN");
        h.sign_in();
        h.transport.enqueue(200, data_envelope("{\"items\":[],\"nextCursor\":\"\"}"));

        AnnouncementService svc(&h.client->api(), h.client->config().language);
        const ServiceOutcome<Page<AnnouncementDto>> page = svc.list(20);

        CHECK(page.ok, "f76 list ok");
        CHECK(h.transport.requests[1].url ==
                  "https://api.example.com/v1/announcements?limit=20&language=zh-CN",
              "f76 config language auto attach");

        AnnouncementQuery q;
        q.language = "en";
        h.transport.enqueue(200, data_envelope("{\"items\":[],\"nextCursor\":\"\"}"));
        const ServiceOutcome<Page<AnnouncementDto>> overridden = svc.list(20, "", q);
        CHECK(h.transport.requests[2].url ==
                  "https://api.example.com/v1/announcements?limit=20&language=en",
              "f76 explicit query language wins");
    }
    {
        // F76:行内 language/groupKey 语种声明元数据,只读回显
        Harness h;
        h.sign_in();
        h.transport.enqueue(200, data_envelope(
            "{\"id\":\"ann_f76\",\"title\":\"维护公告\",\"body\":\"停机\","
            "\"severity\":\"INFO\",\"publishedAt\":\"t\","
            "\"language\":\"zh-CN\",\"groupKey\":\"maint\"}"));

        AnnouncementService svc(&h.client->api());
        const ServiceOutcome<AnnouncementDto> out = svc.get("ann_f76");

        CHECK(out.ok, "f76 detail ok");
        CHECK(out.data.language.has_value() && *out.data.language == "zh-CN", "f76 language meta");
        CHECK(out.data.groupKey.has_value() && *out.data.groupKey == "maint", "f76 groupKey meta");
    }
    {
        // 工单 context 编码下发 + 回显零解释透传
        Harness h;
        h.sign_in();
        h.transport.enqueue(200, data_envelope(
            "{\"id\":\"tkt_3\",\"title\":\"t\",\"status\":\"OPEN\",\"createdAt\":\"t\",\"updatedAt\":\"t\","
            "\"context\":{\"orderId\":\"o-1\",\"vip\":true,\"level\":42}}"));

        CreateTicketRequest req;
        req.title = "t";
        req.body = "b";
        req.context["orderId"] = json::Value::str("o-1");
        req.context["vip"] = json::Value::boolean(true);
        req.context["level"] = json::Value::integer(42);
        SupportService svc(&h.client->api());
        const ServiceOutcome<TicketDto> ticket = svc.create_ticket(req);

        CHECK(ticket.ok, "v1.1 create ticket context ok");
        // map 序按键字典序(level < orderId < vip);零解释 = 原样下发。
        CHECK(h.transport.requests[1].json_body.find(
                  "\"context\":{\"level\":42,\"orderId\":\"o-1\",\"vip\":true}") != std::string::npos,
              "context encoded in body");
        CHECK(ticket.data.context.has_value(), "context echoed");
        CHECK(ticket.data.context->find("orderId")->as_string() == "o-1", "context orderId echoed");
        CHECK(ticket.data.context->find("vip")->as_bool(), "context vip echoed");
        CHECK(ticket.data.context->find("level")->as_int() == 42, "context level echoed");
    }

    if (gFailed == 0) {
        std::printf("ue service tests: all green\n");
        return 0;
    }
    std::printf("ue service tests: %d FAILED\n", gFailed);
    return 1;
}
