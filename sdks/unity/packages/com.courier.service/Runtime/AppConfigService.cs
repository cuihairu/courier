// M3 远程配置客户端(契约 config.md Frozen v1)。
// 客户端要求:启动/进前台拉取;config.updated 事件到达后经 NeedsRefetch 判重拉
// (相同 version 即忽略);重拉失败沿用缓存(配置是加速器,不是依赖)。
using System;
using System.Collections.Generic;
using System.Threading;
using System.Threading.Tasks;
using Courier.Core;
using Newtonsoft.Json.Linq;

namespace Courier.Service
{
    public sealed class AppConfigService
    {
        const string Path = "/v1/app/config";
        const string CodeCapabilityDisabled = "COMMON_CAPABILITY_DISABLED";

        readonly ApiClient _api;

        public AppConfigService(ApiClient api)
        {
            _api = api;
        }

        /// <summary>最近一次成功拉取(缓存);null = 从未拉到。</summary>
        public AppConfigDto Cached { get; private set; }

        /// <summary>拉取命中当前条件的键值全量(platform/appVersion/region 可选;
        /// 缺省维度不参与过滤)。501 能力未接 → 空结果(v0 + 空 items,接入方使用
        /// 内建默认值,不进报错路径)。</summary>
        public async Task<AppConfigDto> FetchAsync(string platform, string appVersion,
            string region, CancellationToken ct)
        {
            var query = "";
            if (!string.IsNullOrEmpty(platform))
            {
                query += "&platform=" + UriEscape(platform);
            }
            if (!string.IsNullOrEmpty(appVersion))
            {
                query += "&appVersion=" + UriEscape(appVersion);
            }
            if (!string.IsNullOrEmpty(region))
            {
                query += "&region=" + UriEscape(region);
            }
            if (query.Length > 0)
            {
                query = "?" + query.Substring(1);
            }
            try
            {
                var json = await _api.SendAsync("GET", Path + query, null, true, ct)
                    .ConfigureAwait(false);
                var snapshot = Json.Deserialize<AppConfigDto>(json);
                Cached = snapshot;
                return snapshot;
            }
            catch (CourierException ex)
            {
                if (ex.Error.WireCode == CodeCapabilityDisabled)
                {
                    return new AppConfigDto
                    {
                        ConfigVersion = 0,
                        Items = new Dictionary<string, JToken>(),
                    };
                }
                throw; // 其余错误(认证/限流/网络)照常抛
            }
        }

        /// <summary>config.updated 事件到达后的重拉判据:事件 version 与缓存不同才需要
        /// 重拉(相同即忽略,不重渲染);从未拉到过 → 一律重拉。</summary>
        public bool NeedsRefetch(long eventConfigVersion)
        {
            return Cached == null || Cached.ConfigVersion != eventConfigVersion;
        }

        /// <summary>取值并按目标类型转换;键缺失 → false。类型不匹配抛 Newtonsoft
        /// 异常(使用方错误:SDK 按原始 JSON 值返回,不做二次校验)。</summary>
        public bool TryGet<T>(string key, out T value)
        {
            value = default;
            if (Cached == null || Cached.Items == null ||
                !Cached.Items.TryGetValue(key, out var token))
            {
                return false;
            }
            value = token.ToObject<T>();
            return true;
        }

        static string UriEscape(string v)
        {
            return Uri.EscapeDataString(v);
        }
    }
}
