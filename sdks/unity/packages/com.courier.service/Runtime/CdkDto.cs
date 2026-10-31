// F62 兑换码域 DTO(契约 cdk.md Frozen v1)。同账号+码重复 → repeated=true。
namespace Courier.Service
{
    /// <summary>兑换结果。</summary>
    public sealed class RedeemResultDto
    {
        public string Code { get; set; }
        public string Grant { get; set; }
        public bool Repeated { get; set; }
    }
}
