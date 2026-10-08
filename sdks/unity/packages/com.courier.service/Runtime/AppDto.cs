// M3 App/Config 域 DTO(契约 app.md、config.md Frozen v1)。wire camelCase 由
// Core Json resolver 统一映射;items 为任意 JSON 值(JToken 原样返回,SDK 不做
// 二次校验——接入方结构变更是使用方错误)。
using System.Collections.Generic;
using Newtonsoft.Json;
using Newtonsoft.Json.Linq;

namespace Courier.Service
{
    /// <summary>远程配置快照:configVersion(热生效判据)+ 命中条件的键值全量。</summary>
    public sealed class AppConfigDto
    {
        public long ConfigVersion { get; set; }
        public Dictionary<string, JToken> Items { get; set; }
    }

    /// <summary>品牌物料:version(热生效判据)+ 业务字段透传(ExtensionData 收
    /// 其余键,SDK 零解释;兜底规则与消费全在 UI 包)。</summary>
    public sealed class BrandingDto
    {
        public long Version { get; set; }

        [JsonExtensionData]
        public IDictionary<string, JToken> Fields { get; set; }
    }

    /// <summary>版本检查:forceUpdate = appVersion &lt; minVersion(服务端判定)。</summary>
    public sealed class AppVersionDto
    {
        public string LatestVersion { get; set; }
        public string MinVersion { get; set; }
        public string UpdateUrl { get; set; }
        public bool ForceUpdate { get; set; }
    }

    /// <summary>维护状态:estimatedRecoveryAt/message 可选(缺省即 null)。</summary>
    public sealed class AppMaintenanceDto
    {
        public bool InMaintenance { get; set; }
        public string EstimatedRecoveryAt { get; set; }
        public string Message { get; set; }
    }

    /// <summary>环境回显:初始化校验(排查接错环境)。</summary>
    public sealed class AppEnvironmentDto
    {
        public string GameId { get; set; }
        public string Env { get; set; }
    }
}
