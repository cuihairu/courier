// M3 诊断可选包测试(契约 diagnostics.md Frozen v1 验收):四类默认零上报;
// 逐类开启逐类生效;运行时关闭立即归零;wire schema(路径去 query、指标注册表、
// props 原样键、≤1KB 校验);上报失败静默不反噬。
using System;
using System.Collections.Generic;
using System.Threading;
using System.Threading.Tasks;
using Courier.Core;
using Courier.Diagnostics;
using Newtonsoft.Json.Linq;
using Xunit;

namespace Courier.DiagnosticsTests
{
    // 极简假通道:同步完成,请求全部留痕;Fallback 可注入故障。
    sealed class FakeTransport : ITransport
    {
        public readonly List<TransportRequest> Requests = new List<TransportRequest>();
        public Func<TransportRequest, TransportResponse> Fallback;

        public Task<TransportResponse> SendAsync(TransportRequest request,
            CancellationToken cancellationToken)
        {
            Requests.Add(request);
            if (Fallback != null)
            {
                return Task.FromResult(Fallback(request));
            }
            return Task.FromResult(new TransportResponse { StatusCode = 200 });
        }
    }

    public class DiagnosticsTests
    {
        static DiagnosticsResource Resource()
        {
            return new DiagnosticsResource
            {
                GameId = "game_demo",
                Env = "prod",
                Platform = "ios",
                AppVersion = "2.0.0",
                EngineVersion = "2022.3",
                SessionId = "ses_1",
            };
        }

        static DiagnosticsOptions Only(DiagnosticsCategory cat, string endpoint = "https://diag.example.com/x")
        {
            var o = new DiagnosticsOptions();
            o.Of(cat).Enabled = true;
            o.Of(cat).Endpoint = endpoint;
            return o;
        }

        [Fact]
        public void AllOffByDefault_ZeroSends_NoInstances()
        {
            var transport = new FakeTransport();
            var hub = DiagnosticsHub.Create(new DiagnosticsOptions(), transport, Resource());

            Assert.False(hub.IsEnabled(DiagnosticsCategory.Crash)); // 默认全关
            Assert.False(hub.IsEnabled(DiagnosticsCategory.Trace));
            Assert.False(hub.IsEnabled(DiagnosticsCategory.Performance));
            Assert.False(hub.IsEnabled(DiagnosticsCategory.Analytics));

            // 调遍四类接口:一次发送都不发生(验收:默认零上报)。
            hub.Crash.Report("boom", "stack", null);
            hub.Crash.Breadcrumb("进入主城");
            hub.Trace.Span("op", 5, "/p");
            hub.Performance.Startup(100);
            hub.Analytics.Track("evt", null);
            Assert.Empty(transport.Requests);
        }

        [Fact]
        public void CreateWithNull_ReturnsDisabled()
        {
            Assert.Same(DiagnosticsHub.Disabled,
                DiagnosticsHub.Create(null, new FakeTransport(), null));
        }

        [Fact]
        public void EnabledWithoutEndpoint_StillOff()
        {
            var transport = new FakeTransport();
            var o = new DiagnosticsOptions();
            o.Crash.Enabled = true; // 只开开关,没配端点 = 未配置(安全侧归零)
            var hub = DiagnosticsHub.Create(o, transport, Resource());

            Assert.False(hub.IsEnabled(DiagnosticsCategory.Crash));
            hub.Crash.Report("boom", "stack", null);
            Assert.Empty(transport.Requests);
        }

        [Fact]
        public void OnlyAnalyticsEnabled_OtherCategoriesZero()
        {
            var transport = new FakeTransport();
            var hub = DiagnosticsHub.Create(Only(DiagnosticsCategory.Analytics,
                "https://diag.example.com/events"), transport, Resource());

            hub.Analytics.Track("level_complete", new Dictionary<string, object> { { "level", 7 } });
            // 其余三类照调,零上报(验收:逐类开启逐类生效)。
            hub.Crash.Report("boom", "stack", null);
            hub.Trace.Span("op", 5, "/p");
            hub.Performance.Startup(100);

            var req = Assert.Single(transport.Requests);
            Assert.Equal("POST", req.Method);
            Assert.Equal("https://diag.example.com/events", req.Url);
            Assert.Equal("application/json", req.Headers["Content-Type"]);
            var body = JObject.Parse(req.JsonBody);
            Assert.Equal("level_complete", (string)body["eventName"]);
            Assert.Equal(7, (int)body["level"]);
            Assert.Equal("ses_1", (string)body["sessionId"]);
        }

