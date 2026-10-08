// 诊断门面(契约 diagnostics.md「SDK 侧接口语义」)。
// 零开销哲学的落点:options 为 null / transport 为 null → 返回共享 Disabled
// (零实例零线程零队列);全关 options → 轻量枢纽(行为 no-op,reporter 仍懒建,
// 为运行时开闸保留端点);某类未开启 → 该类属性返回共享 no-op 实现,不建实例。
// 运行时 SetEnabled:关立即生效(后续调用 no-op,breadcrumb 清空);开需要端点已配,
// 未配端点保持 no-op(契约:只开端点不开启关 = 关,开启必须同时配置端点)。
using System;
using Courier.Core;

namespace Courier.Diagnostics
{
    public sealed class DiagnosticsHub
    {
        /// <summary>全关共享实例:所有属性为 no-op(未配置即零开销)。</summary>
        public static readonly DiagnosticsHub Disabled = new DiagnosticsHub(null, null, null);

        readonly ITransport _transport;
        readonly DiagnosticsResource _resource;
        readonly DiagnosticsCategoryOptions[] _options; // (int)category 索引,运行时可变
        readonly HttpReporter[] _reporters;             // 懒建:开启前不实例化

        DiagnosticsHub(ITransport transport, DiagnosticsOptions options, DiagnosticsResource resource)
        {
            _transport = transport;
            _resource = resource ?? new DiagnosticsResource();
            _options = new DiagnosticsCategoryOptions[4];
            _reporters = new HttpReporter[4];
            for (var i = 0; i < 4; i++)
            {
                var o = options != null ? options.Of((DiagnosticsCategory)i) : null;
                // 拷贝为内部状态(运行时 SetEnabled 可变),不改调用方配置对象;
                // 开了开关但没配端点 = 未配置(安全侧归零)。
                _options[i] = new DiagnosticsCategoryOptions
                {
                    Enabled = o != null && o.Enabled && !string.IsNullOrEmpty(o.Endpoint),
                    Endpoint = o != null ? o.Endpoint : null,
                };
            }
        }

        /// <summary>创建诊断门面:未配置(options/transport 为 null)→ Disabled 共享实例;
        /// 其余配置(含全关)建轻量枢纽——行为 no-op,reporter 懒建零实例。</summary>
        public static DiagnosticsHub Create(DiagnosticsOptions options, ITransport transport,
            DiagnosticsResource resource)
        {
            if (transport == null || options == null)
            {
                return Disabled;
            }
            return new DiagnosticsHub(transport, options, resource);
        }

        public ICrashDiagnostics Crash { get { return Get<ICrashDiagnostics>(DiagnosticsCategory.Crash); } }
        public ITraceDiagnostics Trace { get { return Get<ITraceDiagnostics>(DiagnosticsCategory.Trace); } }
        public IPerformanceDiagnostics Performance { get { return Get<IPerformanceDiagnostics>(DiagnosticsCategory.Performance); } }
        public IAnalyticsDiagnostics Analytics { get { return Get<IAnalyticsDiagnostics>(DiagnosticsCategory.Analytics); } }

        public bool IsEnabled(DiagnosticsCategory category)
        {
            return Effective(category);
        }

        /// <summary>运行时开关:关闭立即生效(丢弃未发上下文,v1 无未发缓冲);
        /// 开启要求端点已配置,否则维持关闭。</summary>
        public void SetEnabled(DiagnosticsCategory category, bool enabled)
        {
            var o = _options[(int)category];
            if (!enabled)
            {
                o.Enabled = false;
                var r = _reporters[(int)category];
                if (r != null)
                {
                    r.OnDisabled(); // 清 breadcrumb 等采集上下文(契约:关闭即清空)
                }
                return;
            }
            if (!string.IsNullOrEmpty(o.Endpoint))
            {
                o.Enabled = true;
            }
        }

        // 未开启 → no-op 实现(共享,零实例开销);开启 → 懒建 Http reporter。
        T Get<T>(DiagnosticsCategory category) where T : class
        {
            if (!Effective(category))
            {
                return Noop(category) as T;
            }
            if (_reporters[(int)category] == null)
            {
                _reporters[(int)category] = HttpReporter.Create(category, _transport, _resource,
                    _options[(int)category]);
            }
            return (T)(object)_reporters[(int)category];
        }

        // 有效开启 = 开关开 + 端点在(Disabled 枢纽全 false)。
        bool Effective(DiagnosticsCategory category)
        {
            var o = _options[(int)category];
            return _transport != null && o.Enabled && !string.IsNullOrEmpty(o.Endpoint);
        }

        static object Noop(DiagnosticsCategory category)
        {
            switch (category)
            {
                case DiagnosticsCategory.Crash: return NoopCrash.Instance;
                case DiagnosticsCategory.Trace: return NoopTrace.Instance;
                case DiagnosticsCategory.Performance: return NoopPerformance.Instance;
                default: return NoopAnalytics.Instance;
            }
        }
    }

    // ---- SDK 侧统一接口(契约「SDK 侧接口语义」;全部 fire-and-forget 非阻塞) ----

    /// <summary>Crash / Error:错误上报 + breadcrumb(为下一次报告附加上下文,环形 20 条)。</summary>
    public interface ICrashDiagnostics
    {
        void Report(string message, string stack, string traceId);
        void Breadcrumb(string text);
    }

    /// <summary>Trace:上报已结束的 span;URL 只记 path 不记 query(契约数据范围)。</summary>
    public interface ITraceDiagnostics
    {
        void Span(string name, long durationMs, string path);
    }

    /// <summary>Performance:三个注册指标(startup/frame_jank/network_rtt,ms)。</summary>
    public interface IPerformanceDiagnostics
    {
        void Startup(long ms);
        void Jank(long ms);
        void Rtt(long ms);
    }

    /// <summary>Analytics:埋点;props 仅原始类型,单条 ≤1KB(违例抛参数错,使用方错误)。</summary>
    public interface IAnalyticsDiagnostics
    {
        void Track(string eventName, System.Collections.Generic.IDictionary<string, object> props);
    }

    // ---- no-op 实现(共享单例;零副作用) ----

    sealed class NoopCrash : ICrashDiagnostics
    {
        public static readonly NoopCrash Instance = new NoopCrash();
        NoopCrash() { }
        public void Report(string message, string stack, string traceId) { }
        public void Breadcrumb(string text) { }
    }

    sealed class NoopTrace : ITraceDiagnostics
    {
        public static readonly NoopTrace Instance = new NoopTrace();
        NoopTrace() { }
        public void Span(string name, long durationMs, string path) { }
    }

    sealed class NoopPerformance : IPerformanceDiagnostics
    {
        public static readonly NoopPerformance Instance = new NoopPerformance();
        NoopPerformance() { }
        public void Startup(long ms) { }
        public void Jank(long ms) { }
        public void Rtt(long ms) { }
    }

    sealed class NoopAnalytics : IAnalyticsDiagnostics
    {
        public static readonly NoopAnalytics Instance = new NoopAnalytics();
        NoopAnalytics() { }
        public void Track(string eventName, System.Collections.Generic.IDictionary<string, object> props) { }
    }
}
