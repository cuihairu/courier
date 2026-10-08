// M3 App/Config 域服务测试:config 投影解析/缓存/重拉判据/501 空结果;
// app 三端点匿名请求与可选字段解析;501 → null;503/426 typed 直抛。
using System;
using System.Linq;
using System.Threading;
using System.Threading.Tasks;
using Courier;
using Courier.Core;
using Courier.Identity;
using Courier.Service;
using Courier.CoreTests;
using Newtonsoft.Json.Linq;
using Xunit;

namespace Courier.ServiceTests
{
    public class AppServiceTests
    {
        static (CourierServices, FakeTransport) NewServices()
        {
            var transport = new FakeTransport();
            var client = new CourierClient(Fixtures.Config(), transport, new FakeTokenStore(),
                new FixedClock(Fixtures.BaseTime));
            transport.EnqueueJson(200, Fixtures.Success(Fixtures.SessionJson()));
            client.InitAsync(CancellationToken.None).Wait();
            client.GuestAsync(new GuestRequest { DeviceId = "device-1", Platform = "ios" },
                CancellationToken.None).Wait();
            return (new CourierServices(client), transport);
        }

        [Fact]
        public async Task Config_Fetch_WireAndParse_ItemsAreRawJson()
        {
            var (svc, transport) = NewServices();
            transport.EnqueueJson(200, Fixtures.Success(
                "{\"configVersion\":7,\"items\":{\"shop_switch\":true,\"pvp_ratio\":1.5," +
                "\"banner\":{\"img\":\"summer.png\",\"jump\":\"activity_01\"}}}"));

            var snap = await svc.Config.FetchAsync("ios", "2.0.0", "cn", CancellationToken.None);

            var req = transport.Requests.Last();
            Assert.Equal("GET", req.Method);
            Assert.Equal("https://api.example.com/v1/app/config?platform=ios&appVersion=2.0.0&region=cn",
                req.Url);
            Assert.Equal("Bearer access-1", req.Headers["Authorization"]); // 契约:config 要 Bearer
            Assert.Equal(7, snap.ConfigVersion);
            Assert.Same(snap, svc.Config.Cached); // 拉取成功即缓存

            Assert.True(snap.Items["shop_switch"].ToObject<bool>());   // 原始 JSON 值
            Assert.Equal(1.5, snap.Items["pvp_ratio"].ToObject<double>());
            Assert.Equal("summer.png", snap.Items["banner"]["img"].ToObject<string>());

            Assert.True(svc.Config.TryGet<bool>("shop_switch", out var flag));
            Assert.True(flag);
            // 键是扁平的([a-zA-Z0-9_.-] 是键名字符集,不是路径):banner 整体是对象值
            Assert.True(svc.Config.TryGet<JObject>("banner", out var banner));
            Assert.Equal("summer.png", (string)banner["img"]);
            // 对象值当 bool 取 = 使用方错误(抛,不静默给 false)
            Assert.Throws<ArgumentException>(() => svc.Config.TryGet<bool>("banner", out _));
        }

        [Fact]
        public async Task Config_Fetch_OmitOptionalDimensions()
        {
            var (svc, transport) = NewServices();
            transport.EnqueueJson(200, Fixtures.Success(
                "{\"configVersion\":1,\"items\":{}}"));

            await svc.Config.FetchAsync(null, "", null, CancellationToken.None);

            // 全缺省:无 query(缺省维度不参与过滤是服务端语义,客户端只是不带)。
            Assert.Equal("https://api.example.com/v1/app/config", transport.Requests.Last().Url);
            Assert.Empty(svc.Config.Cached.Items);
        }

        [Fact]
        public async Task Config_Disabled_ReturnsEmpty_NotError()
        {
            var (svc, transport) = NewServices();
            transport.EnqueueJson(501, Fixtures.Failure(
                "COMMON_CAPABILITY_DISABLED", "capability 'app' not configured", false));

            var snap = await svc.Config.FetchAsync("ios", null, null, CancellationToken.None);

            Assert.NotNull(snap); // 契约:空结果,不进报错路径
            Assert.Equal(0, snap.ConfigVersion);
            Assert.Empty(snap.Items);
            Assert.Null(svc.Config.Cached); // 501 不写缓存
        }

        [Fact]
        public async Task Config_NeedsRefetch_ByEventVersion()
        {
            var (svc, transport) = NewServices();
            Assert.True(svc.Config.NeedsRefetch(1)); // 从未拉到 → 重拉

            transport.EnqueueJson(200, Fixtures.Success(
                "{\"configVersion\":7,\"items\":{\"k\":\"v\"}}"));
            await svc.Config.FetchAsync(null, null, null, CancellationToken.None);

            Assert.False(svc.Config.NeedsRefetch(7)); // 事件 version 相同 → 忽略(不重渲染)
            Assert.True(svc.Config.NeedsRefetch(8));  // 不同 → 重拉热生效
        }

