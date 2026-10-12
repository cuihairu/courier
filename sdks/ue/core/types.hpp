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

/// 会话(登录/轮换响应 data)。accountId 不上线(网关不下发),切号检测取 account.id。
struct SessionDto {
    std::optional<AccountDto> account;
    std::string access_token;
    std::string access_expires_at;
    std::string refresh_token;
    std::string refresh_expires_at;
    std::string device_id;
};

/// GET /v1/identity/session(当前会话信息)。
struct SessionInfoDto {
    std::optional<AccountDto> account;
    std::string session_id;
    std::string device_id;
};

/// 已绑定设备(GET /v1/identity/devices 分页项;id 为服务端行 ID,platform 可选)。
struct DeviceDto {
    std::string id;
    std::string device_id;
    std::optional<std::string> platform;
    std::string created_at;
};

struct DeviceListDto {
    std::vector<DeviceDto> items;
    std::string next_cursor;
};

/// POST /v1/identity/account/delete 响应(v1.1):PENDING_DELETE 带一次性 cancel_token;
/// DELETED(立即删:GUEST 或宽限期配置为 0)带 deleted_at。
struct DeleteAccountDto {
    std::string status;  // PENDING_DELETE / DELETED
    std::optional<std::string> cancel_token;
    bool pending_delete = false;
    std::optional<std::string> deleted_at;
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

/// 第三方凭证换会话请求(契约 oauth.md v1.1:SDK 不内置第三方 SDK,凭证由接入方
/// 自行集成取得;凭证只发一次,不本地缓存;token_type 缺省 ID_TOKEN)。
struct OAuthExchangeRequest {
    std::string provider;
    std::string token;
    std::optional<std::string> token_type;  // ID_TOKEN / ACCESS_TOKEN / CODE
    std::optional<std::string> device_id;
    std::optional<std::string> platform;
};

/// 已绑定第三方身份(契约 oauth.md GET bindings:不返回 subject,防客户端侧泄露)。
struct OAuthBindingDto {
    std::string provider;
    std::string created_at;
};

struct OAuthBindingListDto {
    std::vector<OAuthBindingDto> items;
    std::string next_cursor;
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
    if (const json::Value* acc = v.find("account"); acc && acc->is_object()) {
        d.account = decode_account(*acc);
    }
    d.session_id = json::get_str(v, "sessionId");
    d.device_id = json::get_str(v, "deviceId");
    return d;
}

inline DeviceListDto decode_device_list(const json::Value& v) {
    DeviceListDto d;
    if (const json::Value* items = v.find("items"); items && items->is_array()) {
        for (const json::Value& item : items->items()) {
            DeviceDto dev;
            dev.id = json::get_str(item, "id");
            dev.device_id = json::get_str(item, "deviceId");
            std::string platform;
            if (json::get_opt_str(item, "platform", platform)) {
                dev.platform = platform;
            }
            dev.created_at = json::get_str(item, "createdAt");
            d.items.push_back(std::move(dev));
        }
    }
    json::get_opt_str(v, "nextCursor", d.next_cursor);
    return d;
}

inline DeleteAccountDto decode_delete_account(const json::Value& v) {
    DeleteAccountDto d;
    d.status = json::get_str(v, "status");
    std::string s;
    if (json::get_opt_str(v, "cancelToken", s)) {
        d.cancel_token = s;
    }
    if (const json::Value* p = v.find("pendingDelete"); p && p->is_bool()) {
        d.pending_delete = p->as_bool();
    }
    if (json::get_opt_str(v, "deletedAt", s)) {
        d.deleted_at = s;
    }
    return d;
}

inline OAuthBindingListDto decode_oauth_bindings(const json::Value& v) {
    OAuthBindingListDto d;
    if (const json::Value* items = v.find("items"); items && items->is_array()) {
        for (const json::Value& item : items->items()) {
            OAuthBindingDto b;
            b.provider = json::get_str(item, "provider");
            b.created_at = json::get_str(item, "createdAt");
            d.items.push_back(std::move(b));
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

inline json::Value encode_cancel_delete(const std::string& cancel_token) {
    json::Value v = json::Value::object();
    v.set("cancelToken", json::Value::str(cancel_token));
    return v;
}

inline json::Value encode_oauth_exchange(const OAuthExchangeRequest& r) {
    json::Value v = json::Value::object();
    v.set("provider", json::Value::str(r.provider));
    v.set("token", json::Value::str(r.token));
    if (r.token_type.has_value()) {
        v.set("tokenType", json::Value::str(*r.token_type));
    }
    if (r.device_id.has_value()) {
        v.set("deviceId", json::Value::str(*r.device_id));
    }
    if (r.platform.has_value()) {
        v.set("platform", json::Value::str(*r.platform));
    }
    return v;
}

inline json::Value encode_oauth_bind(const std::string& provider, const std::string& token) {
    json::Value v = json::Value::object();
    v.set("provider", json::Value::str(provider));
    v.set("token", json::Value::str(token));
    return v;
}

}  // namespace courier

#endif  // COURIER_CORE_TYPES_HPP
