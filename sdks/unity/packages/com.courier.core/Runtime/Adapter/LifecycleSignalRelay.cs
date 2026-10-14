// L3 Adapter:前后台信号 → 状态机事件的归一化(引擎无关部分)。
// ApplicationLifecycleMonitor(MonoBehaviour)只做引擎回调转发,全部归一化在此件,
// 故纯 dotnet 工程可覆盖(CoreTests~/LifecycleSignalRelayTests.cs);
// 与 godot src/platform/lifecycleMonitor.gd、cocos lifecycleMonitor 同构。
// 语义:移动端 OnApplicationPause 与桌面端 OnApplicationFocus 成对触发 → 幂等去重;
// 恢复成对发 ResumeStarted+ResumeCompleted(转移表归 L2);转移被拒不更新基线,
// 下一条信号重试(隐藏启动不丢挂起);非法转移走 TryFire(不抛,平台消息处理器不许崩);
// 未绑定全 no-op(信号早于 Bind 到达的安全面)。
using Courier.Core;

namespace Courier.Adapter
{
    /// <summary>前后台信号归一化件:去重 + 成对转移;零引擎 API 依赖(packcheck 逐文件验收)。</summary>
    public sealed class LifecycleSignalRelay
    {
        LifecycleMachine _lifecycle;
        bool _suspended;

        /// <summary>去重基线:是否已挂起(转移被拒时保持旧值,以便下一条信号重试)。</summary>
        public bool Suspended
        {
            get { return _suspended; }
        }

        /// <summary>绑定状态机;重绑即重置去重基线(新状态机无旧挂起历史)。</summary>
        public void Bind(LifecycleMachine lifecycle)
        {
            _lifecycle = lifecycle;
            _suspended = false;
        }

        /// <summary>切后台:已挂起或未绑定为 no-op;返回是否真的挂起。</summary>
        public bool Suspend()
        {
            if (_suspended || _lifecycle == null)
            {
                return false;
            }
            if (!_lifecycle.TryFire(LifecycleTrigger.Suspended))
            {
                return false;
            }
            _suspended = true;
            return true;
        }

        /// <summary>回前台:未挂起或未绑定为 no-op;成对发恢复触发,返回是否真的恢复。</summary>
        public bool Resume()
        {
            if (!_suspended || _lifecycle == null)
            {
                return false;
            }
            if (!_lifecycle.TryFire(LifecycleTrigger.ResumeStarted) ||
                !_lifecycle.TryFire(LifecycleTrigger.ResumeCompleted))
            {
                return false;
            }
            _suspended = false;
            return true;
        }
    }
}