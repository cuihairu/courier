// M4 Assistant 域服务测试:命中解析、未命中语义(非错误)、校验 400 直抛、
// 501 → null(隐藏入口)。
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
    public class AssistantServiceTests
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
        public async Task Query_Hit_WireAndParse()
        {
            var (svc, transport) = NewServices();
            transport.EnqueueJson(200, Fixtures.Success(
                "{\"matched\":true,\"answer\":{\"id\":\"faq_abc123\"," +
                "\"question\":\"怎么找回账号\",\"answer\":\"点忘记密码\",\"keywords\":[\"找回\"]}," +
                "\"suggestTransfer\":false}"));

            var r = await svc.Assistant.QueryAsync("账号怎么找回", CancellationToken.None);

            var req = transport.Requests.Last();
            Assert.Equal("POST", req.Method);
            Assert.Equal("https://api.example.com/v1/assistant/query", req.Url);
            Assert.Equal("Bearer access-1", req.Headers["Authorization"]); // 契约:本域 Bearer
            Assert.Equal("{\"text\":\"账号怎么找回\"}", req.JsonBody);
            Assert.True(r.Matched);
            Assert.False(r.SuggestTransfer);
            Assert.Equal("faq_abc123", r.Answer.Id);
            Assert.Equal("怎么找回账号", r.Answer.Question);
            Assert.Equal("找回", r.Answer.Keywords[0]);
        }

        [Fact]
        public async Task Query_Miss_IsNotError()
        {
            var (svc, transport) = NewServices();
            transport.EnqueueJson(200, Fixtures.Success(
                "{\"matched\":false,\"suggestTransfer\":true}"));

            var r = await svc.Assistant.QueryAsync("如何下载游戏", CancellationToken.None);

            Assert.False(r.Matched);
            Assert.True(r.SuggestTransfer); // UI 据此展示「转人工」
            Assert.Null(r.Answer);          // answer 缺省 = null
        }

        [Fact]
        public async Task Query_Disabled_ReturnsNull()
        {
            var (svc, transport) = NewServices();
            transport.EnqueueJson(501, Fixtures.Failure(
                "COMMON_CAPABILITY_DISABLED", "capability 'assistant' not configured", false));

            Assert.Null(await svc.Assistant.QueryAsync("q", CancellationToken.None)); // 隐藏入口
        }

        [Fact]
        public async Task Query_Validation400_TypedNoRetry()
        {
            var (svc, transport) = NewServices();
            transport.EnqueueJson(400, Fixtures.Failure(
                "COMMON_INVALID_ARGUMENT", "text 须为 1-500 字符(修剪首尾后)", false));

            var ex = await Assert.ThrowsAsync<CourierException>(
                () => svc.Assistant.QueryAsync("   ", CancellationToken.None));
            Assert.Equal("COMMON_INVALID_ARGUMENT", ex.Error.WireCode);
            Assert.Single(transport.Requests.Skip(1)); // retryable=false,无重试
        }
    }
}
