// L2 Core 共享类型(契约 auth.md / primitives.md;wire camelCase,时间 RFC3339 ms UTC)。
// DTO 以 ../../docs/contract/ 为唯一事实源;可选字段 = 缺失/类型不符即缺省。
#ifndef COURIER_CORE_TYPES_HPP
#define COURIER_CORE_TYPES_HPP

#include <cstdint>
#include <optional>
#include <string>
#include <vector>

#include "json.hpp"

namespace courier {

/// URL 路径段编码(RFC 3986 unreserved 之外一律 %XX;设备号/游标/关键词用)。
inline std::string url_encode(const std::string& raw) {
    static const char* kHex = "0123456789ABCDEF";
    std::string out;
    for (const unsigned char c : raw) {
        if ((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
            c == '-' || c == '_' || c == '.' || c == '~') {
            out += static_cast<char>(c);
        } else {
            out += '%';
            out += kHex[c >> 4];
            out += kHex[c & 0x0F];
        }
    }
    return out;
}

/// 连接配置(endpoint 不带尾斜杠也可,ApiClient 归一)。
struct CourierConfig {
    std::string endpoint;
    std::string game_id;
    std::string env;
};

/// 账号(契约 auth.md 数据模型;GUEST 时 email 缺省)。
struct AccountDto {
    std::string id;
    std::string type;  // EMAIL / GUEST
    std::optional<std::string> email;
    std::string status;  // ACTIVE / DISABLED
    std::string created_at;
};

/// 会话(登录/轮换响应 data)。
struct SessionDto {
    std::string account_id;
    std::optional<AccountDto> account;
    std::string access_token;
    std::string access_expires_at;
    std::string refresh_token;
    std::string refresh_expires_at;
    std::string device_id;
};

/// GET /v1/identity/session(当前会话信息)。
struct SessionInfoDto {
    std::string session_id;
    std::string device_id;
};

/// 已绑定设备(GET /v1/identity/devices 分页项)。
struct DeviceDto {
    std::string device_id;
    std::string created_at;
};

struct DeviceListDto {
    std::vector<DeviceDto> items;
    std::string next_cursor;
};

/// 注册/登录请求(契约 auth.md F1/F2)。
struct CredentialsRequest {
    std::string email;
    std::string password;
};

/// 游客登录请求(按设备幂等)。
struct GuestRequest {
    std::string device_id;
    std::optional<std::string> platform;
};

/// 绑定邮箱请求(游客转正;Bearer)。
struct BindRequest {
    std::string email;
    std::string password;
};

// ---- 解码(wire JSON → DTO;字段缺失安全) ----

inline AccountDto decode_account(const json::Value& v) {
    AccountDto d;
    d.id = json::get_str(v, "id");
    d.type = json::get_str(v, "type");
    std::string email;
    if (json::get_opt_str(v, "email", email)) {
        d.email = email;
    }
    d.status = json::get_str(v, "status");
    d.created_at = json::get_str(v, "createdAt");
    return d;
}

inline SessionDto decode_session(const json::Value& v) {
    SessionDto d;
    d.account_id = json::get_str(v, "accountId");
    if (const json::Value* acc = v.find("account"); acc && acc->is_object()) {
        d.account = decode_account(*acc);
    }
    d.access_token = json::get_str(v, "accessToken");
    d.access_expires_at = json::get_str(v, "accessExpiresAt");
    d.refresh_token = json::get_str(v, "refreshToken");
    d.refresh_expires_at = json::get_str(v, "refreshExpiresAt");
    d.device_id = json::get_str(v, "deviceId");
    return d;
}

inline SessionInfoDto decode_session_info(const json::Value& v) {
    SessionInfoDto d;
    d.session_id = json::get_str(v, "sessionId");
    d.device_id = json::get_str(v, "deviceId");
    return d;
}

inline DeviceListDto decode_device_list(const json::Value& v) {
    DeviceListDto d;
    if (const json::Value* items = v.find("items"); items && items->is_array()) {
        for (const json::Value& item : items->items()) {
            DeviceDto dev;
            dev.device_id = json::get_str(item, "deviceId");
            dev.created_at = json::get_str(item, "createdAt");
            d.items.push_back(std::move(dev));
        }
    }
    json::get_opt_str(v, "nextCursor", d.next_cursor);
    return d;
}

// ---- 编码(DTO → wire JSON;可选字段缺省不下发) ----

inline json::Value encode_credentials(const CredentialsRequest& r) {
    json::Value v = json::Value::object();
    v.set("email", json::Value::str(r.email));
    v.set("password", json::Value::str(r.password));
    return v;
}

inline json::Value encode_guest(const GuestRequest& r) {
    json::Value v = json::Value::object();
    v.set("deviceId", json::Value::str(r.device_id));
    if (r.platform.has_value()) {
        v.set("platform", json::Value::str(*r.platform));
    }
    return v;
}

inline json::Value encode_bind(const BindRequest& r) {
    json::Value v = json::Value::object();
    v.set("email", json::Value::str(r.email));
    v.set("password", json::Value::str(r.password));
    return v;
}

}  // namespace courier

#endif  // COURIER_CORE_TYPES_HPP
