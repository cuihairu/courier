// 批次 4 验收:生命周期状态机全事件单测(全合法迁移表 + 非法迁移 + 契约事件面)。
using System;
using System.Collections.Generic;
using Courier.Core;
using Xunit;

namespace Courier.CoreTests
{
    public class LifecycleMachineTests
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
        }

        static LifecycleMachine MachineAt(string state)
        {
            var m = new LifecycleMachine();
            if (state == "Uninitialized") return m;
            m.Fire(LifecycleTrigger.InitStarted);
            if (state == "Initializing") return m;
            m.Fire(LifecycleTrigger.InitCompleted);
            if (state == "Ready") return m;
            m.Fire(LifecycleTrigger.AuthStarted);
            if (state == "Authenticating") return m;
            m.Fire(LifecycleTrigger.AuthSucceeded);
            if (state == "Authenticated") return m;
            m.Fire(LifecycleTrigger.EnterPlayerReady);
            if (state == "PlayerReady") return m;
            if (state == "SignedOut")
            {
                m.Fire(LifecycleTrigger.SignedOut);  // 直接从 PlayerReady 登出
                return m;
            }
            m.Fire(LifecycleTrigger.Suspended);
            if (state == "Suspended") return m;
            m.Fire(LifecycleTrigger.ResumeStarted);
            if (state == "Resuming") return m;
            throw new ArgumentException("未知构造态: " + state);
        }

        public static IEnumerable<object[]> FullTransitionTable()
        {
            // (起始态, 触发, 期望态)——architecture.md 状态图全表。
            yield return new object[] { "Uninitialized", LifecycleTrigger.InitStarted, "Initializing" };
            yield return new object[] { "Initializing", LifecycleTrigger.InitCompleted, "Ready" };
            yield return new object[] { "Ready", LifecycleTrigger.AuthStarted, "Authenticating" };
            yield return new object[] { "Authenticating", LifecycleTrigger.AuthSucceeded, "Authenticated" };
            yield return new object[] { "Authenticating", LifecycleTrigger.AuthFailed, "Ready" };
            yield return new object[] { "Authenticated", LifecycleTrigger.EnterPlayerReady, "PlayerReady" };
            yield return new object[] { "Authenticated", LifecycleTrigger.SignedOut, "SignedOut" };
            yield return new object[] { "Authenticated", LifecycleTrigger.Suspended, "Suspended" };
            yield return new object[] { "PlayerReady", LifecycleTrigger.SignedOut, "SignedOut" };
            yield return new object[] { "PlayerReady", LifecycleTrigger.Suspended, "Suspended" };
            yield return new object[] { "Suspended", LifecycleTrigger.ResumeStarted, "Resuming" };
            yield return new object[] { "Resuming", LifecycleTrigger.ResumeCompleted, "PlayerReady" };
            yield return new object[] { "SignedOut", LifecycleTrigger.AuthStarted, "Authenticating" };
        }

        [Theory]
        [MemberData(nameof(FullTransitionTable))]
        public void FullTable_ValidTransitions(string from, LifecycleTrigger trigger, string to)
        {
            var m = MachineAt(from);
            m.Fire(trigger);
            Assert.Equal(to, m.State.ToString());
        }

        [Fact]
        public void Resume_ReturnsToPreSuspendState()
        {
            // 从 Authenticated 挂起 → 恢复回 Authenticated(不是 PlayerReady)。
            var m = MachineAt("Authenticated");
            m.Fire(LifecycleTrigger.Suspended);
            m.Fire(LifecycleTrigger.ResumeStarted);
            m.Fire(LifecycleTrigger.ResumeCompleted);
            Assert.Equal(LifecycleState.Authenticated, m.State);
        }

        [Theory]
        [InlineData("Ready", LifecycleTrigger.Suspended)]        // 未认证无挂起语义
        [InlineData("Uninitialized", LifecycleTrigger.AuthStarted)]
        [InlineData("Uninitialized", LifecycleTrigger.InitCompleted)]
        [InlineData("Ready", LifecycleTrigger.EnterPlayerReady)] // 必须先认证
        [InlineData("PlayerReady", LifecycleTrigger.AuthStarted)]
        [InlineData("PlayerReady", LifecycleTrigger.InitCompleted)]
        [InlineData("SignedOut", LifecycleTrigger.Suspended)]
        [InlineData("Authenticating", LifecycleTrigger.SignedOut)]
        public void InvalidTransition_Throws(string from, LifecycleTrigger trigger)
        {
            var m = MachineAt(from);
            Assert.Throws<InvalidOperationException>(() => m.Fire(trigger));
            Assert.Equal(from, m.State.ToString());  // 状态不被非法触发破坏
        }

        [Fact]
        public void ContractEvents_EmittedForInitSuspendResumeSignOut()
        {
            var m = MachineAt("Uninitialized");
            var rec = new Recorder(m);
            m.Fire(LifecycleTrigger.InitStarted);
            m.Fire(LifecycleTrigger.InitCompleted);
            m.Fire(LifecycleTrigger.AuthStarted);
            m.Fire(LifecycleTrigger.AuthSucceeded);
            m.Fire(LifecycleTrigger.EnterPlayerReady);
            m.Fire(LifecycleTrigger.Suspended);
            m.Fire(LifecycleTrigger.ResumeStarted);
            m.Fire(LifecycleTrigger.ResumeCompleted);
            m.Fire(LifecycleTrigger.SignedOut);
            // 契约 events.md:AuthStarted/Succeeded/EnterPlayerReady 不是契约生命周期事件。
            Assert.Equal(new[]
            {
                "lifecycle.initialized", "lifecycle.suspended", "lifecycle.resumed", "lifecycle.signed_out"
            }, rec.Types);
        }

        [Fact]
        public void TokenExpired_EventWithoutStateChange()
        {
            var m = MachineAt("PlayerReady");
            var rec = new Recorder(m);
            m.RaiseTokenExpired();
            Assert.Equal(new[] { "lifecycle.token_expired" }, rec.Types);
            Assert.Equal(LifecycleState.PlayerReady, m.State);
        }

        [Fact]
        public void AccountSwitched_FirstLoginNoEvent_ThenSwitchDetected()
        {
            var m = MachineAt("Uninitialized");
            var rec = new Recorder(m);
            Assert.False(m.ReportAccountId("acc_A"));   // 首次登录不算切换
            Assert.Empty(rec.Types);
            Assert.True(m.ReportAccountId("acc_B"));    // 切号
            Assert.Equal(new[] { "lifecycle.account_switched" }, rec.Types);
            Assert.False(m.ReportAccountId("acc_B"));   // 同号重登不算切换
        }

        // --- TryFire(批次 5:Adapter 平台信号幂等,非法转移不抛) ---

        [Fact]
        public void TryFire_ValidTransition_MatchesFire()
        {
            var m = MachineAt("PlayerReady");
            Assert.True(m.TryFire(LifecycleTrigger.Suspended));
            Assert.Equal(LifecycleState.Suspended, m.State);
            Assert.True(m.TryFire(LifecycleTrigger.ResumeStarted));
            Assert.True(m.TryFire(LifecycleTrigger.ResumeCompleted));
            Assert.Equal(LifecycleState.PlayerReady, m.State);   // 回挂起前状态
        }

        [Fact]
        public void TryFire_InvalidTransition_ReturnsFalse_StateUnchanged_NoEvent()
        {
            var m = MachineAt("PlayerReady");
            var rec = new Recorder(m);
            Assert.False(m.TryFire(LifecycleTrigger.AuthStarted));   // PlayerReady 不可再登录
            Assert.False(m.TryFire(LifecycleTrigger.ResumeStarted)); // 未挂起不可恢复
            Assert.Equal(LifecycleState.PlayerReady, m.State);
            Assert.Empty(rec.Types);
        }

        [Fact]
        public void Fire_InvalidTransition_StillThrows_AfterTryFireRefactor()
        {
            var m = MachineAt("PlayerReady");
            Assert.Throws<InvalidOperationException>(() => m.Fire(LifecycleTrigger.AuthStarted));
            Assert.Equal(LifecycleState.PlayerReady, m.State);
        }
    }
}
