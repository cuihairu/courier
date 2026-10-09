// L2 Core 会话域(契约 auth.md F5–F8):冷启动恢复、refresh 轮换、登出吊销、
// 设备列表。安全事件(REFRESH_REUSED/TOKEN_REVOKED)→ 本地清场
// (整个会话已被服务端吊销,契约 auth.md「重放检测」)。
#ifndef COURIER_CORE_SESSION_HPP
#define COURIER_CORE_SESSION_HPP

#include <optional>
#include <string>

#include "api_client.hpp"
#include "lifecycle.hpp"
#include "token_store.hpp"
#include "types.hpp"

namespace courier {

class SessionService {
public:
    // api 由 CourierClient 两段装配回填(session 先建、api 后建——api 的
    // token_provider/refresh_hook 又回调本服务,天然成环,只能延迟接线)。
    SessionService(TokenStore* store, LifecycleMachine* lifecycle)
        : store_(store), lifecycle_(lifecycle) {}

    /// CourierClient 装配回填(内部接线点,接入方不经此构造)。
    void attach_api(ApiClient* api) { api_ = api; }

    /// 当前会话(冷启动时尝试从 store 恢复;nullopt = 未认证)。
    std::optional<SessionDto> current() { return store_->load(); }

    /// 当前 access token(轮换后自动为新值;空 = 未认证)。
    std::string access_token() {
        const std::optional<SessionDto> s = store_->load();
        return s.has_value() ? s->access_token : std::string();
    }

    /// 落库新会话(登录/轮换成功后由 IdentityService 经 on_session 调用)。
    void adopt(const SessionDto& session) {
        store_->save(session);
        lifecycle_->report_account_id(session.account_id);  // 切号检测(events.md)
    }

    /// 本地清场(登出成功/安全事件后)。
    void clear() { store_->clear(); }

    /// refresh 轮换。返回是否拿到新凭证(同步口径:重入即拒绝)。
    bool refresh() {
        if (refreshing_) {
            return false;  // 重入保护(同步核心无并发;防御递归刷新)
        }
        refreshing_ = true;
        const bool ok = try_refresh();
        refreshing_ = false;
        return ok;
    }

    /// 登出:吊销当前会话(F6,Bearer)+ 本地清场;服务端失败仍清本地(尽力而为)。
    bool logout() {
        bool revoked = false;
        if (api_ != nullptr) {
            const RequestOutcome out = api_->request("POST", "/v1/identity/logout", nullptr, true);
            revoked = out.ok;
        }
        store_->clear();
        lifecycle_->try_fire(LifecycleTrigger::kSignedOut);
        return revoked;
    }

    /// 当前会话信息(F7)。
    RequestOutcome info() {
        return require_api()->request("GET", "/v1/identity/session", nullptr, true);
    }

    /// 已绑定设备列表(F8)。
    RequestOutcome list_devices() {
        return require_api()->request("GET", "/v1/identity/devices", nullptr, true);
    }

    /// 解绑设备并吊销其全部会话(F9;device_id 需 URL 编码)。
    RequestOutcome unbind_device(const std::string& device_id) {
        return require_api()->request("DELETE", "/v1/identity/devices/" + url_encode(device_id),
                                      nullptr, true);
    }

private:
    ApiClient* require_api() {
        return api_;  // 未接线 = 编程错误(须经 CourierClient 构造);调用方判空
    }

    static std::string url_encode(const std::string& raw) {
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

    bool try_refresh() {
        const std::optional<SessionDto> current = store_->load();
        if (!current.has_value()) {
            return false;
        }
        lifecycle_->raise_token_expired();  // 契约事件:自动 refresh 开始(状态不变)
        json::Value body = json::Value::object();
        body.set("refreshToken", json::Value::str(current->refresh_token));
        const RequestOutcome out = require_api()->request("POST", "/v1/identity/refresh", &body, false);
        if (out.ok) {
            store_->save(decode_session(out.data));
            return true;
        }
        if (out.error.wire == "AUTH_REFRESH_REUSED" || out.error.wire == "AUTH_TOKEN_REVOKED") {
            // 安全事件:本地清场 + 生命周期登出(重登;契约 auth.md「重放检测」)。
            store_->clear();
            lifecycle_->try_fire(LifecycleTrigger::kSignedOut);
            return false;
        }
        return false;  // 其余错误:本次 refresh 失败(原 401 由调用方按 outcome 判)
    }

    ApiClient* api_ = nullptr;
    TokenStore* store_;
    LifecycleMachine* lifecycle_;
    bool refreshing_ = false;
};

}  // namespace courier

#endif  // COURIER_CORE_SESSION_HPP
