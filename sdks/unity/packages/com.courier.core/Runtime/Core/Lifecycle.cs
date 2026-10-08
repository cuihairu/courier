// L2 Core:生命周期状态机(architecture.md「SDK 生命周期状态机」;事件面契约 events.md Frozen v1)。
// 状态机平台无关:平台差异(前后台/进程信号)由 L3 Adapter 注入 Trigger。
//
//   Uninitialized → Initializing → Ready → Authenticating → Authenticated → PlayerReady
//                                    ▲                        │              │
//                                    │        ┌───────────────┘              │
//                                    │        ▼                              ▼
//                                    └── SignedOut                Suspended ⇄ Resuming
using System;
using System.Collections.Generic;

namespace Courier.Core
{
    public enum LifecycleState
    {
        Uninitialized,
        Initializing,
        Ready,
        Authenticating,
        Authenticated,
        PlayerReady,
        Suspended,
        Resuming,
        SignedOut
    }

    /// <summary>状态机触发器(平台信号与业务动作的统一入口)。</summary>
    public enum LifecycleTrigger
    {
        InitStarted,
        InitCompleted,
        AuthStarted,
        AuthSucceeded,
        AuthFailed,
        EnterPlayerReady,
        SignedOut,
        Suspended,
        ResumeStarted,
        ResumeCompleted
    }

    /// <summary>对外生命周期事件(events.md「生命周期事件」六个 type 之五;
    /// token_expired 由 SessionService 在自动 refresh 开始时发出)。</summary>
    public sealed class LifecycleEvent
    {
        public string Type { get; }
        public LifecycleState From { get; }
        public LifecycleState To { get; }

        public LifecycleEvent(string type, LifecycleState from, LifecycleState to)
        {
            Type = type;
            From = from;
            To = to;
        }
    }

    /// <summary>生命周期状态机:非法触发抛 InvalidOperationException(严格表,全事件单测覆盖)。</summary>
    public sealed class LifecycleMachine
    {
        static readonly Dictionary<long, LifecycleState> Transitions = BuildTransitions();

        LifecycleState _state = LifecycleState.Uninitialized;
        LifecycleState _preSuspend;  // Suspended 前的稳定态,ResumeCompleted 时回到该态
        string _lastAccountId;       // 切号检测(合同 events.md lifecycle.account_switched)

        public LifecycleState State { get { return _state; } }

        /// <summary>契约生命周期事件(lifecycle.*)。SessionService 追加 token_expired。</summary>
        public event Action<LifecycleEvent> EventRaised;

        static Dictionary<long, LifecycleState> BuildTransitions()
        {
            var t = new Dictionary<long, LifecycleState>();
            Action<LifecycleState, LifecycleTrigger, LifecycleState> add = (s, trg, to) =>
                t[(long)s * 100 + (long)trg] = to;
            add(LifecycleState.Uninitialized, LifecycleTrigger.InitStarted, LifecycleState.Initializing);
            add(LifecycleState.Initializing, LifecycleTrigger.InitCompleted, LifecycleState.Ready);
            add(LifecycleState.Ready, LifecycleTrigger.AuthStarted, LifecycleState.Authenticating);
            add(LifecycleState.Authenticating, LifecycleTrigger.AuthSucceeded, LifecycleState.Authenticated);
            add(LifecycleState.Authenticating, LifecycleTrigger.AuthFailed, LifecycleState.Ready);
            add(LifecycleState.Authenticated, LifecycleTrigger.EnterPlayerReady, LifecycleState.PlayerReady);
            add(LifecycleState.Authenticated, LifecycleTrigger.SignedOut, LifecycleState.SignedOut);
            add(LifecycleState.Authenticated, LifecycleTrigger.Suspended, LifecycleState.Suspended);
            add(LifecycleState.PlayerReady, LifecycleTrigger.SignedOut, LifecycleState.SignedOut);
            add(LifecycleState.PlayerReady, LifecycleTrigger.Suspended, LifecycleState.Suspended);
            add(LifecycleState.Suspended, LifecycleTrigger.ResumeStarted, LifecycleState.Resuming);
            // Resuming → 挂起前的稳定态:在 Trigger 里按 _preSuspend 分派
            add(LifecycleState.SignedOut, LifecycleTrigger.AuthStarted, LifecycleState.Authenticating);
            return t;
        }

        /// <summary>推进状态机;成功时发出对应契约事件。非法转移抛异常。</summary>
        public LifecycleState Fire(LifecycleTrigger trigger)
        {
            if (!TryFire(trigger))
            {
                throw new InvalidOperationException(
                    "invalid lifecycle transition: " + _state + " --" + trigger + "--> ?");
            }
            return _state;
        }

        /// <summary>非抛版:非法转移返回 false 且状态不变(Adapter 平台信号幂等用)。</summary>
        public bool TryFire(LifecycleTrigger trigger)
        {
            if (_state == LifecycleState.Resuming && trigger == LifecycleTrigger.ResumeCompleted)
            {
                Transition(trigger, _preSuspend, EventOf(trigger, _preSuspend));
                return true;
            }

            long key = (long)_state * 100 + (long)trigger;
            LifecycleState target;
            if (!Transitions.TryGetValue(key, out target))
            {
                return false;
            }
            Transition(trigger, target, EventOf(trigger, target));
            return true;
        }

        LifecycleState Transition(LifecycleTrigger trigger, LifecycleState target, string eventType)
        {
            if (target == LifecycleState.Suspended)
            {
                _preSuspend = _state;
            }
            var from = _state;
            _state = target;
            if (eventType != null && EventRaised != null)
            {
                EventRaised(new LifecycleEvent(eventType, from, target));
            }
            return _state;
        }

        /// <summary>状态机触发 → 契约事件 type 映射;非契约触发返回 null(不对外发)。</summary>
        static string EventOf(LifecycleTrigger trigger, LifecycleState to)
        {
            switch (trigger)
            {
                case LifecycleTrigger.InitCompleted:
                    return "lifecycle.initialized";      // Init 完成,进入 Ready
                case LifecycleTrigger.SignedOut:
                    return "lifecycle.signed_out";       // 登出/被踢(未认证)
                case LifecycleTrigger.Suspended:
                    return "lifecycle.suspended";        // 切后台/断网
                case LifecycleTrigger.ResumeCompleted:
                    return "lifecycle.resumed";          // 恢复,重新可用
                default:
                    return null;
            }
        }

        /// <summary>切号检测:新账号与上次不同 → account_switched(SessionService 调用)。</summary>
        public bool ReportAccountId(string accountId)
        {
            var switched = _lastAccountId != null && accountId != null && _lastAccountId != accountId;
            _lastAccountId = accountId ?? _lastAccountId;
            if (switched && EventRaised != null)
            {
                EventRaised(new LifecycleEvent("lifecycle.account_switched", _state, _state));
            }
            return switched;
        }

        /// <summary>access 过期,自动 refresh 开始(SessionService 调用)。</summary>
        public void RaiseTokenExpired()
        {
            if (EventRaised != null)
            {
                EventRaised(new LifecycleEvent("lifecycle.token_expired", _state, _state));
            }
        }
    }
}
