// L3 Adapter:前后台信号 → 状态机事件(layers.md L3「生命周期」)。
// architecture.md:Suspended⇄Resuming,resume 恢复到挂起前状态(转移表归 L2)。
// 移动端 OnApplicationPause 与桌面端 OnApplicationFocus 会成对触发,
// 此处幂等去重;非法转移走 TryFire(不抛,平台消息处理器不许崩)。
using Courier.Core;
using UnityEngine;

namespace Courier.Adapter
{
    /// <summary>挂到场景常驻物体(建议 DontDestroyOnLoad);Bind 传入客户端状态机。</summary>
    public sealed class ApplicationLifecycleMonitor : MonoBehaviour
    {
        LifecycleMachine _lifecycle;
        bool _suspended;

        public void Bind(LifecycleMachine lifecycle)
        {
            _lifecycle = lifecycle;
        }

        void OnApplicationPause(bool paused)
        {
            if (paused)
            {
                Suspend();
            }
            else
            {
                Resume();
            }
        }

        void OnApplicationFocus(bool focused)
        {
            if (focused)
            {
                Resume();
            }
            else
            {
                Suspend();
            }
        }

        void Suspend()
        {
            if (_suspended || _lifecycle == null)
            {
                return;
            }
            if (_lifecycle.TryFire(LifecycleTrigger.Suspended))
            {
                _suspended = true;
            }
        }

        void Resume()
        {
            if (!_suspended || _lifecycle == null)
            {
                return;
            }
            if (_lifecycle.TryFire(LifecycleTrigger.ResumeStarted) &&
                _lifecycle.TryFire(LifecycleTrigger.ResumeCompleted))
            {
                _suspended = false;
            }
        }
    }
}
