// L3 平台适配(UE 引擎无关持久化):TokenStore 的文件实现。
// 会话落盘到接入方提供的文件读写面(引擎侧映射到平台安全存储目录,
// 如 Saved/ 或用户偏好目录);损坏/不可读数据按「未认证」清场,不抛。
// 纯 ISO C++17,无引擎依赖;文件 I/O 经注入的 FileSystem 接缝(引擎/测试各自实现)。
#ifndef COURIER_CORE_FILE_TOKEN_STORE_HPP
#define COURIER_CORE_FILE_TOKEN_STORE_HPP

#include <functional>
#include <optional>
#include <string>

#include "json.hpp"
#include "token_store.hpp"
#include "types.hpp"

namespace courier {

/// 文件系统接缝(最小面;引擎侧按平台映射,测试用真实文件或内存 fake)。
/// read_file 成功返回 true 且 out 填内容;失败返回 false(缺失/不可读 → 未认证)。
/// write_file 覆盖写入;remove_file 删除(缺失视为成功)。
struct FileSystem {
    virtual ~FileSystem() = default;
    virtual bool read_file(const std::string& path, std::string& out) = 0;
    virtual bool write_file(const std::string& path, const std::string& contents) = 0;
    virtual bool remove_file(const std::string& path) = 0;
};

/// 会话形状校验(契约 auth.md 会话数据模型):合法会话必须为对象且五个
/// 必填凭证/设备字段均为非空字符串——缺失/空串/非字符串即非会话。
/// (load 用它区分「合法 JSON 但非会话对象」与真实会话;损坏数据按未认证清场。)
inline bool is_session_dto(const json::Value& v) {
    if (!v.is_object()) {
        return false;
    }
    const char* required[] = {"accessToken", "refreshToken", "accessExpiresAt",
                              "refreshExpiresAt", "deviceId"};
    for (const char* key : required) {
        const json::Value* f = v.find(key);
        if (f == nullptr || !f->is_string() || f->as_string().empty()) {
            return false;
        }
    }
    return true;
}

/// 文件持久化 TokenStore(经注入 FileSystem;JSON 序列化整会话;clear 即删文件)。
class FileTokenStore : public TokenStore {
public:
    FileTokenStore(FileSystem* fs, std::string path,
                   std::function<void(const std::string&)> on_error = nullptr)
        : fs_(fs), path_(std::move(path)), on_error_(std::move(on_error)) {}

    std::optional<SessionDto> load() override {
        std::string raw;
        if (fs_ == nullptr || !fs_->read_file(path_, raw) || raw.empty()) {
            return std::nullopt;  // 缺失/不可读:按未认证
        }
        json::Value v;
        std::string err;
        if (!json::parse(raw, v, err)) {
            if (!fs_->remove_file(path_)) {
                report_error("load: corrupt data clear failed for " + path_);
            }
            return std::nullopt;
        }
        if (!is_session_dto(v)) {
            // 合法 JSON 但非会话对象(字符串/数字/数组/`{}`/缺字段/空 token):按未认证清场。
            if (!fs_->remove_file(path_)) {
                report_error("load: non-session clear failed for " + path_);
            }
            return std::nullopt;
        }
        return decode_session(v);
    }

    void save(const SessionDto& session) override {
        if (fs_ == nullptr) {
            return;
        }
        if (!fs_->write_file(path_, encode_session(session).dump())) {
            report_error("save: write_file failed for " + path_);
        }
    }

    void clear() override {
        if (fs_ == nullptr) {
            return;
        }
        if (!fs_->remove_file(path_)) {
            report_error("clear: remove_file failed for " + path_);
        }
    }

private:
    void report_error(const std::string& message) const {
        if (on_error_) {
            on_error_(message);
        }
    }

    FileSystem* fs_;
    std::string path_;
    std::function<void(const std::string&)> on_error_;
};

}  // namespace courier

#endif  // COURIER_CORE_FILE_TOKEN_STORE_HPP
