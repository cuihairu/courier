// 内置自有端点直发实现(契约「上报 Schema 与数据范围」wire)。单条一请求,
// v1 直发不缓冲;失败(网络错/非 2xx)静默丢弃——诊断永不反噬游戏(红线 6)。
// Sentry / OTLP 适配由接入方按同一接口接生态 SDK,不在此处。
using System.Collections.Generic;
using System.Text;
using System.Threading;
using System.Threading.Tasks;
using Courier.Core;
using Newtonsoft.Json.Linq;

namespace Courier.Diagnostics
{
    // 四类内置 reporter 的公共底座:fire-and-forget POST。
    abstract class HttpReporter
    {
        protected readonly ITransport Transport;
        protected readonly DiagnosticsResource Resource;
        protected readonly string Endpoint;

        protected HttpReporter(ITransport transport, DiagnosticsResource resource, string endpoint)
        {
            Transport = transport;
            Resource = resource;
            Endpoint = endpoint;
        }

        public static HttpReporter Create(DiagnosticsCategory category, ITransport transport,
            DiagnosticsResource resource, DiagnosticsCategoryOptions options)
        {
            switch (category)
            {
                case DiagnosticsCategory.Crash:
                    return new CrashReporter(transport, resource, options);
                case DiagnosticsCategory.Trace:
                    return new TraceReporter(transport, resource, options);
                case DiagnosticsCategory.Performance:
                    return new PerformanceReporter(transport, resource, options);
                default:
                    return new AnalyticsReporter(transport, resource, options);
            }
        }

        /// <summary>运行时关闭回调(清采集上下文;契约:关闭即清空)。</summary>
        public virtual void OnDisabled() { }

        protected void Post(object payload)
        {
            PostRaw(Json.Serialize(payload));
        }

        // 显式弃置任务:内部吞掉一切异常(网络错/非 2xx 都不反噬调用方)。
        protected void PostRaw(string json)
        {
            _ = SendAsync(json);
        }

        async Task SendAsync(string json)
        {
            try
            {
                var req = new TransportRequest
                {
                    Method = "POST",
                    Url = Endpoint,
                    JsonBody = json,
                    Headers = new Dictionary<string, string> { { "Content-Type", "application/json" } },
                    TimeoutMs = 10000,
                };
                await Transport.SendAsync(req, CancellationToken.None).ConfigureAwait(false);
                // 2xx = 受理(响应体丢弃);其余丢弃该条——不重试、不缓存(红线 6)。
            }
            catch
            {
                // 网络层异常静默丢弃:诊断数据可丢,游戏不可断。
            }
        }
    }

    // Crash / Error:breadcrumb 环形 20 条,报告即随附并清空。
    sealed class CrashReporter : HttpReporter, ICrashDiagnostics
    {
        const int MaxBreadcrumbs = 20;
        readonly object _gate = new object();
        readonly Queue<string> _breadcrumbs = new Queue<string>();

        public CrashReporter(ITransport transport, DiagnosticsResource resource,
            DiagnosticsCategoryOptions options)
            : base(transport, resource, options.Endpoint) { }

        public void Breadcrumb(string text)
        {
            if (string.IsNullOrEmpty(text))
            {
                return;
            }
            lock (_gate)
            {
                _breadcrumbs.Enqueue(text);
                while (_breadcrumbs.Count > MaxBreadcrumbs)
                {
                    _breadcrumbs.Dequeue();
                }
            }
        }

        public void Report(string message, string stack, string traceId)
        {
            string[] crumbs;
            lock (_gate)
            {
                crumbs = _breadcrumbs.ToArray();
                _breadcrumbs.Clear(); // 已随本次报告送出
            }
            Post(new
            {
                Resource.SessionId,
                Resource.Platform,
                Resource.AppVersion,
                Resource.EngineVersion,
                Resource.OsVersion,
                Resource.DeviceModel,
                message,
                stack,
                breadcrumbs = crumbs,
                traceId,
            });
        }

        public override void OnDisabled()
        {
            lock (_gate)
            {
                _breadcrumbs.Clear();
            }
        }
    }

    // Trace:URL 只记 path 不记 query(契约数据范围);空名 span 无意义,丢弃。
    sealed class TraceReporter : HttpReporter, ITraceDiagnostics
    {
        public TraceReporter(ITransport transport, DiagnosticsResource resource,
            DiagnosticsCategoryOptions options)
            : base(transport, resource, options.Endpoint) { }

