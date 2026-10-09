// L2 Core 身份域(契约 auth.md F1–F4)。register/login/guest 匿名,bind 需 Bearer;
// 成功响应一律是新会话(bind 除外:返回账号)——经 on_session 交还持有方落库。
// restore(冷启动恢复)、refresh 轮换、登出吊销归 SessionService。
#ifndef COURIER_CORE_IDENTITY_HPP
#define COURIER_CORE_IDENTITY_HPP

#include <functional>
#include <string>

#include "api_client.hpp"
#include "types.hpp"

namespace courier {

class IdentityService {
public:
    IdentityService(ApiClient* api, std::function<void(const SessionDto&)> on_session)
        : api_(api), on_session_(std::move(on_session)) {}

    /// 邮箱+密码注册并登录(F1)。
    RequestOutcome register_account(const CredentialsRequest& request) {
        return send("register", encode_credentials(request));
    }

    /// 邮箱+密码登录(F2)。
    RequestOutcome login(const CredentialsRequest& request) {
        return send("login", encode_credentials(request));
    }

    /// 游客登录(F3,按设备幂等:同设备回到同一游客账号)。
    RequestOutcome guest(const GuestRequest& request) {
        return send("guest", encode_guest(request));
    }

    /// 游客账号绑定邮箱(转正,F4;Bearer;返回转正后账号,不换会话)。
    RequestOutcome bind(const BindRequest& request) {
        json::Value body = encode_bind(request);
        return api_->request("POST", prefix() + "bind", &body, true);
    }

    /// refresh 轮换(F5,匿名;响应为轮换后的新会话)。
    RequestOutcome refresh(const std::string& refresh_token) {
        json::Value body = json::Value::object();
        body.set("refreshToken", json::Value::str(refresh_token));
        return send("refresh", body);
    }

private:
    static std::string prefix() { return "/v1/identity/"; }

    RequestOutcome send(const char* action, const json::Value& body) {
        const RequestOutcome out = api_->request("POST", prefix() + action, &body, false);
        if (out.ok && out.data.is_object() && out.data.has("accessToken")) {
            on_session_(decode_session(out.data));
        }
        return out;
    }

    ApiClient* api_;
    std::function<void(const SessionDto&)> on_session_;
};

}  // namespace courier

#endif  // COURIER_CORE_IDENTITY_HPP
