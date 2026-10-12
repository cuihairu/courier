// v1.1 验收:身份域注销与撤销 wire 面(POST account/delete + /account/delete/cancel)。
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
    public class IdentityDeleteTests
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
        public async Task DeleteAccount_BearerPost_ParsePending()
        {
            var (client, transport) = NewClient();
            transport.EnqueueJson(200, Fixtures.Success(
                "{\"status\":\"PENDING_DELETE\",\"cancelToken\":\"deltok_a\",\"pendingDelete\":true}"));

            var r = await client.Identity.DeleteAccountAsync(CancellationToken.None);

            var req = transport.Requests.Last();
            Assert.Equal("POST", req.Method);
            Assert.Equal("https://api.example.com/v1/identity/account/delete", req.Url);
            Assert.Equal("Bearer access-1", req.Headers["Authorization"]);
            Assert.Equal("PENDING_DELETE", r.Status);
            Assert.Equal("deltok_a", r.CancelToken);
            Assert.True(r.PendingDelete);
            Assert.Null(r.DeletedAt);
        }

        [Fact]
        public async Task DeleteAccount_ParseImmediate()
        {
            var (client, transport) = NewClient();
            transport.EnqueueJson(200, Fixtures.Success(
                "{\"status\":\"DELETED\",\"deletedAt\":\"2026-10-12T00:00:00.000Z\"}"));

            var r = await client.Identity.DeleteAccountAsync(CancellationToken.None);

            Assert.Equal("DELETED", r.Status);
            Assert.Null(r.CancelToken);
            Assert.False(r.PendingDelete);
            Assert.Equal("2026-10-12T00:00:00.000Z", r.DeletedAt);
        }

        [Fact]
        public async Task CancelDelete_AnonymousPost_OneTimeToken()
        {
            var (client, transport) = NewClient();
            transport.EnqueueJson(200, Fixtures.Success("{}"));

            await client.Identity.CancelDeleteAsync("deltok_a", CancellationToken.None);

            var req = transport.Requests.Last();
            Assert.Equal("POST", req.Method);
            Assert.Equal("https://api.example.com/v1/identity/account/delete/cancel", req.Url);
            Assert.False(req.Headers.ContainsKey("Authorization")); // 匿名端点
            Assert.Equal("{\"cancelToken\":\"deltok_a\"}", req.JsonBody);
        }
    }
}
