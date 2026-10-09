// M5 支付客户端(契约 payment.md Frozen v1)。玩家侧四端点全 Bearer;
// POST /v1/payments/callback 为 S2S 渠道回调(HMAC 签名即认证),不经客户端 SDK 封装。
// 下单只收 skuId(服务端定价红线:客户端不下发/不计算金额)。
using System.Threading;
using System.Threading.Tasks;
using Courier.Core;

namespace Courier.Service
{
    public sealed class PaymentService
    {
        const string Prefix = "/v1/payments";
        const string CodeCapabilityDisabled = "COMMON_CAPABILITY_DISABLED";

        readonly ApiClient _api;

        public PaymentService(ApiClient api)
        {
            _api = api;
        }

        /// <summary>可购 SKU 列表(价格表投影;金额原样展示)。</summary>
        public async Task<PaymentSkusPageDto> GetSkusAsync(CancellationToken ct)
        {
            return await SendOrDisabled<PaymentSkusPageDto>("GET", Prefix + "/skus", null, ct)
                .ConfigureAwait(false);
        }

        /// <summary>下单:服务端按 skuId 定价 → CREATED + PayToken(仅本次响应出现;
        /// 沙箱渠道直接回传模拟支付完成,真实渠道按渠道 SDK 语义消费)。</summary>
        public async Task<PaymentOrderDto> CreateOrderAsync(string skuId, CancellationToken ct)
        {
            return await SendOrDisabled<PaymentOrderDto>("POST", Prefix + "/orders",
                new CreateOrderRequest { SkuId = skuId }, ct).ConfigureAwait(false);
        }

        /// <summary>我的订单(updatedAt 倒序;不含 PayToken)。</summary>
        public async Task<PaymentOrdersPageDto> ListOrdersAsync(CancellationToken ct)
        {
            return await SendOrDisabled<PaymentOrdersPageDto>("GET", Prefix + "/orders", null, ct)
                .ConfigureAwait(false);
        }

        /// <summary>订单详情(发货感知 = 轮询本端点看 Status;不属当前玩家 → 404 不泄露存在性)。</summary>
        public async Task<PaymentOrderDto> GetOrderAsync(string orderId, CancellationToken ct)
        {
            return await SendOrDisabled<PaymentOrderDto>("GET", Prefix + "/orders/" + orderId, null, ct)
                .ConfigureAwait(false);
        }

        // 降级语义:501 能力关闭 → null(隐藏商城/充值 UI,不进报错路径);其余错误照常抛。
        async Task<T> SendOrDisabled<T>(string method, string path, object body,
            CancellationToken ct) where T : class
        {
            try
            {
                var json = await _api.SendAsync(method, path, body, true, ct)
                    .ConfigureAwait(false);
                return Json.Deserialize<T>(json);
            }
            catch (CourierException ex)
            {
                if (ex.Error.WireCode == CodeCapabilityDisabled)
                {
                    return null;
                }
                throw;
            }
        }

        sealed class CreateOrderRequest
        {
            public string SkuId { get; set; }
        }
    }
}
