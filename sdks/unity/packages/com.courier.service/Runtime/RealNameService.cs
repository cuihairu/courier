// M2 后段实名域客户端(契约 realname.md Frozen v1)。
// 红线落地:姓名/证件号只经 SubmitAsync 传输一次,SDK 不缓存、不落盘、不进日志;
// playtime-report 是 S2S 端点(F30),SDK 不封装——接入方服务端直调。
using System.Threading;
using System.Threading.Tasks;
using Courier.Core;

namespace Courier.Service
{
    /// <summary>实名提交请求(POST /v1/realname/verify)。字段只在传输链存活,勿持久化。</summary>
    public sealed class RealNameSubmitRequest
    {
        public string Name { get; set; }
        public string IdNumber { get; set; }
    }

    public sealed class RealNameService
    {
        const string Prefix = "/v1/realname";
        const string CodeCapabilityDisabled = "COMMON_CAPABILITY_DISABLED";
        readonly ApiClient _api;

        public RealNameService(ApiClient api)
        {
            _api = api;
        }

        /// <summary>提交核验(F27)。REJECTED/PENDING_REVIEW 也是有效提交结果,不是异常。
        /// 能力未开启(501 COMMON_CAPABILITY_DISABLED)→ 返回 null,调用方隐藏实名 UI,
        /// 不进报错路径(契约「能力开关」节)。</summary>
        public async Task<RealNameStatusDto> SubmitAsync(string name, string idNumber,
            CancellationToken ct)
        {
            return await SendOrDisabled<RealNameStatusDto>("POST", Prefix + "/verify",
                new RealNameSubmitRequest { Name = name, IdNumber = idNumber }, ct)
                .ConfigureAwait(false);
        }

        /// <summary>状态查询(F27):UNVERIFIED/PENDING_REVIEW/VERIFIED/REJECTED;
        /// isMinor 仅 VERIFIED 后由服务端下发。能力未开启 → null。</summary>
        public async Task<RealNameStatusDto> StatusAsync(CancellationToken ct)
        {
            return await SendOrDisabled<RealNameStatusDto>("GET", Prefix + "/status", null, ct)
                .ConfigureAwait(false);
        }

        /// <summary>可玩时段查询(F28):playable=false 时 nextWindowAt 给下一窗口。
        /// 能力未开启 → null(接入方自行决定是否限制)。</summary>
        public async Task<RealNameCurfewDto> CurfewAsync(CancellationToken ct)
        {
            return await SendOrDisabled<RealNameCurfewDto>("GET", Prefix + "/curfew", null, ct)
                .ConfigureAwait(false);
        }

        /// <summary>充值额度校验(F29):支付下单前的前置校验;限额字段 0 = 未下发。
        /// 能力未开启 → null(不拦支付,由接入方决定)。</summary>
        public async Task<RealNameChargeDto> ChargeCheckAsync(int amountCents,
            CancellationToken ct)
        {
            return await SendOrDisabled<RealNameChargeDto>("POST", Prefix + "/charge-check",
                new RealNameChargeRequest { AmountCents = amountCents }, ct)
                .ConfigureAwait(false);
        }

        // 实名域专用降级:501 能力关闭按契约转「未启用」(null),不走异常路径。
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
                    return null; // 契约:查询接口返回「未启用」,接入方据此隐藏 UI
                }
                throw; // 其余错误(参数/限流/网络)照常抛给调用方分支
            }
        }
    }

    public sealed class RealNameChargeRequest
    {
        public int AmountCents { get; set; }
    }
}
