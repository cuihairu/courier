// 诊断可选包配置(契约 diagnostics.md「配置形状」)。四类同构:enabled + endpoint;
// 默认全关(零开销哲学:未配置即 no-op)。资源上下文由宿主注入,不采集则缺省。
namespace Courier.Diagnostics
{
    /// <summary>四类诊断能力(契约「四类能力与 Provider 生态」)。</summary>
    public enum DiagnosticsCategory
    {
        Crash = 0,
        Trace = 1,
        Performance = 2,
        Analytics = 3,
    }

    /// <summary>单类开关:endpoint 为空时按未配置处理(契约:开启必须同时配置端点)。</summary>
    public sealed class DiagnosticsCategoryOptions
    {
        public bool Enabled;
        public string Endpoint;
    }

    /// <summary>四类独立开关,缺省全关。</summary>
    public sealed class DiagnosticsOptions
    {
        public DiagnosticsCategoryOptions Crash = new DiagnosticsCategoryOptions();
        public DiagnosticsCategoryOptions Trace = new DiagnosticsCategoryOptions();
        public DiagnosticsCategoryOptions Performance = new DiagnosticsCategoryOptions();
        public DiagnosticsCategoryOptions Analytics = new DiagnosticsCategoryOptions();

        public DiagnosticsCategoryOptions Of(DiagnosticsCategory category)
        {
            switch (category)
            {
                case DiagnosticsCategory.Crash: return Crash;
                case DiagnosticsCategory.Trace: return Trace;
                case DiagnosticsCategory.Performance: return Performance;
                default: return Analytics;
            }
        }

        /// <summary>任一类有效开启(开启且配了端点)= 需要建实例;否则全 no-op。</summary>
        public bool AnyEnabled
        {
            get
            {
                for (var cat = DiagnosticsCategory.Crash; cat <= DiagnosticsCategory.Analytics; cat++)
                {
                    var o = Of(cat);
                    if (o.Enabled && !string.IsNullOrEmpty(o.Endpoint))
                    {
                        return true;
                    }
                }
                return false;
            }
        }
    }

    /// <summary>资源上下文(宿主注入;不采集的字段留 null,序列化时不下发)。
    /// 敏感接口报文永不进入诊断采集(契约红线 5,本包无此数据路径)。</summary>
    public sealed class DiagnosticsResource
    {
        public string GameId;
        public string Env;
        public string Platform;
        public string AppVersion;
        public string EngineVersion;
        public string SessionId;
        public string OsVersion;
        public string DeviceModel;
    }
}
