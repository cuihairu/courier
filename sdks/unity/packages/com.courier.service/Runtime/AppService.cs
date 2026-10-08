// M3 应用状态客户端(契约 app.md Frozen v1)。三端点匿名可(维护中仍需可查);
// 501 能力未接 → null(跳过版本/维护检查直接登录,能力安静地不存在)。
// 503 APP_MAINTENANCE / 426 APP_VERSION_UNSUPPORTED 由 ApiClient 照常抛 typed
// 异常(信封已登记),调用方据此走维护页/更新引导;SDK 不自动跳转商店。
using System;
using System.Threading;
using System.Threading.Tasks;
using Courier.Core;

namespace Courier.Service
{
    public sealed class AppService
    {
        const string Prefix = "/v1/app";
        const string CodeCapabilityDisabled = "COMMON_CAPABILITY_DISABLED";

        readonly ApiClient _api;

        public AppService(ApiClient api)
        {
            _api = api;
        }

        /// <summary>版本检查:appVersion 上报参与 forceUpdate 判定(缺省 = 不判定);
        /// platform 可选。forceUpdate=true 引导更新(受保护操作服务端 426 拦截)。</summary>
        public async Task<AppVersionDto> CheckUpdateAsync(string appVersion, string platform,
            CancellationToken ct)
        {
            var query = "";
            if (!string.IsNullOrEmpty(appVersion))
            {
                query += "&appVersion=" + Uri.EscapeDataString(appVersion);
            }
            if (!string.IsNullOrEmpty(platform))
            {
                query += "&platform=" + Uri.EscapeDataString(platform);
            }
            if (query.Length > 0)
            {
                query = "?" + query.Substring(1);
            }
            return await SendOrDisabled<AppVersionDto>("GET", Prefix + "/version" + query, ct)
                .ConfigureAwait(false);
        }

        /// <summary>维护状态查询:维护页数据源;estimatedRecoveryAt 供轮询节奏。</summary>
        public async Task<AppMaintenanceDto> CheckMaintenanceAsync(CancellationToken ct)
        {
            return await SendOrDisabled<AppMaintenanceDto>("GET", Prefix + "/maintenance", ct)
                .ConfigureAwait(false);
        }

        /// <summary>环境回显:初始化后校验 scope 与网关一致(排查接错环境)。</summary>
        public async Task<AppEnvironmentDto> GetEnvironmentAsync(CancellationToken ct)
        {
            return await SendOrDisabled<AppEnvironmentDto>("GET", Prefix + "/environment", ct)
                .ConfigureAwait(false);
        }

        // App 域降级:501 能力关闭 → null(未启用态,跳过检查直接登录)。
        async Task<T> SendOrDisabled<T>(string method, string path, CancellationToken ct)
            where T : class
        {
            try
            {
                var json = await _api.SendAsync(method, path, null, false, ct)
                    .ConfigureAwait(false);
                return Json.Deserialize<T>(json);
            }
            catch (CourierException ex)
            {
                if (ex.Error.WireCode == CodeCapabilityDisabled)
                {
                    return null;
                }
                throw;
            }
        }
    }
}
