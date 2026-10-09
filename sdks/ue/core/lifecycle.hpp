// L2 Core:生命周期状态机(architecture.md「SDK 生命周期状态机」;事件面契约
// events.md Frozen v1)。状态机平台无关:平台差异(前后台/进程信号)由 L3
// Adapter 注入 Trigger(cocos lifecycle.ts / unity Lifecycle.cs 同构)。
//
//   Uninitialized → Initializing → Ready → Authenticating → Authenticated → PlayerReady
//                                    ▲                        │              │
//                                    │        ┌───────────────┘              │
//                                    │        ▼                              ▼
//                                    └── SignedOut                Suspended ⇄ Resuming
//
// UE 无异常口径:fire/try_fire 均返回 bool,非法转移 false 且状态不变
// (业务流用 fire 判返,平台信号用 try_fire 幂等否决)。
#ifndef COURIER_CORE_LIFECYCLE_HPP
#define COURIER_CORE_LIFECYCLE_HPP

#include <cstdint>
#include <functional>
#include <map>
#include <string>
#include <utility>
#include <vector>

namespace courier {

enum class LifecycleState {
    kUninitialized,
    kInitializing,
    kReady,
    kAuthenticating,
    kAuthenticated,
    kPlayerReady,
    kSuspended,
    kResuming,
    kSignedOut,
};

/// 状态机触发器(平台信号与业务动作的统一入口)。
enum class LifecycleTrigger {
    kInitStarted,
    kInitCompleted,
    kAuthStarted,
    kAuthSucceeded,
    kAuthFailed,
    kEnterPlayerReady,
    kSignedOut,
    kSuspended,
    kResumeStarted,
    kResumeCompleted,
};

/// 契约生命周期事件(events.md「生命周期事件」之五;token_expired 由
/// SessionService 在自动 refresh 开始时发出)。
struct LifecycleEvent {
    std::string type;
    LifecycleState from;
    LifecycleState to;
};

inline const char* lifecycle_state_name(LifecycleState s) {
    switch (s) {
        case LifecycleState::kUninitialized: return "Uninitialized";
        case LifecycleState::kInitializing: return "Initializing";
        case LifecycleState::kReady: return "Ready";
        case LifecycleState::kAuthenticating: return "Authenticating";
        case LifecycleState::kAuthenticated: return "Authenticated";
        case LifecycleState::kPlayerReady: return "PlayerReady";
        case LifecycleState::kSuspended: return "Suspended";
        case LifecycleState::kResuming: return "Resuming";
        case LifecycleState::kSignedOut: return "SignedOut";
    }
    return "?";
}

inline const char* lifecycle_trigger_name(LifecycleTrigger t) {
    switch (t) {
        case LifecycleTrigger::kInitStarted: return "InitStarted";
        case LifecycleTrigger::kInitCompleted: return "InitCompleted";
        case LifecycleTrigger::kAuthStarted: return "AuthStarted";
        case LifecycleTrigger::kAuthSucceeded: return "AuthSucceeded";
        case LifecycleTrigger::kAuthFailed: return "AuthFailed";
        case LifecycleTrigger::kEnterPlayerReady: return "EnterPlayerReady";
        case LifecycleTrigger::kSignedOut: return "SignedOut";
        case LifecycleTrigger::kSuspended: return "Suspended";
        case LifecycleTrigger::kResumeStarted: return "ResumeStarted";
        case LifecycleTrigger::kResumeCompleted: return "ResumeCompleted";
    }
    return "?";
}

/// 合法迁移表(architecture.md 状态图全表;表外一律非法)。
/// Resuming → kResumeCompleted 特判:回挂起前稳定态(见 LifecycleMachine)。
inline bool lifecycle_transition(LifecycleState from, LifecycleTrigger trigger,
                                 LifecycleState& to) {
    switch (from) {
        case LifecycleState::kUninitialized:
            if (trigger == LifecycleTrigger::kInitStarted) { to = LifecycleState::kInitializing; return true; }
            return false;
        case LifecycleState::kInitializing:
            if (trigger == LifecycleTrigger::kInitCompleted) { to = LifecycleState::kReady; return true; }
            return false;
        case LifecycleState::kReady:
            if (trigger == LifecycleTrigger::kAuthStarted) { to = LifecycleState::kAuthenticating; return true; }
            return false;
        case LifecycleState::kAuthenticating:
            if (trigger == LifecycleTrigger::kAuthSucceeded) { to = LifecycleState::kAuthenticated; return true; }
            if (trigger == LifecycleTrigger::kAuthFailed) { to = LifecycleState::kReady; return true; }
            return false;
        case LifecycleState::kAuthenticated:
            if (trigger == LifecycleTrigger::kEnterPlayerReady) { to = LifecycleState::kPlayerReady; return true; }
            if (trigger == LifecycleTrigger::kSignedOut) { to = LifecycleState::kSignedOut; return true; }
            if (trigger == LifecycleTrigger::kSuspended) { to = LifecycleState::kSuspended; return true; }
            return false;
        case LifecycleState::kPlayerReady:
            if (trigger == LifecycleTrigger::kSignedOut) { to = LifecycleState::kSignedOut; return true; }
            if (trigger == LifecycleTrigger::kSuspended) { to = LifecycleState::kSuspended; return true; }
            return false;
        case LifecycleState::kSuspended:
            if (trigger == LifecycleTrigger::kResumeStarted) { to = LifecycleState::kResuming; return true; }
            return false;
        case LifecycleState::kResuming:
            return false;  // kResumeCompleted 特判(回挂起前稳定态)
        case LifecycleState::kSignedOut:
            if (trigger == LifecycleTrigger::kAuthStarted) { to = LifecycleState::kAuthenticating; return true; }
            return false;
    }
    return false;
}

/// 状态机触发 → 契约事件 type 映射;非契约触发返回 nullptr(不对外发)。
inline const char* lifecycle_event_of(LifecycleTrigger trigger) {
    switch (trigger) {
        case LifecycleTrigger::kInitCompleted: return "lifecycle.initialized";  // Init 完成,进入 Ready
        case LifecycleTrigger::kSignedOut: return "lifecycle.signed_out";       // 登出/被踢
        case LifecycleTrigger::kSuspended: return "lifecycle.suspended";        // 切后台/断网
        case LifecycleTrigger::kResumeCompleted: return "lifecycle.resumed";    // 恢复,重新可用
        default: return nullptr;  // AuthStarted/Succeeded/Failed/EnterPlayerReady 非契约事件
    }
}

/// 生命周期状态机:严格表;非法转移 false 且状态不变(业务流 fire / 平台信号 try_fire)。
class LifecycleMachine {
public:
    using Listener = std::function<void(const LifecycleEvent&)>;

