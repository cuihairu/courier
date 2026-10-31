// L2 Service:邮件客户端(契约 mail.md Frozen v1)。玩家侧三端点全 Bearer;
// 邮件只是附件载体(发放走接入方钩子);mail.updated 推送为提示,列表为准。
#ifndef COURIER_SERVICE_MAIL_SERVICE_HPP
#define COURIER_SERVICE_MAIL_SERVICE_HPP

#include <optional>
#include <string>
#include <vector>

#include "../core/api_client.hpp"
#include "../core/courier_error.hpp"
#include "../core/types.hpp"
#include "../contract/envelope.hpp"
#include "service_dto.hpp"

namespace courier {

/// 附件(不透明物品描述;无第二价值单位)。
struct MailAttachmentDto {
    std::string id;
    std::string type;
    std::int64_t amount = 0;
    std::optional<std::string> currency;
};

/// 邮件(列表/详情;createdAt 倒序)。
struct MailDto {
    std::string id;
    std::string title;
    std::string body;
    std::vector<MailAttachmentDto> attachments;
    std::string createdAt;
    std::optional<std::string> expiresAt;
};

inline MailAttachmentDto decode_mail_attachment(const json::Value& v) {
    MailAttachmentDto d;
    d.id = json::get_str(v, "id");
    d.type = json::get_str(v, "type");
    d.amount = json::get_i64(v, "amount", 0);
    std::string s;
    if (json::get_opt_str(v, "currency", s)) d.currency = s;
    return d;
}

inline MailDto decode_mail(const json::Value& v) {
    MailDto d;
    d.id = json::get_str(v, "id");
    d.title = json::get_str(v, "title");
    d.body = json::get_str(v, "body");
    if (const json::Value* atts = v.find("attachments"); atts && atts->is_array()) {
        for (const json::Value& a : atts->items()) {
            d.attachments.push_back(decode_mail_attachment(a));
        }
    }
    d.createdAt = json::get_str(v, "createdAt");
    std::string s;
    if (json::get_opt_str(v, "expiresAt", s)) d.expiresAt = s;
    return d;
}

class MailService {
public:
    explicit MailService(ApiClient* api) : api_(api) {}

    /// 我的邮件列表(个人+全量,最新在前;501 → nullopt)。
    ServiceOutcome<std::optional<Page<MailDto>>> list(int limit = 20,
        const std::string& cursor = "") {
        std::string path = "/v1/mail?limit=" + std::to_string(limit);
        if (!cursor.empty()) { path += "&cursor=" + cursor; }
        RequestOutcome out = api_->request("GET", path, nullptr, true);
        ServiceOutcome<std::optional<Page<MailDto>>> result;
        if (out.ok) {
            result.ok = true; result.data = decode_page(out.data, decode_mail);
        } else if (is_capability_disabled(out.error)) {
            result.ok = true; result.data = std::nullopt;
        } else { result.error = out.error; }
        return result;
    }

    /// 邮件详情;不存在/非本人 → typed MAIL_NOT_FOUND。
    ServiceOutcome<std::optional<MailDto>> get(const std::string& mail_id) {
        RequestOutcome out = api_->request("GET", "/v1/mail/" + mail_id, nullptr, true);
        return single(out);
    }

    /// 领取附件(幂等:重复领取同结果,不二次发放)。
    ServiceOutcome<std::optional<MailDto>> claim(const std::string& mail_id) {
        json::Value body = json::Value::object();
        RequestOutcome out = api_->request("POST", "/v1/mail/" + mail_id + "/claim", &body, true);
        return single(out);
    }

private:
    ServiceOutcome<std::optional<MailDto>> single(const RequestOutcome& out) {
        ServiceOutcome<std::optional<MailDto>> result;
        if (out.ok) {
            result.ok = true; result.data = decode_mail(out.data);
        } else if (is_capability_disabled(out.error)) {
            result.ok = true; result.data = std::nullopt;
        } else { result.error = out.error; }
        return result;
    }
    ApiClient* api_;
};

}  // namespace courier

#endif  // COURIER_SERVICE_MAIL_SERVICE_HPP
