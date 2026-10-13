// 批次 4 验收:与 auth 契约字段 100% 对齐(wire 断言)+ 自动 refresh/吊销/登出/恢复全链路。
// 验收行「游客登录 → 绑定邮箱 → 登出 → refresh 轮换 → 吊销拒绝」在 Core 层以假传输覆盖;
// 服务端真实链路由 gateway e2e 覆盖(批次 3),Unity 侧真机链路留批次 5 Adapter。
using System;
using System.Collections.Generic;
using System.Linq;
using System.Threading;
using System.Threading.Tasks;
using Courier.Core;
using Courier.Identity;
using Xunit;

namespace Courier.CoreTests
{
    public class CourierClientTests
    {
        static (CourierClient, FakeTransport, FakeTokenStore, FixedClock, List<string>) NewClient(
            DateTimeOffset? now = null)
        {
            var transport = new FakeTransport();
            var store = new FakeTokenStore();
            var clock = new FixedClock(now ?? Fixtures.BaseTime);
            var client = new CourierClient(Fixtures.Config(), transport, store, clock);
            var events = new List<string>();
            client.Lifecycle.EventRaised += e => events.Add(e.Type);
            return (client, transport, store, clock, events);
        }

        async Task InitGuestAsync(CourierClient client, FakeTransport transport)
        {
            transport.EnqueueJson(200, Fixtures.Success(Fixtures.SessionJson()));
            await client.InitAsync(CancellationToken.None);
            await client.GuestAsync(new GuestRequest { DeviceId = "device-1", Platform = "ios" },
                CancellationToken.None);
        }

        // --- wire 对齐(验收:与 auth 契约字段 100% 对齐) ---

        [Fact]
        public async Task GuestLogin_WireAlignment_ScopeHeadersMethodBody()
        {
            var (client, transport, _, _, _) = NewClient();
            transport.EnqueueJson(200, Fixtures.Success(Fixtures.SessionJson()));
            await client.InitAsync(CancellationToken.None);
            var dto = await client.GuestAsync(
                new GuestRequest { DeviceId = "device-1", Platform = "ios" }, CancellationToken.None);

            var req = transport.Requests.Single();
            Assert.Equal("POST", req.Method);
            Assert.Equal("https://api.example.com/v1/identity/guest", req.Url);
            Assert.Equal("game_demo", req.Headers[ScopeHeaders.GameId]);   // scope.md:只走 header
            Assert.Equal("prod", req.Headers[ScopeHeaders.Env]);
            Assert.Equal("application/json", req.Headers["Content-Type"]);
            // wire key camelCase,字段逐一对应 auth.md(可选字段缺省不序列化)。
            Assert.Equal("{\"deviceId\":\"device-1\",\"platform\":\"ios\"}", req.JsonBody);

            // 响应解析逐字段对齐会话 DTO。
            Assert.Equal("acc_01J", dto.Account.Id);
            Assert.Equal("GUEST", dto.Account.Type);
            Assert.Equal("ACTIVE", dto.Account.Status);
            Assert.Equal("2026-10-08T00:00:00.000Z", dto.Account.CreatedAt);
            Assert.Equal("access-1", dto.AccessToken);
            Assert.Equal(Fixtures.AccessExpires, dto.AccessExpiresAt);
            Assert.Equal("refresh-1", dto.RefreshToken);
            Assert.Equal(Fixtures.RefreshExpires, dto.RefreshExpiresAt);
            Assert.Equal("device-1", dto.DeviceId);

            // 登录后:token 持久化 + 状态机到 PlayerReady。
            Assert.Equal("access-1", client.Session.Current.AccessToken);
            Assert.Equal(Courier.Core.LifecycleState.PlayerReady, client.Lifecycle.State);
        }

        [Fact]
        public async Task Bind_SendsBearerToken_ParsesAccount()
        {
            var (client, transport, _, _, _) = NewClient();
            await InitGuestAsync(client, transport);
            transport.EnqueueJson(200, Fixtures.Success(
                "{\"id\":\"acc_01J\",\"type\":\"EMAIL\",\"email\":\"a@b.co\",\"status\":\"ACTIVE\"," +
                "\"createdAt\":\"2026-10-08T00:00:00.000Z\"}"));

            var acc = await client.BindAsync(
                new BindRequest { Email = "a@b.co", Password = "password8" }, CancellationToken.None);

            var req = transport.Requests.Last();
            Assert.Equal("https://api.example.com/v1/identity/bind", req.Url);
            Assert.Equal("Bearer access-1", req.Headers["Authorization"]);
            Assert.Equal("{\"email\":\"a@b.co\",\"password\":\"password8\"}", req.JsonBody);
            Assert.Equal("EMAIL", acc.Type);
            Assert.Equal("a@b.co", acc.Email);
        }

