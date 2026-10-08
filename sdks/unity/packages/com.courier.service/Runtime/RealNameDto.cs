// 实名域 DTO(契约 realname.md Frozen v1 数据模型;camelCase wire 对齐)。
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
}
