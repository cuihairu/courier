// L2 Service 服务域 DTO(契约 announcement.md / support.md Frozen v1)。
// wire camelCase 与字段语义与网关响应逐一对齐;可选 = 契约缺省字段。
// 分页复用契约信封层 Page<T>(../../contract/envelope.hpp)。
#ifndef COURIER_SERVICE_SERVICE_DTO_HPP
#define COURIER_SERVICE_SERVICE_DTO_HPP

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
};

/// 工单(状态 OPEN/REPLIED/CLOSED)。
struct TicketDto {
    std::string id;
    std::string title;
    std::string status;
    std::optional<std::string> category;
    std::string createdAt;
    std::string updatedAt;
};

/// 工单消息(senderType: PLAYER/AGENT/SYSTEM,createdAt 升序)。
struct TicketMessageDto {
    std::string senderType;
    std::string body;
    std::string createdAt;
};

/// 工单详情(ticket + messages)。
struct TicketDetailDto {
    TicketDto ticket;
    std::vector<TicketMessageDto> messages;
};

/// FAQ 知识条目。
struct FaqDto {
    std::string id;
    std::string question;
    std::string answer;
    std::optional<std::vector<std::string>> keywords;
};

/// 提单请求(POST /support/tickets;category 缺省 OTHER)。
struct CreateTicketRequest {
    std::string title;
    std::string body;
    std::optional<std::string> category;
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
    return d;
}

inline TicketMessageDto decode_ticket_message(const json::Value& v) {
    TicketMessageDto d;
    d.senderType = json::get_str(v, "senderType");
    d.body = json::get_str(v, "body");
    d.createdAt = json::get_str(v, "createdAt");
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
    return v;
}

}  // namespace courier

#endif  // COURIER_SERVICE_SERVICE_DTO_HPP
