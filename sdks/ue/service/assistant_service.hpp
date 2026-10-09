// L2 Service:小助手客户端(契约 assistant.md Frozen v1)。POST /v1/assistant/query(全 Bearer)。
// 未命中不是错误:返回 matched=false 的结果对象,UI 据此引导转人工;
// 501 能力未接 → null(隐藏小助手入口,能力安静地不存在);无本地缓存(即席查询)。
#ifndef COURIER_SERVICE_ASSISTANT_SERVICE_HPP
#define COURIER_SERVICE_ASSISTANT_SERVICE_HPP

#include <optional>
#include <string>
#include <vector>

#include "../core/api_client.hpp"
#include "../core/courier_error.hpp"
#include "service_dto.hpp"

namespace courier {

/// 知识条目快照(命中时原样返回;契约:不改写)。
struct AssistantAnswerDto {
    std::string id;
    std::string question;
    std::string answer;
    std::optional<std::vector<std::string>> keywords;
};

/// 查询结果:matched=false 时 answer 缺省且 suggestTransfer=true(未命中不是错误)。
struct AssistantQueryDto {
    bool matched = false;
    std::optional<AssistantAnswerDto> answer;
    bool suggestTransfer = false;
};

inline AssistantAnswerDto decode_assistant_answer(const json::Value& v) {
    AssistantAnswerDto d;
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

inline AssistantQueryDto decode_assistant_query(const json::Value& v) {
    AssistantQueryDto d;
    d.matched = json::get_bool(v, "matched", false);
    if (const json::Value* a = v.find("answer"); a && a->is_object()) {
        d.answer = decode_assistant_answer(*a);
    }
    d.suggestTransfer = json::get_bool(v, "suggestTransfer", false);
    return d;
}

class AssistantService {
public:
    explicit AssistantService(ApiClient* api) : api_(api) {}

    /// 提问(1-500 字符;空/超长 = 使用方错误,服务端 400)。
    /// 未命中 → matched=false + suggestTransfer=true;UI 据此调 Support 域提单转人工。
    ServiceOutcome<std::optional<AssistantQueryDto>> query(const std::string& text) {
        json::Value body = json::Value::object();
        body.set("text", json::Value::str(text));
        RequestOutcome out = api_->request("POST", "/v1/assistant/query", &body, true);
        ServiceOutcome<std::optional<AssistantQueryDto>> result;
        if (out.ok) {
            result.ok = true;
            result.data = decode_assistant_query(out.data);
        } else if (is_capability_disabled(out.error)) {
            result.ok = true;
            result.data = std::nullopt;  // 契约:能力未接 → 隐藏小助手入口
        } else {
            result.error = out.error;  // 参数/限流/网络照常返给调用方
        }
        return result;
    }

private:
    ApiClient* api_;
};

}  // namespace courier

#endif  // COURIER_SERVICE_ASSISTANT_SERVICE_HPP
