// M3 品牌客户端(契约 branding.md Frozen v1)。SDK 透传原始 JSON——零解释、
// 零渲染、零缓存策略(缓存归宿主/平台 Adapter);兜底规则与消费全在 UI 包。
// 501 能力未接 → null(UI 全默认,Courier 自己的标兜底)。
using System.Threading;
using System.Threading.Tasks;
using Courier.Core;

namespace Courier.Service
{
    public sealed class BrandingService
    {
        const string Path = "/v1/app/branding";
        const string CodeCapabilityDisabled = "COMMON_CAPABILITY_DISABLED";

        readonly ApiClient _api;

        public BrandingService(ApiClient api)
        {
            _api = api;
        }

        /// <summary>最近一次成功拉取;null = 从未拉到。</summary>
        public BrandingDto Cached { get; private set; }

        /// <summary>拉取品牌物料(匿名可:登录页就要显示品牌;version + 业务字段)。</summary>
        public async Task<BrandingDto> FetchAsync(CancellationToken ct)
        {
            try
            {
                var json = await _api.SendAsync("GET", Path, null, false, ct)
                    .ConfigureAwait(false);
                var dto = Json.Deserialize<BrandingDto>(json);
                Cached = dto;
                return dto;
            }
            catch (CourierException ex)
            {
                if (ex.Error.WireCode == CodeCapabilityDisabled)
                {
                    return null; // 契约:未启用态,UI 全默认
                }
                throw;
            }
        }

        /// <summary>branding.updated 事件到达后的重拉判据:事件 version 与缓存不同
        /// 才需要重拉(相同即忽略,不重渲染);从未拉到过 → 一律重拉。</summary>
        public bool NeedsRefetch(long eventVersion)
        {
            return Cached == null || Cached.Version != eventVersion;
        }
    }
}
