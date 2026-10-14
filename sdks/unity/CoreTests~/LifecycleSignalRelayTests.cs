// 批次 81:前后台信号归一化(LifecycleSignalRelay)单测——MonoBehaviour 只做引擎回调转发,
// 幂等去重/成对恢复/被拒不丢挂起/未绑定 no-op 全在此件(引擎无关),故纯 dotnet 可覆盖。
// 用例与 godot tests/test_adapter.gd(cocos lifecycle 同构)逐条对齐,保证跨引擎生命周期语义同结果。
using System;
using System.Collections.Generic;
using Courier.Adapter;
using Courier.Core;
using Xunit;

namespace Courier.CoreTests
{
    public class LifecycleSignalRelayTests
    {
        sealed class Recorder
        {
            public readonly List<LifecycleEvent> Events = new List<LifecycleEvent>();
            public Recorder(LifecycleMachine m)
            {
                m.EventRaised += e => Events.Add(e);
            }
            public List<string> Types
            {
                get { return Events.ConvertAll(e => e.Type); }
            }
            public int CountOf(string type)
            {
                int n = 0;
                foreach (var e in Events)
                {
                    if (e.Type == type) n++;
                }
                return n;
            }
        }

        static LifecycleMachine MachineAt(string state)
        {
            var m = new LifecycleMachine();
            if (state == "Uninitialized") return m;
            m.Fire(LifecycleTrigger.InitStarted);
            m.Fire(LifecycleTrigger.InitCompleted);
            m.Fire(LifecycleTrigger.AuthStarted);
            m.Fire(LifecycleTrigger.AuthSucceeded);
            if (state == "Authenticated") return m;
            m.Fire(LifecycleTrigger.EnterPlayerReady);
            return m;
        }

        [Fact]
        public void SuspendResume_FromPlayerReady_RestoresAndEmitsContractEvents()
        {
            var m = MachineAt("PlayerReady");
            var rec = new Recorder(m);
            var relay = new LifecycleSignalRelay();
            relay.Bind(m);

            Assert.True(relay.Suspend());
            Assert.Equal(LifecycleState.Suspended, m.State);
            Assert.True(relay.Suspended);

            Assert.True(relay.Resume());
            Assert.Equal(LifecycleState.PlayerReady, m.State);  // 恢复到挂起前
            Assert.False(relay.Suspended);
            Assert.Equal(new[] { "lifecycle.suspended", "lifecycle.resumed" }, rec.Types);
        }

        [Fact]
        public void SuspendResume_FromAuthenticated_ReturnsAuthenticated()
        {
            // 挂起前是 Authenticated 就回 Authenticated,不是一律回 PlayerReady。
            var m = MachineAt("Authenticated");
            var relay = new LifecycleSignalRelay();
            relay.Bind(m);

            Assert.True(relay.Suspend());
            Assert.Equal(LifecycleState.Suspended, m.State);
            Assert.True(relay.Resume());
            Assert.Equal(LifecycleState.Authenticated, m.State);
        }

        [Fact]
        public void PairedSignals_AreDeduplicated()
        {
            // 移动端 pause 与桌面端 focus 会成对触发:重复信号不重复进状态机。
            var m = MachineAt("PlayerReady");
            var rec = new Recorder(m);
            var relay = new LifecycleSignalRelay();
            relay.Bind(m);

            Assert.True(relay.Suspend());
            for (int i = 0; i < 4; i++)
            {
                Assert.False(relay.Suspend());  // 首条已挂起,其余为 no-op
            }
            Assert.Equal(LifecycleState.Suspended, m.State);
            Assert.Equal(1, rec.CountOf("lifecycle.suspended"));

            Assert.True(relay.Resume());
            for (int i = 0; i < 4; i++)
            {
                Assert.False(relay.Resume());
            }
            Assert.Equal(LifecycleState.PlayerReady, m.State);
            Assert.Equal(1, rec.CountOf("lifecycle.resumed"));
        }

        [Fact]
        public void Resume_WithoutSuspend_IsNoOp()
        {
            // 启动即回前台(焦点先到):不应触发任何恢复事件。
            var m = MachineAt("PlayerReady");
            var rec = new Recorder(m);
            var relay = new LifecycleSignalRelay();
            relay.Bind(m);

            Assert.False(relay.Resume());
            Assert.Equal(LifecycleState.PlayerReady, m.State);
            Assert.Empty(rec.Events);
        }

        [Fact]
        public void Unbound_IsNoOp_NoThrow()
        {
            // 未 Bind:平台消息处理器不许崩(MonoBehaviour 早于 Bind 收到信号的安全面)。
            var relay = new LifecycleSignalRelay();
            Assert.False(relay.Suspend());
            Assert.False(relay.Resume());
            Assert.False(relay.Suspended);
        }

        [Fact]
        public void RejectedTransition_KeepsBaseline_SuspendAppliesLater()
        {
            // 隐藏启动:状态机未就绪时转移被拒 → 状态不动、不记基线,下一条信号可补挂起。
            var m = MachineAt("Uninitialized");
            var rec = new Recorder(m);
            var relay = new LifecycleSignalRelay();
            relay.Bind(m);

            Assert.False(relay.Suspend());
            Assert.Equal(LifecycleState.Uninitialized, m.State);
            Assert.False(relay.Suspended);
            Assert.Empty(rec.Events);

            m.Fire(LifecycleTrigger.InitStarted);
            m.Fire(LifecycleTrigger.InitCompleted);
            m.Fire(LifecycleTrigger.AuthStarted);
            m.Fire(LifecycleTrigger.AuthSucceeded);
            m.Fire(LifecycleTrigger.EnterPlayerReady);

            Assert.True(relay.Suspend());
            Assert.Equal(LifecycleState.Suspended, m.State);
            Assert.Equal(1, rec.CountOf("lifecycle.suspended"));
        }

        [Fact]
        public void Rebind_ResetsDedupeBaseline()
        {
            // 换绑新状态机:旧机的挂起标记不得让新机永远收不到后台信号(否则新机永不挂起)。
            var oldMachine = MachineAt("PlayerReady");
            var relay = new LifecycleSignalRelay();
            relay.Bind(oldMachine);
            Assert.True(relay.Suspend());

            var fresh = MachineAt("PlayerReady");
            var rec = new Recorder(fresh);
            relay.Bind(fresh);
            Assert.False(relay.Suspended);

            Assert.True(relay.Suspend());
            Assert.Equal(LifecycleState.Suspended, fresh.State);
            Assert.Equal(1, rec.CountOf("lifecycle.suspended"));
        }

        [Fact]
        public void SignedOutMachine_SuspendRejected_NoStuckBaseline()
        {
            // SignedOut 可挂起(SignedOut→Suspended 非法)→ 被拒不挂起;再次尝试仍被拒不崩。
            var m = MachineAt("PlayerReady");
            m.Fire(LifecycleTrigger.SignedOut);
            var relay = new LifecycleSignalRelay();
            relay.Bind(m);

            Assert.False(relay.Suspend());
            Assert.False(relay.Suspend());
            Assert.Equal(LifecycleState.SignedOut, m.State);

            // 重新登录后可正常挂起(基线未残留)。
            m.Fire(LifecycleTrigger.AuthStarted);
            m.Fire(LifecycleTrigger.AuthSucceeded);
            m.Fire(LifecycleTrigger.EnterPlayerReady);
            Assert.True(relay.Suspend());
            Assert.Equal(LifecycleState.Suspended, m.State);
        }
    }
}