        [Fact]
        public async Task Config_TryGet_MismatchIsCallerError()
        {
            var (svc, transport) = NewServices();
            transport.EnqueueJson(200, Fixtures.Success(
                "{\"configVersion\":1,\"items\":{\"name\":\"abc\"}}"));
            await svc.Config.FetchAsync(null, null, null, CancellationToken.None);

            Assert.False(svc.Config.TryGet<int>("missing", out var absent));
            Assert.Equal(0, absent);
            // 值解析失败是使用方错误:SDK 按原始 JSON 返回,不做二次校验
            Assert.Throws<FormatException>(
                () => svc.Config.TryGet<int>("name", out _));
        }

        [Fact]
        public async Task App_CheckUpdate_AnonymousAndForceFlag()
        {
            var (svc, transport) = NewServices();
            transport.EnqueueJson(200, Fixtures.Success(
                "{\"latestVersion\":\"2.1.0\",\"minVersion\":\"2.0.0\"," +
                "\"updateUrl\":\"https://example.com/dl\",\"forceUpdate\":true}"));

            var v = await svc.App.CheckUpdateAsync("1.5.0", "ios", CancellationToken.None);

            var req = transport.Requests.Last();
            Assert.Equal("https://api.example.com/v1/app/version?appVersion=1.5.0&platform=ios",
                req.Url);
            Assert.False(req.Headers.ContainsKey("Authorization")); // 匿名可(维护中仍需可查)
            Assert.True(v.ForceUpdate);
            Assert.Equal("2.1.0", v.LatestVersion);
            Assert.Equal("https://example.com/dl", v.UpdateUrl);
        }

        [Fact]
        public async Task App_CheckUpdate_NoParams_NoQueryNoForce()
        {
            var (svc, transport) = NewServices();
            transport.EnqueueJson(200, Fixtures.Success(
                "{\"latestVersion\":\"2.1.0\",\"minVersion\":\"2.0.0\",\"forceUpdate\":false}"));

            var v = await svc.App.CheckUpdateAsync(null, null, CancellationToken.None);

            Assert.Equal("https://api.example.com/v1/app/version", transport.Requests.Last().Url);
            Assert.False(v.ForceUpdate); // 缺省不判定
            Assert.Null(v.UpdateUrl);   // 可选字段缺省 = null
        }

        [Fact]
        public async Task App_Maintenance_ParseWithOptionals()
        {
            var (svc, transport) = NewServices();
            transport.EnqueueJson(200, Fixtures.Success(
                "{\"inMaintenance\":true,\"estimatedRecoveryAt\":\"2026-10-10T02:00:00.000Z\"," +
                "\"message\":\"升级维护\"}"));

            var m = await svc.App.CheckMaintenanceAsync(CancellationToken.None);

            Assert.Equal("https://api.example.com/v1/app/maintenance",
                transport.Requests.Last().Url);
            Assert.True(m.InMaintenance);
            Assert.Equal("2026-10-10T02:00:00.000Z", m.EstimatedRecoveryAt);
            Assert.Equal("升级维护", m.Message);

            transport.EnqueueJson(200, Fixtures.Success("{\"inMaintenance\":false}"));
            var m2 = await svc.App.CheckMaintenanceAsync(CancellationToken.None);
            Assert.False(m2.InMaintenance);
            Assert.Null(m2.EstimatedRecoveryAt); // 可选字段缺省 = null
            Assert.Null(m2.Message);
        }

        [Fact]
        public async Task App_Environment_Echo()
        {
            var (svc, transport) = NewServices();
            transport.EnqueueJson(200, Fixtures.Success(
                "{\"gameId\":\"game_demo\",\"env\":\"prod\"}"));

            var e = await svc.App.GetEnvironmentAsync(CancellationToken.None);

            Assert.Equal("game_demo", e.GameId);
            Assert.Equal("prod", e.Env);
        }

        [Fact]
        public async Task App_Disabled_AllEndpointsNull()
        {
            var (svc, transport) = NewServices();
            transport.EnqueueJson(501, Fixtures.Failure(
                "COMMON_CAPABILITY_DISABLED", "capability 'app' not configured", false));
            transport.EnqueueJson(501, Fixtures.Failure(
                "COMMON_CAPABILITY_DISABLED", "capability 'app' not configured", false));
            transport.EnqueueJson(501, Fixtures.Failure(
                "COMMON_CAPABILITY_DISABLED", "capability 'app' not configured", false));

            // 契约:能力未接 → null,跳过版本/维护检查直接登录
            Assert.Null(await svc.App.CheckUpdateAsync("1.0.0", null, CancellationToken.None));
            Assert.Null(await svc.App.CheckMaintenanceAsync(CancellationToken.None));
            Assert.Null(await svc.App.GetEnvironmentAsync(CancellationToken.None));
        }

        [Fact]
        public async Task App_Maintenance503_TypedErrorThrown()
        {
            var (svc, transport) = NewServices();
            transport.EnqueueJson(503, Fixtures.Failure(
                "APP_MAINTENANCE", "server under maintenance", false));

            // 维护 503 是 typed 错误(retryable=false,不重试),照常抛给调用方走维护页
            var ex = await Assert.ThrowsAsync<CourierException>(
                () => svc.App.CheckMaintenanceAsync(CancellationToken.None));
            Assert.Equal("APP_MAINTENANCE", ex.Error.WireCode);
            Assert.Single(transport.Requests.Skip(1)); // 无重试
        }
    }
}
