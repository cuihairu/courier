// Courier UE SDK 门面(cocos courierClient.ts / unity CourierClient 同构):装配
// 生命周期状态机 + ApiClient + 身份/会话,域服务随批次挂入(与 unity CourierServices
// 门面对齐)。依赖方向 service→core;core 零平台引用(平台差异收敛在 Transport/TokenStore)。
#ifndef COURIER_CORE_COURIER_CLIENT_HPP
#define COURIER_CORE_COURIER_CLIENT_HPP

#include <memory>
#include <string>
#include <utility>

#include "api_client.hpp"
#include "identity.hpp"
#include "lifecycle.hpp"
#include "session.hpp"
#include "token_store.hpp"
#include "types.hpp"

namespace courier {

struct CourierClientOptions {
    CourierConfig config;
    Transport* transport = nullptr;
    TokenStore* token_store = nullptr;  // 空 = 内存实现(测试/无持久化)
    RetryPolicy retry;
    std::function<void(int)> sleep;  // 退避睡眠注入(测试)
};

class CourierClient {
public:
    explicit CourierClient(CourierClientOptions options)
        : config_(options.config),
          external_store_(options.token_store),
          owned_store_(std::make_unique<MemoryTokenStore>()),
          session_(store(), &lifecycle_) {
        ApiClientOptions api_options;
        api_options.config = config_;
        api_options.transport = options.transport;
        api_options.retry = options.retry;
        api_options.sleep = std::move(options.sleep);
        api_options.access_token_provider = [this] { return session_.access_token(); };
        api_options.refresh_hook = [this] { return session_.refresh(); };
        api_ = std::make_unique<ApiClient>(std::move(api_options));
        session_.attach_api(api_.get());
        identity_ = std::make_unique<IdentityService>(
            api_.get(), [this](const SessionDto& s) { session_.adopt(s); });
    }

    const CourierConfig& config() const { return config_; }
    LifecycleMachine& lifecycle() { return lifecycle_; }
    ApiClient& api() { return *api_; }
    IdentityService& identity() { return *identity_; }
    SessionService& session() { return session_; }

    /// 配置是否有效(gameId/env 必填非空白,契约 scope.md)。
    bool config_valid() const {
        return non_blank(config_.game_id) && non_blank(config_.env);
    }

    /// 初始化(Uninitialized→Initializing→Ready):校验配置后推进状态机。
    /// 返回是否成功(配置无效/重复 init → false,状态不变)。
    bool init() {
        if (!config_valid()) {
            return false;
        }
        if (!lifecycle_.fire(LifecycleTrigger::kInitStarted)) {
            return false;  // 重复 init:已过 Uninitialized
        }
        return lifecycle_.fire(LifecycleTrigger::kInitCompleted);
    }

    /// 游客登录完整流(Ready/SignedOut → Authenticating → PlayerReady)。
    RequestOutcome guest_login(const GuestRequest& request) {
        return auth_flow([this, &request] { return identity_->guest(request); });
    }

    /// 邮箱+密码登录完整流。
    RequestOutcome login(const CredentialsRequest& request) {
        return auth_flow([this, &request] { return identity_->login(request); });
    }

    /// 邮箱+密码注册并登录完整流。
    RequestOutcome register_account(const CredentialsRequest& request) {
        return auth_flow([this, &request] { return identity_->register_account(request); });
    }

private:
    TokenStore* store() { return external_store_ != nullptr ? external_store_ : owned_store_.get(); }

    /// 非空白判定(空白字符均 < 0x21,无需转义序列)。
    static bool non_blank(const std::string& s) {
        for (const char c : s) {
            if (c > ' ') {
                return true;
            }
        }
        return false;
    }

    // 认证状态流(unity LoginAsync 同构):守卫 Ready/SignedOut 才可发起;
    // AuthFailed 只在仍处 Authenticating 时回退(成功后失败不存在)。
    RequestOutcome auth_flow(const std::function<RequestOutcome()>& send) {
        const LifecycleState state = lifecycle_.current();
        if (state != LifecycleState::kReady && state != LifecycleState::kSignedOut) {
            RequestOutcome out;
            out.error = CourierApiError::make("COMMON_INVALID_ARGUMENT",
                std::string("login requires state Ready/SignedOut, current: ") +
                    lifecycle_state_name(state),
                400, false);
            return out;
        }
        lifecycle_.fire(LifecycleTrigger::kAuthStarted);
        RequestOutcome out = send();
        if (out.ok) {
            lifecycle_.fire(LifecycleTrigger::kAuthSucceeded);
            lifecycle_.fire(LifecycleTrigger::kEnterPlayerReady);
            return out;
        }
        if (lifecycle_.current() == LifecycleState::kAuthenticating) {
            lifecycle_.fire(LifecycleTrigger::kAuthFailed);
        }
        return out;
    }

    CourierConfig config_;
    TokenStore* external_store_;
    std::unique_ptr<MemoryTokenStore> owned_store_;
    LifecycleMachine lifecycle_;
    std::unique_ptr<ApiClient> api_;
    SessionService session_;
    std::unique_ptr<IdentityService> identity_;
};

}  // namespace courier

#endif  // COURIER_CORE_COURIER_CLIENT_HPP
