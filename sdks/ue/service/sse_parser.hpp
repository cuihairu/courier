// L2 Service:SSE 帧解析(契约 messages.md;cocos sseParser.ts 同构)。
// 逐行喂入(引擎侧网络回调按行切分),空行结算一帧;注释帧(:)与未知字段容忍;
// 多行 data 按 SSE 规范以 "\n" 连接。推送为提示,业务事实以拉取端点为准。
#ifndef COURIER_SERVICE_SSE_PARSER_HPP
#define COURIER_SERVICE_SSE_PARSER_HPP

#include <optional>
#include <string>

namespace courier {

struct SseEvent {
    std::string type;  // event 字段(缺省 = "")
    std::string data;  // data 字段(多行以 "\n" 连接)
    std::string id;    // id 字段(缺省 = "")
};

class SseParser {
public:
    /// 喂入一行(不含换行);返回结算出的帧(空行且帧内有内容时)。
    std::optional<SseEvent> feed_line(const std::string& line) {
        if (line.empty()) {
            if (!in_frame_) {
                return std::nullopt;  // 纯注释后的空行:无帧结算
            }
            SseEvent event{event_type_, data_, id_};
            reset();
            return event;
        }
        if (line[0] == ':') {
            return std::nullopt;  // 心跳/注释:忽略,不置帧
        }
        std::string field;
        std::string value;
        const std::size_t colon = line.find(':');
        if (colon == std::string::npos) {
            field = line;
        } else {
            field = line.substr(0, colon);
            value = line.substr(colon + 1);
            if (!value.empty() && value[0] == ' ') {
                value.erase(0, 1);  // 仅剥冒号后单个空格(SSE 规范)
            }
        }
        if (field == "event") {
            event_type_ = value;
            in_frame_ = true;
        } else if (field == "data") {
            data_ = in_frame_ && !data_.empty() ? data_ + "\n" + value : value;
            in_frame_ = true;
        } else if (field == "id") {
            id_ = value;
            in_frame_ = true;
        }
        // 未知字段(retry 等)容忍忽略(契约 versioning.md)。
        return std::nullopt;
    }

    /// 断线重连前清空半帧残留。
    void reset() {
        in_frame_ = false;
        event_type_.clear();
        data_.clear();
        id_.clear();
    }

private:
    bool in_frame_ = false;
    std::string event_type_;
    std::string data_;
    std::string id_;
};

}  // namespace courier

#endif  // COURIER_SERVICE_SSE_PARSER_HPP
