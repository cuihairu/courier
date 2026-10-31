// F62 邮件客户端(契约 mail.md Frozen v1)。玩家侧三端点全 Bearer;
// mail.updated 推送为提示,列表为准。
using System.Threading;
using System.Threading.Tasks;
using Courier.Core;

namespace Courier.Service
{
    public sealed class MailService
    {
        const string Prefix = "/v1/mail";
        const string CodeCapabilityDisabled = "COMMON_CAPABILITY_DISABLED";

        readonly ApiClient _api;

        public MailService(ApiClient api) { _api = api; }

        /// <summary>我的邮件列表(个人+全量,最新在前;501 → null)。</summary>
        public async Task<MailPageDto> ListAsync(int limit, string cursor, CancellationToken ct)
        {
            var path = Prefix + "?limit=" + limit;
            if (!string.IsNullOrEmpty(cursor)) { path += "&cursor=" + cursor; }
            return await SendOrDisabled<MailPageDto>("GET", path, null, ct).ConfigureAwait(false);
        }

        /// <summary>邮件详情;不存在/非本人 → typed MAIL_NOT_FOUND。</summary>
        public async Task<MailDto> GetAsync(string mailId, CancellationToken ct)
        {
            return await SendOrDisabled<MailDto>("GET", Prefix + "/" + mailId, null, ct)
                .ConfigureAwait(false);
        }

        /// <summary>领取附件(幂等:重复领取同结果,不二次发放)。</summary>
        public async Task<MailDto> ClaimAsync(string mailId, CancellationToken ct)
        {
            return await SendOrDisabled<MailDto>("POST", Prefix + "/" + mailId + "/claim",
                new object(), ct).ConfigureAwait(false);
        }

        async Task<T> SendOrDisabled<T>(string method, string path, object body,
            CancellationToken ct) where T : class
        {
            try
            {
                var json = await _api.SendAsync(method, path, body, true, ct).ConfigureAwait(false);
                return Json.Deserialize<T>(json);
            }
            catch (CourierException ex)
            {
                if (ex.Error.WireCode == CodeCapabilityDisabled) { return null; }
                throw;
            }
        }
    }
}
