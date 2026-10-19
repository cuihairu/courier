// M2 后段实名域客户端(契约 realname.md Frozen v1;consent 组 v1.1,F31)。
// 红线落地:姓名/证件号只经 SubmitAsync 传输一次,SDK 不缓存、不落盘、不进日志;
// consent 的出生日期同款红线(只经 DeclareAgeAsync 传输一次,UI 展示仅年份);
// playtime-report 与 consent/s2s/* 是 S2S 端点(F30/v1.1),SDK 不封装——接入方服务端直调。
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

        /// <summary>嵌套 consent 面(契约 v1.1 投影 RealName.Consent)。</summary>
        public ConsentService Consent { get; }

        public RealNameService(ApiClient api)
        {
            _api = api;
            Consent = new ConsentService(api);
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

    /// <summary>年龄申报请求(POST /v1/realname/consent/declare-age)。
    /// 字段只在传输链存活,勿持久化(红线:出生日期不缓存、不落盘、不进诊断采集)。</summary>
    public sealed class ConsentDeclareRequest
    {
        public string BirthDate { get; set; }
    }

    /// <summary>RealName.Consent 年龄门/家长同意(契约 realname.md v1.1,F31;
    /// 挂载 /v1/realname/consent/*,玩家 Bearer 认证)。
    /// 默认关闭:501 COMMON_CAPABILITY_DISABLED → null,接入方隐藏年龄门 UI,
    /// 不进报错路径(同实名域 v1);判定经 200 载荷,零新增错误码。
    /// OnConsentChanged 回调预留:消费 SSE realname.consent.updated(同 config.updated
    /// 帧形状),事件分发在接入方,SDK 不内置订阅;REVOKED 到达后的诊断停发联动为
    /// diagnostics.md 预留位。</summary>
    public sealed class ConsentService
    {
        const string ConsentPrefix = "/v1/realname/consent";
        const string CodeCapabilityDisabled = "COMMON_CAPABILITY_DISABLED";
        static readonly object EmptyBody = new { }; // 契约固定空体 {}
        readonly ApiClient _api;

        public ConsentService(ApiClient api)
        {
            _api = api;
        }

        /// <summary>同意状态查询(六态,时间戳只在对应态下发)。能力未开启 → null。</summary>
        public async Task<ConsentStatusDto> StatusAsync(CancellationToken ct)
        {
            return await SendOrDisabled<ConsentStatusDto>("GET", ConsentPrefix + "/status",
                null, ct).ConfigureAwait(false);
        }

        /// <summary>年龄申报(申报即判定,响应 data = 同意状态;防回拨:重复申报仅趋严生效)。
        /// 红线:birthDate 只在本调用传输一次,SDK 不缓存、不落盘、不进诊断采集,
        /// UI 展示仅年份。能力未开启 → null。</summary>
        public async Task<ConsentStatusDto> DeclareAgeAsync(string birthDate,
            CancellationToken ct)
        {
            return await SendOrDisabled<ConsentStatusDto>("POST",
                ConsentPrefix + "/declare-age",
                new ConsentDeclareRequest { BirthDate = birthDate }, ct).ConfigureAwait(false);
        }

        /// <summary>发起 VPC 家长同意请求(仅 PENDING_PARENT 态可发起;请求期内幂等,
        /// 未过期返回同一 requestId)。能力未开启 → null。</summary>
        public async Task<ConsentRequestDto> RequestParentConsentAsync(CancellationToken ct)
        {
            return await SendOrDisabled<ConsentRequestDto>("POST",
                ConsentPrefix + "/parent-consent/request", EmptyBody, ct).ConfigureAwait(false);
        }

        // consent 域专用降级:501 能力关闭按契约转「未启用」(null),不走异常路径。
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
                    return null; // 契约:查询返回「未启用」,接入方隐藏年龄门 UI
                }
                throw; // 其余错误(参数/限流/网络)照常抛给调用方分支
            }
        }
    }
}
