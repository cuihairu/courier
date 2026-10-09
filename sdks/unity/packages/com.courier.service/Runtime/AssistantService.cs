// M4 小助手客户端(契约 assistant.md Frozen v1)。POST /v1/assistant/query(全 Bearer)。
// 未命中不是错误:返回 matched=false 的结果对象,UI 据此引导转人工;
// 501 能力未接 → null(隐藏小助手入口,能力安静地不存在);无本地缓存(即席查询)。
using System.Threading;
using System.Threading.Tasks;
using Courier.Core;

namespace Courier.Service
{
    public sealed class AssistantService
    {
        const string Prefix = "/v1/assistant";
        const string CodeCapabilityDisabled = "COMMON_CAPABILITY_DISABLED";

        readonly ApiClient _api;

        public AssistantService(ApiClient api)
        {
            _api = api;
        }

        /// <summary>提问(1-500 字符;空/超长 = 使用方错误,服务端 400)。
        /// 未命中 → Matched=false + SuggestTransfer=true;UI 据此调 Support 域提单转人工。</summary>
        public async Task<AssistantQueryDto> QueryAsync(string text, CancellationToken ct)
        {
            try
            {
                var json = await _api.SendAsync("POST", Prefix + "/query",
                    new QueryRequest { Text = text }, true, ct).ConfigureAwait(false);
                return Json.Deserialize<AssistantQueryDto>(json);
            }
            catch (CourierException ex)
            {
                if (ex.Error.WireCode == CodeCapabilityDisabled)
                {
                    return null; // 契约:能力未接 → 隐藏小助手入口
                }
                throw; // 参数/限流/网络照常抛给调用方
            }
        }

        sealed class QueryRequest
        {
            public string Text { get; set; }
        }
    }
}
