// M5 支付域服务测试:SKU 投影、下单只发 skuId(服务端定价红线)、订单轮询(发货感知)、
// 归属 404 直抛、501 → null(隐藏商城/充值 UI)。
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
    public class PaymentServiceTests
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
        public async Task GetSkus_WireAndParse()
        {
            var (svc, transport) = NewServices();
            transport.EnqueueJson(200, Fixtures.Success(
                "{\"items\":[{\"id\":\"sku_gem_60\",\"productId\":\"com.demo.gem60\"," +
                "\"form\":\"DIRECT_PURCHASE\",\"amountCents\":600,\"currency\":\"CNY\"}]," +
                "\"nextCursor\":\"\"}"));

            var r = await svc.Payments.GetSkusAsync(CancellationToken.None);

            var req = transport.Requests.Last();
            Assert.Equal("GET", req.Method);
            Assert.Equal("https://api.example.com/v1/payments/skus", req.Url);
            Assert.Equal("Bearer access-1", req.Headers["Authorization"]); // 契约:玩家端点全 Bearer
            Assert.Single(r.Items);
            Assert.Equal("sku_gem_60", r.Items[0].Id);
            Assert.Equal(600, r.Items[0].AmountCents);
            Assert.Equal("CNY", r.Items[0].Currency);
        }

        [Fact]
        public async Task CreateOrder_ServerSidePricing_OnlySendsSkuId()
        {
            var (svc, transport) = NewServices();
            transport.EnqueueJson(200, Fixtures.Success(
                "{\"id\":\"order_1\",\"status\":\"CREATED\",\"skuId\":\"sku_gem_60\"," +
                "\"productId\":\"com.demo.gem60\",\"form\":\"DIRECT_PURCHASE\"," +
                "\"amountCents\":600,\"currency\":\"CNY\",\"payToken\":\"sbox_abc\"," +
                "\"createdAt\":\"2026-10-09T12:00:00.000Z\",\"updatedAt\":\"2026-10-09T12:00:00.000Z\"}"));

            var r = await svc.Payments.CreateOrderAsync("sku_gem_60", CancellationToken.None);

            var req = transport.Requests.Last();
            Assert.Equal("POST", req.Method);
            Assert.Equal("https://api.example.com/v1/payments/orders", req.Url);
            Assert.Equal("Bearer access-1", req.Headers["Authorization"]);
            // 服务端定价红线:请求体只有 skuId,不含任何金额字段。
            Assert.Equal("{\"skuId\":\"sku_gem_60\"}", req.JsonBody);
            Assert.Equal("CREATED", r.Status);
            Assert.Equal("sbox_abc", r.PayToken); // 下单响应才有 payToken
            Assert.Equal(600, r.AmountCents);
        }

        [Fact]
        public async Task GetOrder_PollsStatusForDelivery()
        {
            var (svc, transport) = NewServices();
            transport.EnqueueJson(200, Fixtures.Success(
                "{\"id\":\"order_1\",\"status\":\"DELIVERED\",\"skuId\":\"sku_gem_60\"," +
                "\"amountCents\":600,\"currency\":\"CNY\"," +
                "\"createdAt\":\"2026-10-09T12:00:00.000Z\",\"updatedAt\":\"2026-10-09T12:00:02.000Z\"," +
                "\"paidAt\":\"2026-10-09T12:00:01.000Z\",\"deliveredAt\":\"2026-10-09T12:00:02.000Z\"}"));

            var r = await svc.Payments.GetOrderAsync("order_1", CancellationToken.None);

            var req = transport.Requests.Last();
            Assert.Equal("GET", req.Method);
            Assert.Equal("https://api.example.com/v1/payments/orders/order_1", req.Url);
            Assert.Equal("DELIVERED", r.Status); // 发货感知 = 轮询
            Assert.Null(r.PayToken);             // 详情不下发 payToken
            Assert.Equal("2026-10-09T12:00:02.000Z", r.DeliveredAt);
        }

        [Fact]
        public async Task ListOrders_WireAndParse()
        {
            var (svc, transport) = NewServices();
            transport.EnqueueJson(200, Fixtures.Success(
                "{\"items\":[{\"id\":\"order_2\",\"status\":\"CREATED\",\"skuId\":\"sku_pass\"," +
                "\"amountCents\":3000,\"currency\":\"CNY\",\"updatedAt\":\"2026-10-09T12:00:05.000Z\"}," +
                "{\"id\":\"order_1\",\"status\":\"DELIVERED\",\"skuId\":\"sku_gem_60\"," +
                "\"amountCents\":600,\"currency\":\"CNY\",\"updatedAt\":\"2026-10-09T12:00:02.000Z\"}]," +
                "\"nextCursor\":\"\"}"));

            var r = await svc.Payments.ListOrdersAsync(CancellationToken.None);

            var req = transport.Requests.Last();
            Assert.Equal("https://api.example.com/v1/payments/orders", req.Url);
            Assert.Equal(2, r.Items.Count);
            Assert.Equal("order_2", r.Items[0].Id); // updatedAt 倒序由服务端保证
            Assert.All(r.Items, o => Assert.Null(o.PayToken));
        }

        [Fact]
        public async Task OrderOwnership_404_TypedNoRetry()
        {
            var (svc, transport) = NewServices();
            transport.EnqueueJson(404, Fixtures.Failure(
                "PAYMENT_ORDER_NOT_FOUND", "订单不存在", false));

            var ex = await Assert.ThrowsAsync<CourierException>(
                () => svc.Payments.GetOrderAsync("order_other", CancellationToken.None));
            Assert.Equal("PAYMENT_ORDER_NOT_FOUND", ex.Error.WireCode);
            Assert.Single(transport.Requests.Skip(1)); // retryable=false,无重试
        }

        [Fact]
        public async Task Payments_Disabled_ReturnsNull()
        {
            var (svc, transport) = NewServices();
            transport.EnqueueJson(501, Fixtures.Failure(
                "COMMON_CAPABILITY_DISABLED", "capability 'payments' not configured", false));

            Assert.Null(await svc.Payments.GetSkusAsync(CancellationToken.None)); // 隐藏商城/充值 UI
        }
    }
}
