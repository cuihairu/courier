// F62 兑换码客户端(契约 cdk.md Frozen v1)。POST /v1/cdk/redeem 全 Bearer。
using System.Threading;
using System.Threading.Tasks;
using Courier.Core;

namespace Courier.Service
{
    public sealed class CdkService
    {
        const string Prefix = "/v1/cdk";
        const string CodeCapabilityDisabled = "COMMON_CAPABILITY_DISABLED";

        readonly ApiClient _api;

        public CdkService(ApiClient api) { _api = api; }

        /// <summary>兑换一个码(501 → null;未知码 → CDK_NOT_FOUND;限次 → CDK_EXHAUSTED)。</summary>
        public async Task<RedeemResultDto> RedeemAsync(string code, CancellationToken ct)
        {
            try
            {
                var json = await _api.SendAsync("POST", Prefix + "/redeem",
                    new RedeemRequest { Code = code }, true, ct).ConfigureAwait(false);
                return Json.Deserialize<RedeemResultDto>(json);
            }
            catch (CourierException ex)
            {
                if (ex.Error.WireCode == CodeCapabilityDisabled) { return null; }
                throw;
            }
        }

        sealed class RedeemRequest
        {
            public string Code { get; set; }
        }
    }
}
