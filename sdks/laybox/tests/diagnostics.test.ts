// 诊断可选包测试(契约 diagnostics.md Frozen v1 验收;unity DiagnosticsTests 同构):
// 四类默认零上报;逐类开启逐类生效;运行时关闭立即归零;wire schema(路径去 query、
// 指标注册表、props 原样键、≤1KB 校验);上报失败静默不反噬。
import assert from "node:assert/strict";
import { test } from "node:test";
import type { Transport, TransportRequest, TransportResponse } from "../src/core/transport.ts";
import {
  DiagnosticsCategories,
  type DiagnosticsCategory,
  type DiagnosticsOptions,
  type DiagnosticsResource,
  newDiagnosticsOptions,
} from "../src/diagnostics/diagnosticsTypes.ts";
import { DiagnosticsHub } from "../src/diagnostics/diagnosticsHub.ts";

// 极简假通道:同步完成,请求全部留痕;fallback 可注入故障。
class FakeTransport implements Transport {
  requests: TransportRequest[] = [];
  fallback: ((req: TransportRequest) => TransportResponse) | null = null;

  async send(request: TransportRequest): Promise<TransportResponse> {
    this.requests.push(request);
    if (this.fallback != null) {
      return this.fallback(request);
    }
    return { statusCode: 200, headers: {}, body: "" };
  }
}

function resource(): DiagnosticsResource {
  return {
    gameId: "game_demo",
    env: "prod",
    platform: "ios",
    appVersion: "2.0.0",
    engineVersion: "2022.3",
    sessionId: "ses_1",
  };
}

function only(category: DiagnosticsCategory, endpoint: string | null = "https://diag.example.com/x"): DiagnosticsOptions {
  const o = newDiagnosticsOptions();
  o[category].enabled = true;
  o[category].endpoint = endpoint;
  return o;
}

const flush = (): Promise<void> => new Promise((resolve) => setTimeout(resolve, 0));

test("diagnostics:默认全关零上报;调遍四类一次发送都不发生", () => {
  const transport = new FakeTransport();
  const hub = DiagnosticsHub.create(newDiagnosticsOptions(), transport, resource());

  for (const category of DiagnosticsCategories) {
    assert.equal(hub.isEnabled(category), false); // 默认全关
  }

  // 调遍四类接口:一次发送都不发生(验收:默认零上报)。
  hub.crash.report("boom", "stack", null);
  hub.crash.breadcrumb("进入主城");
  hub.trace.span("op", 5, "/p");
  hub.performance.startup(100);
  hub.analytics.track("evt", null);
  assert.equal(transport.requests.length, 0);
});

test("diagnostics:options/transport 为 null → Disabled 共享实例", () => {
  assert.equal(DiagnosticsHub.create(null, new FakeTransport(), null), DiagnosticsHub.Disabled);
});

test("diagnostics:只开开关没配端点 = 未配置(安全侧归零)", () => {
  const transport = new FakeTransport();
  const o = newDiagnosticsOptions();
  o.crash.enabled = true;
  const hub = DiagnosticsHub.create(o, transport, resource());

  assert.equal(hub.isEnabled("crash"), false);
  hub.crash.report("boom", "stack", null);
  assert.equal(transport.requests.length, 0);
});

test("diagnostics:只开 analytics,其余三类照调零上报;wire 形状", () => {
  const transport = new FakeTransport();
  const hub = DiagnosticsHub.create(only("analytics", "https://diag.example.com/events"), transport, resource());

  hub.analytics.track("level_complete", { level: 7 });
  // 其余三类照调,零上报(验收:逐类开启逐类生效)。
  hub.crash.report("boom", "stack", null);
  hub.trace.span("op", 5, "/p");
  hub.performance.startup(100);

  assert.equal(transport.requests.length, 1);
  const req = transport.requests[0];
  assert.equal(req.method, "POST");
  assert.equal(req.url, "https://diag.example.com/events");
  assert.equal((req.headers as Record<string, string>)["Content-Type"], "application/json");
  const body = JSON.parse(req.jsonBody ?? "{}") as Record<string, unknown>;
  assert.equal(body.eventName, "level_complete");
  assert.equal(body.level, 7);
  assert.equal(body.sessionId, "ses_1");
});

