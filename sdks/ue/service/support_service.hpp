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

    /// 申请附件直传(v1.1):登记即时,返回短时效预签名 URL(PUT 直传存储,不经网关)。
    /// 超限 typed SUPPORT_ATTACHMENT_TOO_LARGE(413)/ SUPPORT_ATTACHMENT_LIMIT(409);
    /// 存储未配置 → typed COMMON_CAPABILITY_DISABLED(501)。
    ServiceOutcome<AttachmentUploadResult> request_attachment_upload(
        const std::string& ticket_id, const AttachmentUploadRequest& request) {
        const json::Value body = encode_attachment_upload(request);
        RequestOutcome out = api_->request(
            "POST", "/v1/support/tickets/" + url_encode(ticket_id) + "/attachments", &body, true);
        ServiceOutcome<AttachmentUploadResult> result;
        if (out.ok) {
            result.ok = true;
            result.data = decode_attachment_upload(out.data);
        } else {
            result.error = out.error;
        }
        return result;
    }

    /// 提交举报(F45):受理即返回 id;进展不向举报人反馈(防骚扰设计),无查询端点。
    ServiceOutcome<ReportResult> create_report(const ReportRequest& request) {
        const json::Value body = encode_report(request);
        RequestOutcome out = api_->request("POST", "/v1/support/reports", &body, true);
        ServiceOutcome<ReportResult> result;
        if (out.ok) {
            result.ok = true;
            result.data = decode_report_result(out.data);
        } else {
            result.error = out.error;
        }
        return result;
    }

    /// 我的处罚列表(F45,issuedAt 倒序;处罚判定与执行全在管理面,此处只读投影)。
    ServiceOutcome<Page<PenaltyDto>> list_penalties(int limit, const std::string& cursor = "") {
        std::string path = "/v1/support/penalties?limit=" + std::to_string(limit);
        if (!cursor.empty()) {
            path += "&cursor=" + url_encode(cursor);
        }
        RequestOutcome out = api_->request("GET", path, nullptr, true);
        ServiceOutcome<Page<PenaltyDto>> result;
        if (out.ok) {
            result.ok = true;
            result.data = decode_page(out.data, decode_penalty);
        } else {
            result.error = out.error;
        }
        return result;
    }

    /// 处罚详情(F45,含申诉状态);非本人 / 不存在一律 typed
    /// SUPPORT_PENALTY_NOT_FOUND(404,不泄露存在性)。
    ServiceOutcome<PenaltyDto> get_penalty(const std::string& penalty_id) {
        RequestOutcome out =
            api_->request("GET", "/v1/support/penalties/" + url_encode(penalty_id), nullptr, true);
        ServiceOutcome<PenaltyDto> result;
        if (out.ok) {
            result.ok = true;
            result.data = decode_penalty(out.data);
        } else {
            result.error = out.error;
        }
        return result;
    }

    /// 提交申诉(F45):每处罚至多一个进行中申诉,重复 → typed
    /// SUPPORT_APPEAL_EXISTS(409,不重试);判定结果以拉取详情为准。
    ServiceOutcome<PenaltyDto> appeal_penalty(const std::string& penalty_id) {
        RequestOutcome out = api_->request(
            "POST", "/v1/support/penalties/" + url_encode(penalty_id) + "/appeal", nullptr, true);
        ServiceOutcome<PenaltyDto> result;
        if (out.ok) {
            result.ok = true;
            result.data = decode_penalty(out.data);
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