        public void Span(string name, long durationMs, string path)
        {
            if (string.IsNullOrEmpty(name))
            {
                return;
            }
            Post(new
            {
                // resource 固定属性集;未采集的字段不下发(契约:字段缺省即未设置)。
                @resource = ResourceAttrs(),
                name,
                durationMs,
                path = StripQuery(path),
            });
        }

        static string StripQuery(string path)
        {
            if (string.IsNullOrEmpty(path))
            {
                return path;
            }
            var i = path.IndexOf('?');
            return i < 0 ? path : path.Substring(0, i);
        }

        // 固定集逐项补(null 字段不入 wire)。
        Dictionary<string, string> ResourceAttrs()
        {
            var attrs = new Dictionary<string, string> { { "service.name", "courier.sdk" } };
            if (Resource.GameId != null) attrs["courier.game_id"] = Resource.GameId;
            if (Resource.Env != null) attrs["courier.env"] = Resource.Env;
            if (Resource.Platform != null) attrs["platform"] = Resource.Platform;
            if (Resource.AppVersion != null) attrs["app.version"] = Resource.AppVersion;
            if (Resource.EngineVersion != null) attrs["engine.version"] = Resource.EngineVersion;
            return attrs;
        }
    }

    // Performance:三个注册指标(契约指标名注册表),维度 platform + app.version。
    sealed class PerformanceReporter : HttpReporter, IPerformanceDiagnostics
    {
        public PerformanceReporter(ITransport transport, DiagnosticsResource resource,
            DiagnosticsCategoryOptions options)
            : base(transport, resource, options.Endpoint) { }

        public void Startup(long ms) { Post(Metric("startup_duration_ms", ms)); }
        public void Jank(long ms) { Post(Metric("frame_jank_ms", ms)); }
        public void Rtt(long ms) { Post(Metric("network_rtt_ms", ms)); }

        object Metric(string metric, long value)
        {
            return new
            {
                metric,
                value,
                dims = new { Resource.Platform, Resource.AppVersion },
            };
        }
    }

    // Analytics:props 平铺、原始类型、单条 ≤1KB;违例 = 使用方错误(抛参数错)。
    // props 键名由接入方登记,JObject 原样平铺(不走 camelCase resolver,不改接入方键)。
    sealed class AnalyticsReporter : HttpReporter, IAnalyticsDiagnostics
    {
        const int MaxEventName = 64;
        const int MaxBodyBytes = 1024;

        public AnalyticsReporter(ITransport transport, DiagnosticsResource resource,
            DiagnosticsCategoryOptions options)
            : base(transport, resource, options.Endpoint) { }

        public void Track(string eventName, IDictionary<string, object> props)
        {
            var name = eventName == null ? "" : eventName.Trim();
            if (name.Length < 1 || name.Length > MaxEventName)
            {
                throw new System.ArgumentException(
                    "eventName 须为 1-64 字符(修剪后),契约 diagnostics.md");
            }
            var body = new JObject { { "eventName", name } };
            if (!string.IsNullOrEmpty(Resource.SessionId))
            {
                body["sessionId"] = Resource.SessionId;
            }
            if (props != null)
            {
                foreach (var kv in props)
                {
                    if (string.IsNullOrEmpty(kv.Key))
                    {
                        throw new System.ArgumentException("props 键须非空,契约 diagnostics.md");
                    }
                    body[kv.Key] = ToToken(kv.Key, kv.Value);
                }
            }
            var json = body.ToString(Newtonsoft.Json.Formatting.None);
            if (Encoding.UTF8.GetByteCount(json) > MaxBodyBytes)
            {
                throw new System.ArgumentException(
                    "埋点单条总大小须 ≤ 1KB(含 eventName/sessionId/props),契约 diagnostics.md");
            }
            PostRaw(json); // 已是定稿 wire(JObject 原样键),不再过 resolver
        }

        // 原始类型白名单(string/bool/整型/浮点);其余 = 使用方错误。
        static JToken ToToken(string key, object value)
        {
            if (value is string s) return new JValue(s);
            if (value is bool b) return new JValue(b);
            if (value is int || value is long || value is short || value is byte
                || value is uint || value is ulong || value is ushort || value is sbyte)
            {
                return new JValue(System.Convert.ToInt64(value));
            }
            if (value is double || value is float || value is decimal)
            {
                return new JValue(System.Convert.ToDouble(value));
            }
            throw new System.ArgumentException(
                "props 值仅原始类型(string/number/bool):键 " + key);
        }
    }
}