        [Fact]
        public void CrashReport_WireAndBreadcrumbRing()
        {
            var transport = new FakeTransport();
            var hub = DiagnosticsHub.Create(Only(DiagnosticsCategory.Crash,
                "https://diag.example.com/crash"), transport, Resource());

            for (var i = 1; i <= 21; i++)
            {
                hub.Crash.Breadcrumb("b" + i); // 环形 20:b1 被挤出
            }
            hub.Crash.Report("boom", "NullRef at ...", "trace_9");

            var body = JObject.Parse(Assert.Single(transport.Requests).JsonBody);
            Assert.Equal("boom", (string)body["message"]);
            Assert.Equal("NullRef at ...", (string)body["stack"]);
            Assert.Equal("trace_9", (string)body["traceId"]);
            Assert.Equal("ios", (string)body["platform"]);
            Assert.Equal("2.0.0", (string)body["appVersion"]);
            var crumbs = (JArray)body["breadcrumbs"];
            Assert.Equal(20, crumbs.Count);
            Assert.Equal("b2", (string)crumbs[0]);   // b1 已被挤出
            Assert.Equal("b21", (string)crumbs[19]);
            Assert.Null(body["traceId_x"]); // 无中生有字段不出现

            // 报告送出后 breadcrumb 清空:再报一次无 crumbs。
            transport.Requests.Clear();
            hub.Crash.Report("again", "s", null);
            var body2 = JObject.Parse(Assert.Single(transport.Requests).JsonBody);
            Assert.Equal(0, ((JArray)body2["breadcrumbs"]).Count);
        }

        [Fact]
        public void TraceSpan_PathStripsQuery_ResourceAttrsFixed()
        {
            var transport = new FakeTransport();
            var hub = DiagnosticsHub.Create(Only(DiagnosticsCategory.Trace,
                "https://diag.example.com/trace"), transport, Resource());

            hub.Trace.Span("support.ticket.create", 42, "/v1/support/tickets?token=secret");

            var body = JObject.Parse(Assert.Single(transport.Requests).JsonBody);
            // URL 只记 path 不记 query(token 不进诊断)
            Assert.Equal("/v1/support/tickets", (string)body["path"]);
            Assert.Equal(42, (int)body["durationMs"]);
            Assert.Equal("support.ticket.create", (string)body["name"]);
            var res = (JObject)body["resource"];
            Assert.Equal("courier.sdk", (string)res["service.name"]);
            Assert.Equal("game_demo", (string)res["courier.game_id"]);
            Assert.Equal("prod", (string)res["courier.env"]);
            Assert.Equal("ios", (string)res["platform"]);
        }

        [Fact]
        public void Performance_MetricRegistry_OnlyTwoDims()
        {
            var transport = new FakeTransport();
            var hub = DiagnosticsHub.Create(Only(DiagnosticsCategory.Performance,
                "https://diag.example.com/metrics"), transport, Resource());

            hub.Performance.Startup(1820);
            hub.Performance.Jank(33);
            hub.Performance.Rtt(95);

            Assert.Equal(3, transport.Requests.Count);
            Assert.Equal("startup_duration_ms", (string)JObject.Parse(transport.Requests[0].JsonBody)["metric"]);
            Assert.Equal("frame_jank_ms", (string)JObject.Parse(transport.Requests[1].JsonBody)["metric"]);
            Assert.Equal("network_rtt_ms", (string)JObject.Parse(transport.Requests[2].JsonBody)["metric"]);
            var body = JObject.Parse(transport.Requests[2].JsonBody);
            Assert.Equal(95, (int)body["value"]);
            var dims = (JObject)body["dims"];
            Assert.Equal("ios", (string)dims["platform"]);       // 维度只有 platform + appVersion
            Assert.Equal("2.0.0", (string)dims["appVersion"]);
            Assert.Equal(2, dims.Count);
        }

