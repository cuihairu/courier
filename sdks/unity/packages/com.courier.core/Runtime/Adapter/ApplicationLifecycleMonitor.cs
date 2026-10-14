// L3 Adapter:前后台信号 → 状态机事件(layers.md L3「生命周期」)。
// architecture.md:Suspended⇄Resuming,resume 恢复到挂起前状态(转移表归 L2)。
// 移动端 OnApplicationPause 与桌面端 OnApplicationFocus 会成对触发,归一化与幂等
// 去重在引擎无关的 LifecycleSignalRelay(本类只转发引擎回调),故其逻辑可纯 dotnet 单测。
using Courier.Core;
using UnityEngine;

namespace Courier.Adapter
{
    /// <summary>挂到场景常驻物体(建议 DontDestroyOnLoad);Bind 传入客户端状态机。</summary>
    public sealed class ApplicationLifecycleMonitor : MonoBehaviour
    {
        readonly LifecycleSignalRelay _relay = new LifecycleSignalRelay();

        public void Bind(LifecycleMachine lifecycle)
        {
            _relay.Bind(lifecycle);
        }

        void OnApplicationPause(bool paused)
        {
            if (paused)
            {
                _relay.Suspend();
            }
            else
            {
                _relay.Resume();
            }
        }

        void OnApplicationFocus(bool focused)
        {
            if (focused)
            {
                _relay.Resume();
            }
            else
            {
                _relay.Suspend();
            }
        }
    }
}