        [Fact]
        public async Task Login_Failure_StructuredError_TypedEnum_BackToReady()
        {
            var (client, transport, _, _, _) = NewClient();
            await client.InitAsync(CancellationToken.None);
            transport.EnqueueJson(401, Fixtures.Failure("AUTH_INVALID_CREDENTIALS",
                "invalid email or password", false));

            var ex = await Assert.ThrowsAsync<CourierException>(() => client.LoginAsync(
                new LoginRequest { Email = "a@b.co", Password = "wrong-pass-9", DeviceId = "d" },
                CancellationToken.None));

            Assert.Equal(Contract.ErrorCode.AuthInvalidCredentials, ex.Error.TypedCode);
            Assert.False(ex.Error.Retryable);
            Assert.Equal(32, ex.Error.TraceId.Length);
            Assert.Equal(Courier.Core.LifecycleState.Ready, client.Lifecycle.State);
        }

        [Fact]
        public async Task UnauthenticatedCall_RejectedLocally_NoRequestSent()
        {
            var (client, transport, _, _, _) = NewClient();
            await client.InitAsync(CancellationToken.None);

            var ex = await Assert.ThrowsAsync<CourierException>(
                () => client.Identity.GetSessionAsync(CancellationToken.None));
            Assert.Equal("COMMON_UNAUTHENTICATED", ex.Error.WireCode);
            Assert.Empty(transport.Requests);
        }

        // --- refresh 轮换 / 吊销 / 登出(契约 auth.md Token 模型) ---

        [Fact]
        public async Task AccessExpired_MidCall_AutoRefresh_ThenRetryWithNewToken()
        {
            var (client, transport, store, clock, events) = NewClient();
            await InitGuestAsync(client, transport);

            // access 过期后调 GET /session:第一次 AUTH_TOKEN_EXPIRED → 自动 refresh → 重放成功。
            clock.Advance(TimeSpan.FromMinutes(20));
            transport.EnqueueJson(401, Fixtures.Failure("AUTH_TOKEN_EXPIRED", "session expired", false));
            transport.EnqueueJson(200, Fixtures.Success(Fixtures.SessionJson(
                access: "access-2", refresh: "refresh-2")));
            // 重放的 GET /session。
            transport.EnqueueJson(200, Fixtures.Success(
                "{\"account\":{\"id\":\"acc_01J\",\"type\":\"GUEST\",\"status\":\"ACTIVE\"," +
                "\"createdAt\":\"2026-10-08T00:00:00.000Z\"},\"sessionId\":\"ses_01J\",\"deviceId\":\"device-1\"}"));

            var info = await client.Identity.GetSessionAsync(CancellationToken.None);

            Assert.Equal("access-2", client.Session.Current.AccessToken);
            Assert.Equal("refresh-2", store.Saved.RefreshToken);  // 轮换已持久化
            Assert.Contains("lifecycle.token_expired", events);
            // 第 2 次请求(重放)带新 token;refresh 请求不带 Authorization。
            var refreshReq = transport.Requests.First(r => r.Url.EndsWith("/identity/refresh"));
            Assert.False(refreshReq.Headers.ContainsKey("Authorization"));
            Assert.Equal("{\"refreshToken\":\"refresh-1\"}", refreshReq.JsonBody);
            var retried = transport.Requests.Last();
            Assert.Equal("Bearer access-2", retried.Headers["Authorization"]);
            Assert.NotNull(info);
        }

        [Fact]
        public async Task RefreshReplay_RevokesSession_SignsOut_ClearsStore()
        {
            var (client, transport, store, clock, events) = NewClient();
            await InitGuestAsync(client, transport);

            clock.Advance(TimeSpan.FromMinutes(20));
            // GET /session → TOKEN_EXPIRED(触发 refresh)→ refresh 端点返回 REUSED(安全事件)。
            transport.EnqueueJson(401, Fixtures.Failure("AUTH_TOKEN_EXPIRED", "session expired", false));
            transport.EnqueueJson(401, Fixtures.Failure("AUTH_REFRESH_REUSED",
                "refresh token replay detected; session revoked", false));

            await Assert.ThrowsAsync<CourierException>(
                () => client.Identity.GetSessionAsync(CancellationToken.None));

            Assert.Null(store.Saved);          // 本地清场
            Assert.False(client.Session.IsAuthenticated);
            Assert.Equal(Courier.Core.LifecycleState.SignedOut, client.Lifecycle.State);
            Assert.Contains("lifecycle.token_expired", events);
            Assert.Contains("lifecycle.signed_out", events);
        }

        [Fact]
        public async Task SignOut_RevokesAndClears_EmitsSignedOut()
        {
            var (client, transport, store, _, events) = NewClient();
            await InitGuestAsync(client, transport);
            transport.EnqueueJson(200, Fixtures.Success("{}"));

            await client.SignOutAsync(CancellationToken.None);

            Assert.Null(store.Saved);
            Assert.Equal(Courier.Core.LifecycleState.SignedOut, client.Lifecycle.State);
            Assert.Contains("lifecycle.signed_out", events);
            Assert.Equal("POST", transport.Requests.Last().Method);
            Assert.Equal("https://api.example.com/v1/identity/logout", transport.Requests.Last().Url);
        }

        // --- Init 恢复(ITokenStore 往返) ---

