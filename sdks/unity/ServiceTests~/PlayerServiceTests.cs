// M3 Player 域服务测试:四端点 wire/解析、Bearer 头、PATCH 只发改动字段、
// 绑定幂等响应解析、501 → null、400 参数错 typed 直抛。
using System.Linq;
using System.Threading;
using System.Threading.Tasks;
using Courier;
using Courier.Core;
using Courier.Identity;
using Courier.Service;
using Courier.CoreTests;
using Xunit;

namespace Courier.ServiceTests
{
    public class PlayerServiceTests
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
        public async Task Profile_Get_WireAndParse_LazyDefault()
        {
            var (svc, transport) = NewServices();
            transport.EnqueueJson(200, Fixtures.Success(
                "{\"displayName\":\"Player\",\"createdAt\":\"2026-10-09T12:00:00.000Z\"," +
                "\"updatedAt\":\"2026-10-09T12:00:00.000Z\"}"));

            var p = await svc.Player.GetProfileAsync(CancellationToken.None);

            var req = transport.Requests.Last();
            Assert.Equal("GET", req.Method);
            Assert.Equal("https://api.example.com/v1/player/profile", req.Url);
            Assert.Equal("Bearer access-1", req.Headers["Authorization"]); // 契约:本域全 Bearer
            Assert.Equal("Player", p.DisplayName);
            Assert.Null(p.AvatarUrl); // 可选字段缺省 = null
            Assert.Equal("2026-10-09T12:00:00.000Z", p.CreatedAt);
        }

        [Fact]
        public async Task Profile_Update_SendsOnlyProvidedFields_ResponseAuthoritative()
        {
            var (svc, transport) = NewServices();
            transport.EnqueueJson(200, Fixtures.Success(
                "{\"displayName\":\"新名字\",\"avatarUrl\":\"https://cdn.example/a.png\"," +
                "\"createdAt\":\"2026-10-09T12:00:00.000Z\"," +
                "\"updatedAt\":\"2026-10-09T12:01:00.000Z\"}"));

            var p = await svc.Player.UpdateProfileAsync("  新名字  ", null, CancellationToken.None);

            var req = transport.Requests.Last();
            Assert.Equal("PATCH", req.Method);
            // null 字段不下发:只改提供的字段(契约「修改档案」)
            Assert.Equal("{\"displayName\":\"  新名字  \"}", req.JsonBody);
            // 响应体为准:展示名是服务端修剪后的值,不是请求原值
            Assert.Equal("新名字", p.DisplayName);
            Assert.Equal("https://cdn.example/a.png", p.AvatarUrl);
            Assert.Equal("2026-10-09T12:01:00.000Z", p.UpdatedAt);
        }

        [Fact]
        public async Task Characters_BindAndList_WireAndParse()
        {
            var (svc, transport) = NewServices();
            transport.EnqueueJson(200, Fixtures.Success(
                "{\"playerId\":\"char_001\",\"boundAt\":\"2026-10-09T12:02:00.000Z\"}"));

            var bound = await svc.Player.BindCharacterAsync("char_001", CancellationToken.None);

            var req = transport.Requests.Last();
            Assert.Equal("POST", req.Method);
            Assert.Equal("https://api.example.com/v1/player/characters", req.Url);
            Assert.Equal("{\"playerId\":\"char_001\"}", req.JsonBody);
            Assert.Equal("char_001", bound.PlayerId);
            Assert.Equal("2026-10-09T12:02:00.000Z", bound.BoundAt);

            transport.EnqueueJson(200, Fixtures.Success(
                "{\"items\":[{\"playerId\":\"char_001\",\"boundAt\":\"2026-10-09T12:02:00.000Z\"}," +
                "{\"playerId\":\"char_002\",\"boundAt\":\"2026-10-09T12:03:00.000Z\"}]," +
                "\"nextCursor\":\"\"}"));
            var page = await svc.Player.ListCharactersAsync(CancellationToken.None);

            Assert.Equal("https://api.example.com/v1/player/characters",
                transport.Requests.Last().Url);
            Assert.Equal(2, page.Items.Count);        // boundAt 升序(服务端保证)
            Assert.Equal("char_002", page.Items[1].PlayerId);
            Assert.Equal("", page.NextCursor);        // v1 单页,cursor 恒空
        }

        [Fact]
        public async Task Disabled_AllEndpointsNull()
        {
            var (svc, transport) = NewServices();
            for (int i = 0; i < 4; i++)
            {
                transport.EnqueueJson(501, Fixtures.Failure(
                    "COMMON_CAPABILITY_DISABLED", "capability 'player' not configured", false));
            }

            // 契约:能力未接 → null,调用方隐藏档案 UI,不进报错路径
            Assert.Null(await svc.Player.GetProfileAsync(CancellationToken.None));
            Assert.Null(await svc.Player.UpdateProfileAsync("名", null, CancellationToken.None));
            Assert.Null(await svc.Player.ListCharactersAsync(CancellationToken.None));
            Assert.Null(await svc.Player.BindCharacterAsync("c1", CancellationToken.None));
        }

        [Fact]
        public async Task Validation400_TypedErrorThrown_NoRetry()
        {
            var (svc, transport) = NewServices();
            transport.EnqueueJson(400, Fixtures.Failure(
                "COMMON_INVALID_ARGUMENT", "displayName 须为 1-30 字符(修剪首尾后)", false));

            // 参数错照常抛给调用方(trim 后为空/超长是接入方可见的输入错误)
            var ex = await Assert.ThrowsAsync<CourierException>(
                () => svc.Player.UpdateProfileAsync("   ", null, CancellationToken.None));
            Assert.Equal("COMMON_INVALID_ARGUMENT", ex.Error.WireCode);
            Assert.Single(transport.Requests.Skip(1)); // retryable=false,无重试
        }
    }
}
