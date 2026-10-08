// M3 Branding 测试:service 透传解析(version + ExtensionData)/匿名请求/501 → null/
// 重拉判据;UI 包 BrandingCatalog 兜底链与热切换(纯 C# 件,契约「兜底规则」)。
using System.Linq;
using System.Threading;
using System.Threading.Tasks;
using Courier;
using Courier.Core;
using Courier.Identity;
using Courier.Service;
using Courier.CoreTests;
using Courier.UI;
using Xunit;

namespace Courier.ServiceTests
{
    public class BrandingServiceTests
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
        public async Task Fetch_Anonymous_ParsesVersionAndFields()
        {
            var (svc, transport) = NewServices();
            transport.EnqueueJson(200, Fixtures.Success(
                "{\"version\":3,\"companyName\":\"示例互娱\"," +
                "\"theme\":{\"primaryColor\":\"#4C8DFF\"}," +
                "\"supportEntry\":{\"label\":\"联系客服\",\"url\":\"https://example.com/s\"}}"));

            var dto = await svc.Branding.FetchAsync(CancellationToken.None);

            var req = transport.Requests.Last();
            Assert.Equal("https://api.example.com/v1/app/branding", req.Url);
            Assert.False(req.Headers.ContainsKey("Authorization")); // 匿名可:登录页就要显示品牌
            Assert.Equal(3, dto.Version);
            Assert.Same(dto, svc.Branding.Cached);
            Assert.Equal("示例互娱", (string)dto.Fields["companyName"]);
            Assert.Equal("#4C8DFF", (string)dto.Fields["theme"]["primaryColor"]); // 业务字段透传
            Assert.Equal("联系客服", (string)dto.Fields["supportEntry"]["label"]);
        }

        [Fact]
        public async Task Fetch_EmptyInitial_VersionZeroNoFields()
        {
            var (svc, transport) = NewServices();
            transport.EnqueueJson(200, Fixtures.Success("{\"version\":0}"));

            var dto = await svc.Branding.FetchAsync(CancellationToken.None);

            Assert.Equal(0, dto.Version);
            Assert.True(dto.Fields == null || dto.Fields.Count == 0); // 空物料非错误
        }

        [Fact]
        public async Task Fetch_Disabled_ReturnsNull()
        {
            var (svc, transport) = NewServices();
            transport.EnqueueJson(501, Fixtures.Failure(
                "COMMON_CAPABILITY_DISABLED", "capability 'app' not configured", false));

            var dto = await svc.Branding.FetchAsync(CancellationToken.None);

            Assert.Null(dto); // 契约:未启用态,UI 全默认
            Assert.Null(svc.Branding.Cached);
        }

        [Fact]
        public async Task NeedsRefetch_ByEventVersion()
        {
            var (svc, transport) = NewServices();
            Assert.True(svc.Branding.NeedsRefetch(1)); // 从未拉到 → 重拉

            transport.EnqueueJson(200, Fixtures.Success("{\"version\":5,\"companyName\":\"x\"}"));
            await svc.Branding.FetchAsync(CancellationToken.None);

            Assert.False(svc.Branding.NeedsRefetch(5)); // 相同即忽略(不重渲染)
            Assert.True(svc.Branding.NeedsRefetch(6));  // 不同 → 重拉热切换
        }
    }

    public class BrandingCatalogTests
    {
        [Fact]
        public void Fallbacks_WhenNeverApplied()
        {
            ResetCatalog();
            Assert.Equal(-1, BrandingCatalog.Version); // 从未应用
            Assert.Equal(BrandingCatalog.DefaultCompanyName, BrandingCatalog.GetCompanyName());
            Assert.Equal(BrandingCatalog.DefaultPrimaryColor, BrandingCatalog.GetPrimaryColor());
            Assert.Equal(BrandingCatalog.DefaultSupportLabel, BrandingCatalog.GetSupportLabel());
            Assert.Null(BrandingCatalog.GetSupportUrl());
        }

        [Fact]
        public void Apply_HotSwitchAndIdempotent()
        {
            ResetCatalog();
            var first = Parse(1, "{\"version\":1,\"companyName\":\"示例互娱\"}");
            BrandingCatalog.Apply(first);
            Assert.Equal(1, BrandingCatalog.Version);
            Assert.Equal("示例互娱", BrandingCatalog.GetCompanyName());
            Assert.Equal(BrandingCatalog.DefaultPrimaryColor, BrandingCatalog.GetPrimaryColor()); // 缺失字段仍兜底

            BrandingCatalog.Apply(first); // 同 version:忽略(不重渲染)
            Assert.Equal(1, BrandingCatalog.Version);

            var second = Parse(2, "{\"version\":2,\"theme\":{\"primaryColor\":\"#111111\"}}");
            BrandingCatalog.Apply(second); // 热切换
            Assert.Equal(2, BrandingCatalog.Version);
            Assert.Equal("#111111", BrandingCatalog.GetPrimaryColor());
            Assert.Equal(BrandingCatalog.DefaultCompanyName, BrandingCatalog.GetCompanyName());

            BrandingCatalog.Apply(null); // null:忽略
            Assert.Equal(2, BrandingCatalog.Version);
        }

        [Fact]
        public void SupportEntry_NestedFields()
        {
            ResetCatalog();
            BrandingCatalog.Apply(Parse(3,
                "{\"version\":3,\"supportEntry\":{\"label\":\"在线客服\",\"url\":\"https://s.example\"}}"));
            Assert.Equal("在线客服", BrandingCatalog.GetSupportLabel());
            Assert.Equal("https://s.example", BrandingCatalog.GetSupportUrl());

            string entry;
            Assert.False(BrandingCatalog.TryGetString("supportEntry", out entry)); // 对象不是字符串
            Assert.Null(entry);
            Assert.False(BrandingCatalog.TryGetString("missing", out entry));
        }

        static BrandingDto Parse(long version, string json)
        {
            return Courier.Core.Json.Deserialize<BrandingDto>(json);
        }

        static void ResetCatalog()
        {
            BrandingCatalog.Apply(new BrandingDto { Version = -1 }); // 重置为未应用
        }
    }
}