        [Fact]
        public async Task Init_RestoresValidSession_ToPlayerReady_WithoutNetwork()
        {
            var (client, transport, store, _, _) = NewClient();
            store.Save(new StoredTokens
            {
                AccountId = "acc_01J",
                AccessToken = "access-saved",
                AccessExpiresAt = Fixtures.AccessExpires,
                RefreshToken = "refresh-saved",
                RefreshExpiresAt = Fixtures.RefreshExpires
            });

            await client.InitAsync(CancellationToken.None);

            Assert.Empty(transport.Requests);  // access 有效:恢复是被动的
            Assert.Equal("access-saved", client.Session.Current.AccessToken);
            Assert.Equal(Courier.Core.LifecycleState.PlayerReady, client.Lifecycle.State);
        }

        [Fact]
        public async Task Init_ExpiredRefresh_ClearsLocal_StaysReady()
        {
            var (client, transport, store, _, _) = NewClient();
            store.Save(new StoredTokens
            {
                AccessToken = "access-saved",
                AccessExpiresAt = Fixtures.AccessExpires,
                RefreshToken = "refresh-saved",
                RefreshExpiresAt = "2026-10-08T00:00:00.000Z"  // 已过期
            });

            await client.InitAsync(CancellationToken.None);

            Assert.Null(store.Saved);
            Assert.False(client.Session.IsAuthenticated);
            Assert.Equal(Courier.Core.LifecycleState.Ready, client.Lifecycle.State);
        }

        [Fact]
        public async Task Init_ExpiredAccess_ValidRefresh_RotatesThenPlayerReady()
        {
            var (client, transport, store, _, _) = NewClient();
            store.Save(new StoredTokens
            {
                AccountId = "acc_01J",
                AccessToken = "access-saved",
                AccessExpiresAt = "2026-10-08T00:00:00.000Z",  // 已过期
                RefreshToken = "refresh-saved",
                RefreshExpiresAt = Fixtures.RefreshExpires     // 有效
            });
            transport.EnqueueJson(200, Fixtures.Success(Fixtures.SessionJson(
                access: "access-2", refresh: "refresh-2")));

            await client.InitAsync(CancellationToken.None);

            Assert.Equal("access-2", client.Session.Current.AccessToken);
            Assert.Equal(Courier.Core.LifecycleState.PlayerReady, client.Lifecycle.State);
        }

        // --- 阶段 B 跨引擎一致性:refresh 单飞(TS/Godot 同场景断言并发共享一次轮换) ---

        [Fact]
        public async Task ConcurrentRefresh_SingleFlight_SharesOneRotation()
        {
            var (client, transport, _, _, _) = NewClient();
            await InitGuestAsync(client, transport);
            var baseline = transport.Requests.Count;  // guest 登录 1 次

            // 挂起的 refresh 响应:构造真实并发重叠(第一路挂在传输上,第二路此时进入)。
            var pending = transport.EnqueuePending(new TransportResponse
            {
                StatusCode = 200,
                Headers = new Dictionary<string, string>(),
                Body = Fixtures.Success(Fixtures.SessionJson(access: "access-9", refresh: "refresh-9"))
            });
            var first = client.Session.TryRefreshAsync(CancellationToken.None);
            Assert.Equal(baseline + 1, transport.Requests.Count);  // 第一路已发出 refresh 并挂起

            var second = client.Session.TryRefreshAsync(CancellationToken.None);
            Assert.Equal(baseline + 1, transport.Requests.Count);  // 第二路跟随在途轮换,零新请求

            pending.SetResult();  // 放行:轮换完成,两路同享结果
            Assert.True(await first);
            Assert.True(await second);
            Assert.Equal("access-9", client.Session.Current.AccessToken);
            Assert.Equal("refresh-9", client.Session.Current.RefreshToken);
            Assert.Equal(baseline + 1, transport.Requests.Count);  // guest + 一次 refresh(串行两刷是 +2)
        }

        // --- 阶段 B 跨引擎一致性:切号(UE lifecycle_test.cpp / TS lifecycle.test.ts 同场景) ---

        [Fact]
        public async Task LogoutThenLoginAnotherAccount_RaisesAccountSwitched()
        {
            var (client, transport, store, _, events) = NewClient();
            await InitGuestAsync(client, transport);
            var switchCount = events.Count(e => e == "lifecycle.account_switched");

            transport.EnqueueJson(200, "{\"data\":null}");
            await client.SignOutAsync(CancellationToken.None);
            Assert.Null(store.Saved);

            transport.EnqueueJson(200, Fixtures.Success(Fixtures.SessionJson(
                accountId: "acc_02K", deviceId: "device-2")));
            await client.GuestAsync(new GuestRequest { DeviceId = "device-2", Platform = "ios" },
                CancellationToken.None);

            Assert.Equal("acc_02K", client.Session.Current.AccountId);
            Assert.Equal(switchCount + 1,
                events.Count(e => e == "lifecycle.account_switched"));  // 换账号恰好多发一次
        }
    }
}
