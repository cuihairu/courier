// L2 Service:客服域客户端(契约 support.md:玩家侧工单/消息/FAQ)。
// 坐席回复的实时感知走 messages 通道(见 SseParser);通道关闭 → 拉取详情兜底。
#ifndef COURIER_SERVICE_SUPPORT_SERVICE_HPP
#define COURIER_SERVICE_SUPPORT_SERVICE_HPP

#include "../core/api_client.hpp"
#include "../core/types.hpp"
#include "service_dto.hpp"

namespace courier {

class SupportService {
public:
    explicit SupportService(ApiClient* api) : api_(api) {}

    /// 提单(category 可空 = OTHER);限流超限 typed RATE_LIMITED。
    ServiceOutcome<TicketDto> create_ticket(const CreateTicketRequest& request) {
        const json::Value body = encode_create_ticket(request);
        RequestOutcome out = api_->request("POST", "/v1/support/tickets", &body, true);
        ServiceOutcome<TicketDto> result;
        if (out.ok) {
            result.ok = true;
            result.data = decode_ticket(out.data);
        } else {
            result.error = out.error;
        }
        return result;
    }

    /// 我的工单列表(updatedAt 倒序)。
    ServiceOutcome<Page<TicketDto>> list_tickets(int limit, const std::string& cursor = "") {
        std::string path = "/v1/support/tickets?limit=" + std::to_string(limit);
        if (!cursor.empty()) {
            path += "&cursor=" + url_encode(cursor);
        }
        RequestOutcome out = api_->request("GET", path, nullptr, true);
        ServiceOutcome<Page<TicketDto>> result;
        if (out.ok) {
            result.ok = true;
            result.data = decode_page(out.data, decode_ticket);
        } else {
            result.error = out.error;
        }
        return result;
    }

    /// 工单详情(ticket + messages 升序);非本人 → typed SUPPORT_TICKET_NOT_FOUND。
    ServiceOutcome<TicketDetailDto> get_ticket(const std::string& ticket_id) {
        RequestOutcome out =
            api_->request("GET", "/v1/support/tickets/" + url_encode(ticket_id), nullptr, true);
        ServiceOutcome<TicketDetailDto> result;
        if (out.ok) {
            result.ok = true;
            result.data = decode_ticket_detail(out.data);
        } else {
            result.error = out.error;
        }
        return result;
    }

    /// 追加玩家消息;工单 CLOSED → typed SUPPORT_TICKET_CLOSED(409,不重试)。
    ServiceOutcome<TicketMessageDto> append_message(const std::string& ticket_id,
                                                    const std::string& body) {
        json::Value payload = json::Value::object();
        payload.set("body", json::Value::str(body));
        RequestOutcome out = api_->request(
            "POST", "/v1/support/tickets/" + url_encode(ticket_id) + "/messages", &payload, true);
        ServiceOutcome<TicketMessageDto> result;
        if (out.ok) {
            result.ok = true;
            result.data = decode_ticket_message(out.data);
        } else {
            result.error = out.error;
        }
        return result;
    }

    /// FAQ 检索(关键词命中为空是常态,不是错误)。
    ServiceOutcome<Page<FaqDto>> faq(const std::string& keyword, int limit) {
        std::string path = "/v1/support/faq?limit=" + std::to_string(limit);
        if (!keyword.empty()) {
            path += "&keyword=" + url_encode(keyword);
        }
        RequestOutcome out = api_->request("GET", path, nullptr, true);
        ServiceOutcome<Page<FaqDto>> result;
        if (out.ok) {
            result.ok = true;
            result.data = decode_page(out.data, decode_faq);
        } else {
            result.error = out.error;
        }
        return result;
    }

private:
    ApiClient* api_;
};

}  // namespace courier

#endif  // COURIER_SERVICE_SUPPORT_SERVICE_HPP
