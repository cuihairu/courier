// 批次 7 验收:RealNameService 与 realname.md 契约 wire 对齐——
// 提交/状态/时段/额度四端点、501 能力关闭转 null(不进报错路径)、
// 脱敏口径(RealNameMask)与 S2S 不封装红线(编译面由 packcheck 守)。
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
    public class RealNameServiceTests
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
        public async Task Submit_WireAndParse_VerifiedWithMinorFlag()
        {
            var (svc, transport) = NewServices();
            transport.EnqueueJson(200, Fixtures.Success(
                "{\"state\":\"VERIFIED\",\"isMinor\":false,\"verifiedAt\":\"2026-10-08T15:00:00.000Z\"}"));

            var status = await svc.RealName.SubmitAsync("张三", "110101199001011234",
                CancellationToken.None);

            var req = transport.Requests.Last();
            Assert.Equal("POST", req.Method);
            Assert.Equal("https://api.example.com/v1/realname/verify", req.Url);
            Assert.Equal("Bearer access-1", req.Headers["Authorization"]);
            Assert.Equal("{\"name\":\"张三\",\"idNumber\":\"110101199001011234\"}", req.JsonBody);
            Assert.Equal("VERIFIED", status.State);
            Assert.False(status.IsMinor.Value);
            Assert.Equal("2026-10-08T15:00:00.000Z", status.VerifiedAt);
        }

        [Fact]
        public async Task Submit_RejectedIsResult_NotException()
        {
            var (svc, transport) = NewServices();
            transport.EnqueueJson(200, Fixtures.Success(
                "{\"state\":\"REJECTED\"}"));

            var status = await svc.RealName.SubmitAsync("李四", "110101199001019999",
                CancellationToken.None);

            Assert.Equal("REJECTED", status.State);
            Assert.Null(status.IsMinor); // 仅 VERIFIED 下发
        }

        [Fact]
        public async Task Status_WireAndParse_Unverified()
        {
            var (svc, transport) = NewServices();
            transport.EnqueueJson(200, Fixtures.Success("{\"state\":\"UNVERIFIED\"}"));

            var status = await svc.RealName.StatusAsync(CancellationToken.None);

            var req = transport.Requests.Last();
            Assert.Equal("https://api.example.com/v1/realname/status", req.Url);
            Assert.Equal("UNVERIFIED", status.State);
        }

        [Fact]
        public async Task Curfew_WireAndParse_NextWindow()
        {
            var (svc, transport) = NewServices();
            transport.EnqueueJson(200, Fixtures.Success(
                "{\"playable\":false,\"nextWindowAt\":\"2026-10-09T20:00:00.000Z\"}"));

            var curfew = await svc.RealName.CurfewAsync(CancellationToken.None);

            var req = transport.Requests.Last();
            Assert.Equal("https://api.example.com/v1/realname/curfew", req.Url);
            Assert.False(curfew.Playable);
            Assert.Equal("2026-10-09T20:00:00.000Z", curfew.NextWindowAt);
        }

        [Fact]
        public async Task ChargeCheck_WireAndParse_Limits()
        {
            var (svc, transport) = NewServices();
            transport.EnqueueJson(200, Fixtures.Success(
                "{\"allowed\":false,\"singleLimitCents\":5000,\"monthlyLimitCents\":20000," +
                "\"monthlyUsedCents\":18000}"));

            var charge = await svc.RealName.ChargeCheckAsync(3000, CancellationToken.None);

            var req = transport.Requests.Last();
            Assert.Equal("POST", req.Method);
            Assert.Equal("https://api.example.com/v1/realname/charge-check", req.Url);
            Assert.Equal("{\"amountCents\":3000}", req.JsonBody);
            Assert.False(charge.Allowed);
            Assert.Equal(5000, charge.SingleLimitCents);
            Assert.Equal(20000, charge.MonthlyLimitCents);
            Assert.Equal(18000, charge.MonthlyUsedCents);
        }

        [Fact]
        public async Task CapabilityDisabled_ReturnsNull_NotErrorPath()
        {
            var (svc, transport) = NewServices();
            transport.EnqueueJson(501, Fixtures.Failure("COMMON_CAPABILITY_DISABLED",
                "realname not configured", false));

            var status = await svc.RealName.StatusAsync(CancellationToken.None);

            Assert.Null(status); // 契约:查询返回「未启用」,接入方隐藏 UI
        }

        [Fact]
        public async Task CapabilityDisabled_AllEndpointsReturnNull()
        {
            var (svc, transport) = NewServices();
            transport.EnqueueJson(501, Fixtures.Failure("COMMON_CAPABILITY_DISABLED",
                "realname not configured", false));
            transport.EnqueueJson(501, Fixtures.Failure("COMMON_CAPABILITY_DISABLED",
                "realname not configured", false));
            transport.EnqueueJson(501, Fixtures.Failure("COMMON_CAPABILITY_DISABLED",
                "realname not configured", false));

            Assert.Null(await svc.RealName.CurfewAsync(CancellationToken.None));
            Assert.Null(await svc.RealName.ChargeCheckAsync(100, CancellationToken.None));
            Assert.Null(await svc.RealName.SubmitAsync("张三", "110101199001011234",
                CancellationToken.None));
        }

        [Fact]
        public async Task OtherErrors_StillThrow()
        {
            var (svc, transport) = NewServices();
            // RATE_LIMITED retryable=true:L2 默认重试策略 MaxAttempts=3,三档全入队。
            for (var i = 0; i < 3; i++)
            {
                transport.EnqueueJson(429, Fixtures.Failure("RATE_LIMITED", "slow down", true),
                    retryAfter: "1");
            }

            var ex = await Assert.ThrowsAsync<CourierException>(
                () => svc.RealName.SubmitAsync("张三", "110101199001011234",
                    CancellationToken.None));

            Assert.Equal("RATE_LIMITED", ex.Error.WireCode); // 只有能力关闭走 null
            Assert.Equal(4, transport.Requests.Count); // 1 建会话 + 3 次核验(重试打满)
        }
    }

    public class RealNameMaskTests
    {
        [Theory]
        [InlineData("张三", "张*")]
        [InlineData("李四喜", "李**")]
        [InlineData("O", "O")]
        [InlineData("", "")]
        [InlineData(null, "")]
        public void MaskName_KeepsSurnameOnly(string name, string want)
        {
            Assert.Equal(want, RealNameMask.MaskName(name));
        }

        [Theory]
        [InlineData("110101199001011234", "110***********1234")]
        [InlineData("11010120150620123X", "110***********123X")]
        [InlineData("12345678", "123*5678")]
        [InlineData("1234567", "*******")] // <8 位:全文 *,不回显原文段
        [InlineData("", "")]
        [InlineData(null, "")]
        public void MaskIdNumber_First3Last4(string id, string want)
        {
            Assert.Equal(want, RealNameMask.MaskIdNumber(id));
        }
    }
}
