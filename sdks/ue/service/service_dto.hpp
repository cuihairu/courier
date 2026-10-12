// L2 Service 服务域 DTO(契约 announcement.md / support.md Frozen v1.1)。
// wire camelCase 与字段语义与网关响应逐一对齐;可选 = 契约缺省字段。
// 分页复用契约信封层 Page<T>(../../contract/envelope.hpp)。
#ifndef COURIER_SERVICE_SERVICE_DTO_HPP
#define COURIER_SERVICE_SERVICE_DTO_HPP

#include <map>
#include <optional>
#include <string>
#include <vector>

#include "../contract/envelope.hpp"
#include "../core/courier_error.hpp"
#include "../core/json.hpp"

namespace courier {

/// 服务层结果:ok 时 data 有效;否则 error 有效(UE 无异常口径,错误随结果返回)。
template <typename T>
struct ServiceOutcome {
    bool ok = false;
    T data{};
    CourierApiError error;
};

/// 公告(契约数据模型;严重度大写枚举字符串)。
struct AnnouncementDto {
    std::string id;
    std::string title;
    std::string body;
    std::string severity;
    std::optional<std::string> startAt;  // 缺省 = 发布即可见
    std::optional<std::string> endAt;    // 缺省 = 不过期
    std::string publishedAt;
    std::optional<std::map<std::string, std::vector<std::string>>> filters;  // 定向过滤(v1.1;缺省 = 全量可见)
};

/// 列表定向参数(v1.1):缺省维度不过滤;值须与发布 filters 维度值精确匹配。
struct AnnouncementQuery {
    std::optional<std::string> platform;
    std::optional<std::string> appVersion;
    std::optional<std::string> region;
    std::optional<std::string> language;
};

/// 工单(状态 OPEN/REPLIED/CLOSED)。
struct TicketDto {
    std::string id;
    std::string title;
    std::string status;
    std::optional<std::string> category;
    std::string createdAt;
    std::string updatedAt;
    std::optional<json::Value> context;  // 服务端零解释透传(v1.1;原样对象)
};

/// 工单消息(senderType: PLAYER/AGENT/SYSTEM,createdAt 升序)。
struct TicketMessageDto {
    std::string senderType;
    std::string body;
    std::string createdAt;
};

/// 工单附件(v1.1;createdAt 升序;downloadUrl 为预签名短时效,对象未直传完成前由存储返回 404)。
struct TicketAttachmentDto {
    std::string id;
    std::string fileName;
    std::string contentType;
    int64_t sizeBytes = 0;
    std::optional<std::string> kind;  // IMAGE/LOG/OTHER
    std::string createdAt;
    std::optional<std::string> downloadUrl;
};

/// 工单详情(ticket + messages + 附件;attachments 为 v1.1 加法扩展)。
struct TicketDetailDto {
    TicketDto ticket;
    std::vector<TicketMessageDto> messages;
    std::vector<TicketAttachmentDto> attachments;
};

/// FAQ 知识条目。
struct FaqDto {
    std::string id;
    std::string question;
    std::string answer;
    std::optional<std::vector<std::string>> keywords;
};

/// 提单请求(POST /support/tickets;category 缺省 OTHER)。context 为 v1.1 开集 map:
/// 键 1–32 位 [a-zA-Z0-9_.],≤10 键,值 string/number/boolean ≤256 字符,整体 ≤1KiB;
/// 服务端零解释透传回 ticket.context。
struct CreateTicketRequest {
    std::string title;
    std::string body;
    std::optional<std::string> category;
    std::map<std::string, json::Value> context;
};

/// 附件直传申请(POST /support/tickets/{id}/attachments;kind 缺省 OTHER)。
struct AttachmentUploadRequest {
    std::string fileName;
    std::string contentType;
    int64_t sizeBytes = 0;
    std::optional<std::string> kind;
};

/// 附件直传申请响应(v1.1):客户端拿 uploadUrl 直传存储(HTTP PUT),不经网关。
struct AttachmentUploadResult {
    std::string attachmentId;
    std::string uploadUrl;
    std::string method;  // PUT
    std::map<std::string, std::string> headers;
    std::string expiresAt;
};

// ---- 解码(wire JSON → DTO;字段缺失安全) ----

inline AnnouncementDto decode_announcement(const json::Value& v) {
    AnnouncementDto d;
    d.id = json::get_str(v, "id");
    d.title = json::get_str(v, "title");
    d.body = json::get_str(v, "body");
    d.severity = json::get_str(v, "severity");
    std::string s;
    if (json::get_opt_str(v, "startAt", s)) d.startAt = s;
    if (json::get_opt_str(v, "endAt", s)) d.endAt = s;
    d.publishedAt = json::get_str(v, "publishedAt");
    if (const json::Value* f = v.find("filters"); f && f->is_object()) {
        std::map<std::string, std::vector<std::string>> m;
        for (const auto& [key, val] : f->members()) {
            if (!val.is_array()) {
                continue;
            }
            std::vector<std::string> dims;
            for (const json::Value& item : val.items()) {
                if (item.is_string()) {
                    dims.push_back(item.as_string());
                }
            }
            m.emplace(key, std::move(dims));
        }
        d.filters = std::move(m);
    }
    return d;
}

inline TicketDto decode_ticket(const json::Value& v) {
    TicketDto d;
    d.id = json::get_str(v, "id");
    d.title = json::get_str(v, "title");
    d.status = json::get_str(v, "status");
    std::string s;
    if (json::get_opt_str(v, "category", s)) d.category = s;
    d.createdAt = json::get_str(v, "createdAt");
    d.updatedAt = json::get_str(v, "updatedAt");
    if (const json::Value* c = v.find("context"); c && c->is_object()) {
        d.context = *c;  // 零解释透传(契约 support.md v1.1)
    }
    return d;
}

inline TicketMessageDto decode_ticket_message(const json::Value& v) {
    TicketMessageDto d;
    d.senderType = json::get_str(v, "senderType");
    d.body = json::get_str(v, "body");
    d.createdAt = json::get_str(v, "createdAt");
    return d;
}

inline TicketAttachmentDto decode_ticket_attachment(const json::Value& v) {
    TicketAttachmentDto d;
    d.id = json::get_str(v, "id");
    d.fileName = json::get_str(v, "fileName");
    d.contentType = json::get_str(v, "contentType");
    if (const json::Value* s = v.find("sizeBytes"); s && s->is_number()) {
        d.sizeBytes = s->as_int();
    }
    std::string s;
    if (json::get_opt_str(v, "kind", s)) d.kind = s;
    d.createdAt = json::get_str(v, "createdAt");
    if (json::get_opt_str(v, "downloadUrl", s)) d.downloadUrl = s;
    return d;
}

inline TicketDetailDto decode_ticket_detail(const json::Value& v) {
    TicketDetailDto d;
    if (const json::Value* t = v.find("ticket"); t && t->is_object()) {
        d.ticket = decode_ticket(*t);
    }
    if (const json::Value* msgs = v.find("messages"); msgs && msgs->is_array()) {
        for (const json::Value& m : msgs->items()) {
            d.messages.push_back(decode_ticket_message(m));
        }
    }
    if (const json::Value* atts = v.find("attachments"); atts && atts->is_array()) {
        for (const json::Value& a : atts->items()) {
            d.attachments.push_back(decode_ticket_attachment(a));
        }
    }
    return d;
}

inline AttachmentUploadResult decode_attachment_upload(const json::Value& v) {
    AttachmentUploadResult d;
    d.attachmentId = json::get_str(v, "attachmentId");
    d.uploadUrl = json::get_str(v, "uploadUrl");
    d.method = json::get_str(v, "method");
    if (const json::Value* hs = v.find("headers"); hs && hs->is_object()) {
        for (const auto& [key, val] : hs->members()) {
            if (val.is_string()) {
                d.headers.emplace(key, val.as_string());
            }
        }
    }
    d.expiresAt = json::get_str(v, "expiresAt");
    return d;
}

inline FaqDto decode_faq(const json::Value& v) {
    FaqDto d;
    d.id = json::get_str(v, "id");
    d.question = json::get_str(v, "question");
    d.answer = json::get_str(v, "answer");
    if (const json::Value* kws = v.find("keywords"); kws && kws->is_array()) {
        std::vector<std::string> k;
        for (const json::Value& kw : kws->items()) {
            if (kw.type() == json::Value::Type::kString) k.push_back(kw.as_string());
        }
        d.keywords = std::move(k);
    }
    return d;
}

/// 分页解码(items + nextCursor;decode 为单项解码器)。
template <typename T>
inline Page<T> decode_page(const json::Value& v, T (*decode)(const json::Value&)) {
    Page<T> page;
    if (const json::Value* items = v.find("items"); items && items->is_array()) {
        for (const json::Value& item : items->items()) {
            page.items.push_back(decode(item));
        }
    }
    json::get_opt_str(v, "nextCursor", page.nextCursor);
    return page;
}

// ---- 编码(DTO → wire JSON;可选字段缺省不下发) ----

inline json::Value encode_create_ticket(const CreateTicketRequest& r) {
    json::Value v = json::Value::object();
    v.set("title", json::Value::str(r.title));
    v.set("body", json::Value::str(r.body));
    if (r.category.has_value()) {
        v.set("category", json::Value::str(*r.category));
    }
    if (!r.context.empty()) {
        json::Value ctx = json::Value::object();
        for (const auto& [key, val] : r.context) {
            ctx.set(key, val);
        }
        v.set("context", std::move(ctx));
    }
    return v;
}

inline json::Value encode_attachment_upload(const AttachmentUploadRequest& r) {
    json::Value v = json::Value::object();
    v.set("fileName", json::Value::str(r.fileName));
    v.set("contentType", json::Value::str(r.contentType));
    v.set("sizeBytes", json::Value::integer(r.sizeBytes));
    if (r.kind.has_value()) {
        v.set("kind", json::Value::str(*r.kind));
    }
    return v;
}

}  // namespace courier

#endif  // COURIER_SERVICE_SERVICE_DTO_HPP
