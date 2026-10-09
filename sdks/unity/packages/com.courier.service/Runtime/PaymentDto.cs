// M5 支付域 DTO(契约 payment.md Frozen v1)。状态机 CREATED→PAID→DELIVERED
// / CREATED|PAID→CLOSED;金额原样渲染(amountCents/currency),客户端不做金额计算。
using System.Collections.Generic;

namespace Courier.Service
{
    /// <summary>可购 SKU(服务端价格表投影;金额是服务端定价事实,展示原样)。</summary>
    public sealed class PaymentSkuDto
    {
        public string Id { get; set; }
        public string ProductId { get; set; }
        public string Form { get; set; }
        public long AmountCents { get; set; }
        public string Currency { get; set; }
    }

    public sealed class PaymentSkusPageDto
    {
        public List<PaymentSkuDto> Items { get; set; }
        public string NextCursor { get; set; }
    }

    /// <summary>订单快照。PayToken 仅下单响应出现一次(消费归渠道 SDK,SDK 不解释);
    /// 详情/列表不下发。发货感知 = 轮询本对象 Status 到 DELIVERED。</summary>
    public sealed class PaymentOrderDto
    {
        public string Id { get; set; }
        public string Status { get; set; }
        public string SkuId { get; set; }
        public string ProductId { get; set; }
        public string Form { get; set; }
        public long AmountCents { get; set; }
        public string Currency { get; set; }
        public string PayToken { get; set; }
        public string CreatedAt { get; set; }
        public string UpdatedAt { get; set; }
        public string PaidAt { get; set; }
        public string DeliveredAt { get; set; }
    }

    public sealed class PaymentOrdersPageDto
    {
        public List<PaymentOrderDto> Items { get; set; }
        public string NextCursor { get; set; }
    }
}