test("diagnostics:crash breadcrumb 环形 20;报告随附并清空;未采集字段不入 wire", () => {
  const transport = new FakeTransport();
  const hub = DiagnosticsHub.create(only("crash", "https://diag.example.com/crash"), transport, resource());

  for (let i = 1; i <= 21; i++) {
    hub.crash.breadcrumb("b" + i); // 环形 20:b1 被挤出
  }
  hub.crash.report("boom", "NullRef at ...", "trace_9");

  const body = JSON.parse(transport.requests[0].jsonBody ?? "{}") as Record<string, unknown>;
  assert.equal(body.message, "boom");
  assert.equal(body.stack, "NullRef at ...");
  assert.equal(body.traceId, "trace_9");
  assert.equal(body.platform, "ios");
  assert.equal(body.appVersion, "2.0.0");
  const crumbs = body.breadcrumbs as string[];
  assert.equal(crumbs.length, 20);
  assert.equal(crumbs[0], "b2"); // b1 已被挤出
  assert.equal(crumbs[19], "b21");
  assert.equal("osVersion" in body, false); // 未采集字段不下发(契约:缺省即未设置)
  assert.equal("deviceModel" in body, false);

  // 报告送出后 breadcrumb 清空:再报一次无 crumbs。
  transport.requests.length = 0;
  hub.crash.report("again", "s", null);
  const body2 = JSON.parse(transport.requests[0].jsonBody ?? "{}") as Record<string, unknown>;
  assert.equal((body2.breadcrumbs as string[]).length, 0);
});

test("diagnostics:trace 只记 path 去 query(token 不进诊断);resource 固定集", () => {
  const transport = new FakeTransport();
  const hub = DiagnosticsHub.create(only("trace", "https://diag.example.com/trace"), transport, resource());

  hub.trace.span("support.ticket.create", 42, "/v1/support/tickets?token=secret");

  const body = JSON.parse(transport.requests[0].jsonBody ?? "{}") as Record<string, unknown>;
  assert.equal(body.path, "/v1/support/tickets"); // query 剥离
  assert.equal(body.durationMs, 42);
  assert.equal(body.name, "support.ticket.create");
  const res = body.resource as Record<string, string>;
  assert.equal(res["service.name"], "courier.sdk");
  assert.equal(res["courier.game_id"], "game_demo");
  assert.equal(res["courier.env"], "prod");
  assert.equal(res["platform"], "ios");
  assert.equal(res["app.version"], "2.0.0");
});

test("diagnostics:performance 指标注册表;维度只有 platform + appVersion", () => {
  const transport = new FakeTransport();
  const hub = DiagnosticsHub.create(only("performance", "https://diag.example.com/metrics"), transport, resource());

  hub.performance.startup(1820);
  hub.performance.jank(33);
  hub.performance.rtt(95);

  assert.equal(transport.requests.length, 3);
  assert.equal(JSON.parse(transport.requests[0].jsonBody ?? "{}").metric, "startup_duration_ms");
  assert.equal(JSON.parse(transport.requests[1].jsonBody ?? "{}").metric, "frame_jank_ms");
  const body = JSON.parse(transport.requests[2].jsonBody ?? "{}") as Record<string, unknown>;
  assert.equal(body.metric, "network_rtt_ms");
  assert.equal(body.value, 95);
  const dims = body.dims as Record<string, string>;
  assert.equal(dims.platform, "ios"); // 维度只有 platform + appVersion
  assert.equal(dims.appVersion, "2.0.0");
  assert.equal(Object.keys(dims).length, 2);
});