    LifecycleState current() const { return state_; }

    /// 订阅契约生命周期事件;返回订阅号(remove_listener 退订)。
    int add_listener(Listener listener) {
        const int id = next_listener_id_++;
        listeners_.emplace_back(id, std::move(listener));
        return id;
    }

    void remove_listener(int id) {
        for (std::size_t i = 0; i < listeners_.size(); i++) {
            if (listeners_[i].first == id) {
                listeners_.erase(listeners_.begin() + static_cast<std::ptrdiff_t>(i));
                return;
            }
        }
    }

    /// 推进状态机;成功发出对应契约事件。业务流用(失败 = 编程错误,判返)。
    bool fire(LifecycleTrigger trigger) { return try_fire(trigger); }

    /// 非抛版:非法转移返回 false 且状态不变(Adapter 平台信号幂等用)。
    bool try_fire(LifecycleTrigger trigger) {
        LifecycleState target = state_;
        if (state_ == LifecycleState::kResuming && trigger == LifecycleTrigger::kResumeCompleted) {
            transition(trigger, pre_suspend_);
            return true;
        }
        if (!lifecycle_transition(state_, trigger, target)) {
            return false;
        }
        transition(trigger, target);
        return true;
    }

    /// 切号检测:新账号与上次不同 → account_switched(SessionService 调用)。
    /// 首次登录不算切换;同号重登不算切换。
    bool report_account_id(const std::string& account_id) {
        const bool switched = !last_account_id_.empty() && !account_id.empty() &&
                              last_account_id_ != account_id;
        if (!account_id.empty()) {
            last_account_id_ = account_id;
        }
        if (switched) {
            emit("lifecycle.account_switched");
        }
        return switched;
    }

    /// access 过期,自动 refresh 开始(SessionService 调用);状态不变。
    void raise_token_expired() { emit("lifecycle.token_expired"); }

private:
    void transition(LifecycleTrigger trigger, LifecycleState target) {
        if (target == LifecycleState::kSuspended) {
            pre_suspend_ = state_;
        }
        const LifecycleState from = state_;
        state_ = target;
        if (const char* type = lifecycle_event_of(trigger); type != nullptr) {
            dispatch(LifecycleEvent{type, from, target});
        }
    }

    void emit(const char* type) { dispatch(LifecycleEvent{type, state_, state_}); }

    void dispatch(const LifecycleEvent& event) {
        // 拷贝分发:监听器内增删订阅不影响本轮。
        const std::vector<std::pair<int, Listener>> snapshot = listeners_;
        for (const auto& kv : snapshot) {
            kv.second(event);
        }
    }

    LifecycleState state_ = LifecycleState::kUninitialized;
    LifecycleState pre_suspend_ = LifecycleState::kUninitialized;  // Suspended 前的稳定态
    std::string last_account_id_;                                  // 切号检测
    std::vector<std::pair<int, Listener>> listeners_;
    int next_listener_id_ = 1;
};

}  // namespace courier

#endif  // COURIER_CORE_LIFECYCLE_HPP