        [Fact]
        public void Analytics_Validation_CallerErrors()
        {
            var transport = new FakeTransport();
            var hub = DiagnosticsHub.Create(Only(DiagnosticsCategory.Analytics), transport, Resource());

            Assert.Throws<ArgumentException>(() => hub.Analytics.Track(null, null));
            Assert.Throws<ArgumentException>(() => hub.Analytics.Track("   ", null));
            Assert.Throws<ArgumentException>(() => hub.Analytics.Track(new string('e', 65), null));
            // props 值非原始类型 / 键为空 = 使用方错误
            Assert.Throws<ArgumentException>(() => hub.Analytics.Track("evt",
                new Dictionary<string, object> { { "obj", new object() } }));
            Assert.Throws<ArgumentException>(() => hub.Analytics.Track("evt",
                new Dictionary<string, object> { { "", 1 } }));
            // 单条 > 1KB 拒绝(不静默截断)
            Assert.Throws<ArgumentException>(() => hub.Analytics.Track("evt",
                new Dictionary<string, object> { { "blob", new string('x', 1100) } }));
            Assert.Empty(transport.Requests); // 违例条目一次都没发

            // 边界合法:64 字符名 + 原样键(不走 camelCase 改写)
            hub.Analytics.Track(new string('e', 64),
                new Dictionary<string, object> { { "Level_ID", 3 }, { "ratio", 0.5 }, { "ok", true } });
            var body = JObject.Parse(Assert.Single(transport.Requests).JsonBody);
            Assert.Equal(new string('e', 64), (string)body["eventName"]);
            Assert.Equal(3, (int)body["Level_ID"]);
            Assert.Equal(0.5, (double)body["ratio"]);
            Assert.True((bool)body["ok"]);
        }

        [Fact]
        public void ReportFailure_SwallowedNeverThrows()
        {
            var transport = new FakeTransport
            {
                Fallback = _ => throw new TransportException("net down", null), // 网络层异常
            };
            var hub = DiagnosticsHub.Create(Only(DiagnosticsCategory.Analytics), transport, Resource());

            hub.Analytics.Track("evt", null); // 不抛:诊断永不反噬游戏

            var transport500 = new FakeTransport { Fallback = _ => new TransportResponse { StatusCode = 500 } };
            var hub2 = DiagnosticsHub.Create(Only(DiagnosticsCategory.Crash), transport500, Resource());
            hub2.Crash.Report("boom", "s", null); // 非 2xx 同样静默丢弃
        }

        [Fact]
        public void RuntimeDisable_StopsImmediately_AndClearsContext()
        {
            var transport = new FakeTransport();
            var hub = DiagnosticsHub.Create(Only(DiagnosticsCategory.Analytics), transport, Resource());

            hub.Analytics.Track("evt", null);
            Assert.Single(transport.Requests);

            hub.SetEnabled(DiagnosticsCategory.Analytics, false);
            Assert.False(hub.IsEnabled(DiagnosticsCategory.Analytics));
            hub.Analytics.Track("evt", null);
            Assert.Single(transport.Requests); // 关闭后零上报(验收)

            // crash 上下文随关闭清空:重开后报告无 breadcrumb。
            var crashHub = DiagnosticsHub.Create(Only(DiagnosticsCategory.Crash), transport, Resource());
            crashHub.Crash.Breadcrumb("ctx");
            crashHub.SetEnabled(DiagnosticsCategory.Crash, false);
            crashHub.SetEnabled(DiagnosticsCategory.Crash, true);
            crashHub.Crash.Report("boom", "s", null);
            var last = JObject.Parse(transport.Requests[transport.Requests.Count - 1].JsonBody);
            Assert.Equal(0, ((JArray)last["breadcrumbs"]).Count);
        }

        [Fact]
        public void RuntimeEnable_RequiresEndpoint()
        {
            var transport = new FakeTransport();
            var o = new DiagnosticsOptions();
            o.Trace.Endpoint = "https://diag.example.com/trace"; // 只配端点不开开关
            var hub = DiagnosticsHub.Create(o, transport, Resource());
            Assert.False(hub.IsEnabled(DiagnosticsCategory.Trace));

            hub.SetEnabled(DiagnosticsCategory.Trace, true); // 端点已配 → 可开
            Assert.True(hub.IsEnabled(DiagnosticsCategory.Trace));
            hub.Trace.Span("op", 1, "/p");
            Assert.Single(transport.Requests);

            hub.SetEnabled(DiagnosticsCategory.Analytics, true); // 没端点 → 保持关
            Assert.False(hub.IsEnabled(DiagnosticsCategory.Analytics));
            hub.Analytics.Track("evt", null);
            Assert.Single(transport.Requests);
        }
    }
}
