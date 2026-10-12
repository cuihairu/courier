// v1.1 oauth.md 验收:第三方身份 wire 面(POST oauth/exchange + oauth/bind,
// GET oauth/bindings + DELETE oauth/bindings/{provider})。
using System.Linq;
using System.Threading;
using System.Threading.Tasks;
using Courier;
using Courier.Core;
using Courier.CoreTests;
using Courier.Identity;
using Xunit;

namespace Courier.CoreTests
{
    public class IdentityOAuthTests
    {
        static (CourierClient, FakeTransport) NewClient()
        {
            var transport = new FakeTransport();
            var client = new CourierClient(Fixtures.Config(), transport, new FakeTokenStore(),
                new FixedClock(Fixtures.BaseTime));
            transport.EnqueueJson(200, Fixtures.Success(Fixtures.SessionJson()));
            client.InitAsync(CancellationToken.None).Wait();
            client.GuestAsync(new GuestRequest { DeviceId = "device-1", Platform = "ios" },
                CancellationToken.None).Wait();
            return (client, transport);
        }

        [Fact]
        public async Task OAuthExchange_AnonymousPost_AdoptsNewSession()
        {
            // 登录族编排要求 Ready/SignedOut:空 store 下 InitAsync 不发请求(exchange 前不做 guest)。
            var transport = new FakeTransport();
            var client = new CourierClient(Fixtures.Config(), transport, new FakeTokenStore(),
                new FixedClock(Fixtures.BaseTime));
            await client.InitAsync(CancellationToken.None);
            transport.EnqueueJson(200, Fixtures.Success(
                Fixtures.SessionJson(accountType: "OAUTH", accountId: "acc_oa",
                    access: "access-oa", refresh: "refresh-oa", deviceId: "device-oa")));

            var session = await client.OAuthExchangeAsync(new OAuthExchangeRequest
            {
                Provider = "google",
                Token = "ok.sub-1",
                TokenType = "ID_TOKEN",
                DeviceId = "device-oa",
            }, CancellationToken.None);

            var req = transport.Requests.Last();
            Assert.Equal("POST", req.Method);
            Assert.Equal("https://api.example.com/v1/identity/oauth/exchange", req.Url);
            Assert.False(req.Headers.ContainsKey("Authorization")); // 匿名端点
            Assert.Equal(
                "{\"provider\":\"google\",\"token\":\"ok.sub-1\",\"tokenType\":\"ID_TOKEN\"," +
                "\"deviceId\":\"device-oa\"}", req.JsonBody);
            Assert.Equal("OAUTH", session.Account.Type);

            // 登录族编排:响应会话落库,后续请求带新 access。
            transport.EnqueueJson(200, Fixtures.Success("{}"));
            await client.Identity.GetSessionAsync(CancellationToken.None);
            Assert.Equal("Bearer access-oa", transport.Requests.Last().Headers["Authorization"]);
        }

        [Fact]
        public async Task OauthBind_BearerPost_ParseBinding()
        {
            var (client, transport) = NewClient();
            transport.EnqueueJson(200, Fixtures.Success(
                "{\"provider\":\"google\",\"createdAt\":\"2026-10-12T00:00:00.000Z\"}"));

            var binding = await client.OauthBindAsync("google", "tok_x", CancellationToken.None);

            var req = transport.Requests.Last();
            Assert.Equal("POST", req.Method);
            Assert.Equal("https://api.example.com/v1/identity/oauth/bind", req.Url);
            Assert.Equal("Bearer access-1", req.Headers["Authorization"]);
            Assert.Equal("{\"provider\":\"google\",\"token\":\"tok_x\"}", req.JsonBody);
            Assert.Equal("google", binding.Provider);
            Assert.Equal("2026-10-12T00:00:00.000Z", binding.CreatedAt);
        }

        [Fact]
        public async Task OauthUnbind_DeleteIdempotent()
        {
            var (client, transport) = NewClient();
            transport.EnqueueJson(200, Fixtures.Success("{}"));
            transport.EnqueueJson(200, Fixtures.Success("{}")); // 幂等:重复解绑仍 200

            await client.OauthUnbindAsync("google", CancellationToken.None);
            await client.OauthUnbindAsync("google", CancellationToken.None);

            var reqs = transport.Requests.Skip(2).ToList(); // 跳过 init+guest
            Assert.All(reqs, r =>
            {
                Assert.Equal("DELETE", r.Method);
                Assert.Equal("https://api.example.com/v1/identity/oauth/bindings/google", r.Url);
                Assert.Equal("Bearer access-1", r.Headers["Authorization"]);
                Assert.Null(r.JsonBody);
            });
        }

        [Fact]
        public async Task OauthBindings_GetList_ParseItems()
        {
            var (client, transport) = NewClient();
            transport.EnqueueJson(200, Fixtures.Success(
                "{\"items\":[" +
                "{\"provider\":\"apple\",\"createdAt\":\"2026-10-12T00:00:00.000Z\"}," +
                "{\"provider\":\"google\",\"createdAt\":\"2026-10-13T00:00:00.000Z\"}]}"));

            var list = await client.OauthBindingsAsync(CancellationToken.None);

            var req = transport.Requests.Last();
            Assert.Equal("GET", req.Method);
            Assert.Equal("https://api.example.com/v1/identity/oauth/bindings", req.Url);
            Assert.Equal("Bearer access-1", req.Headers["Authorization"]);
            Assert.Equal(2, list.Items.Count); // 按 createdAt 升序;契约不返回 subject
            Assert.Equal("apple", list.Items[0].Provider);
            Assert.Equal("google", list.Items[1].Provider);
            Assert.Null(list.NextCursor);
        }
    }
}
