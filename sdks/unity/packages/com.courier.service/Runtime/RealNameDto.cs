// 实名域 DTO(契约 realname.md Frozen v1 数据模型;consent 组 v1.1;camelCase wire 对齐)。
namespace Courier.Service
{
    /// <summary>实名状态:state ∈ UNVERIFIED/PENDING_REVIEW/VERIFIED/REJECTED;
    /// isMinor/verifiedAt 仅 VERIFIED 后下发(其余缺省)。</summary>
    public sealed class RealNameStatusDto
    {
        public string State { get; set; }
        public bool? IsMinor { get; set; }
        public string VerifiedAt { get; set; }
    }

    /// <summary>可玩时段(F28):nextWindowAt 仅不可玩时下发。</summary>
    public sealed class RealNameCurfewDto
    {
        public bool Playable { get; set; }
        public string NextWindowAt { get; set; }
    }

    /// <summary>充值额度判定(F29):限额字段 null = 未下发(成年人无限额)。</summary>
    public sealed class RealNameChargeDto
    {
        public bool Allowed { get; set; }
        public int? SingleLimitCents { get; set; }
        public int? MonthlyLimitCents { get; set; }
        public int? MonthlyUsedCents { get; set; }
    }

    /// <summary>同意状态(consent v1.1 六态:UNDECLARED/ADULT/PENDING_PARENT/GRANTED/DENIED/REVOKED;
    /// 时间戳字段只在对应态下发,判定由服务端下发,客户端不得自行推断)。</summary>
    public sealed class ConsentStatusDto
    {
        public string State { get; set; }
        public string DeclaredAt { get; set; }
        public string GrantedAt { get; set; }
        public string RevokedAt { get; set; }
        public string RequestExpiresAt { get; set; }
    }

    /// <summary>VPC 家长验证请求回执(PENDING_PARENT 期间幂等:未过期返回同一 requestId)。</summary>
    public sealed class ConsentRequestDto
    {
        public string RequestId { get; set; }
        public string RequestExpiresAt { get; set; }
    }
}