test("diagnostics:analytics 校验是使用方错误(违例一次不发);边界合法原样键", () => {
  const transport = new FakeTransport();
  const hub = DiagnosticsHub.create(only("analytics"), transport, resource());

  assert.throws(() => hub.analytics.track(null as unknown as string, null)); // 空名
  assert.throws(() => hub.analytics.track("   ", null)); // 修剪后空
  assert.throws(() => hub.analytics.track("e".repeat(65), null)); // 超 64
  // props 值非原始类型 / 键为空 = 使用方错误
  assert.throws(() => hub.analytics.track("evt", { obj: { nested: 1 } as unknown as string }));
  assert.throws(() => hub.analytics.track("evt", { "": 1 }));
  // 单条 > 1KB 拒绝(不静默截断)
  assert.throws(() => hub.analytics.track("evt", { blob: "x".repeat(1100) }));
  assert.equal(transport.requests.length, 0); // 违例条目一次都没发

  // 边界合法:64 字符名 + 原样键(不走 camelCase 改写)
  hub.analytics.track("e".repeat(64), { Level_ID: 3, ratio: 0.5, ok: true });
  assert.equal(transport.requests.length, 1);
  const body = JSON.parse(transport.requests[0].jsonBody ?? "{}") as Record<string, unknown>;
  assert.equal(body.eventName, "e".repeat(64));
  assert.equal(body.Level_ID, 3);
  assert.equal(body.ratio, 0.5);
  assert.equal(body.ok, true);
});

test("diagnostics:上报失败(网络错/非 2xx)静默不反噬", async () => {
  const transport = new FakeTransport();
  transport.fallback = () => {
    throw new Error("net down"); // 网络层异常
  };
  const hub = DiagnosticsHub.create(only("analytics"), transport, resource());
  hub.analytics.track("evt", null); // 不抛:诊断永不反噬游戏
  await flush();

  const transport500 = new FakeTransport();
  transport500.fallback = () => ({ statusCode: 500, headers: {}, body: "" });
  const hub2 = DiagnosticsHub.create(only("crash"), transport500, resource());
  hub2.crash.report("boom", "s", null); // 非 2xx 同样静默丢弃
  await flush();
});

test("diagnostics:运行时关闭立即归零;crash 上下文随关闭清空", () => {
  const transport = new FakeTransport();
  const hub = DiagnosticsHub.create(only("analytics"), transport, resource());

  hub.analytics.track("evt", null);
  assert.equal(transport.requests.length, 1);

  hub.setEnabled("analytics", false);
  assert.equal(hub.isEnabled("analytics"), false);
  hub.analytics.track("evt", null);
  assert.equal(transport.requests.length, 1); // 关闭后零上报(验收)

  // crash 上下文随关闭清空:重开后报告无 breadcrumb。
  const crashHub = DiagnosticsHub.create(only("crash"), transport, resource());
  crashHub.crash.breadcrumb("ctx");
  crashHub.setEnabled("crash", false);
  crashHub.setEnabled("crash", true);
  crashHub.crash.report("boom", "s", null);
  const last = JSON.parse(transport.requests[transport.requests.length - 1].jsonBody ?? "{}") as Record<string, unknown>;
  assert.equal((last.breadcrumbs as string[]).length, 0);
});

test("diagnostics:运行时开启要求端点已配;没端点保持关", () => {
  const transport = new FakeTransport();
  const o = newDiagnosticsOptions();
  o.trace.endpoint = "https://diag.example.com/trace"; // 只配端点不开开关
  const hub = DiagnosticsHub.create(o, transport, resource());
  assert.equal(hub.isEnabled("trace"), false);

  hub.setEnabled("trace", true); // 端点已配 → 可开
  assert.equal(hub.isEnabled("trace"), true);
  hub.trace.span("op", 1, "/p");
  assert.equal(transport.requests.length, 1);

  hub.setEnabled("analytics", true); // 没端点 → 保持关
  assert.equal(hub.isEnabled("analytics"), false);
  hub.analytics.track("evt", null);
  assert.equal(transport.requests.length, 1);
});
