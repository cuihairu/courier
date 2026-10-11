// 内置自有端点直发实现(契约「上报 Schema 与数据范围」wire;unity HttpReporters 同构)。
// 单条一请求,v1 直发不缓冲;失败(网络错/非 2xx)静默丢弃——诊断永不反噬游戏(红线 6)。
// Sentry / OTLP 适配由接入方按同一接口接生态 SDK,不在此处。
import type { Transport, TransportRequest } from "../core/transport.ts";
import type { DiagnosticsResource } from "./diagnosticsTypes.ts";

/** 单条一请求 fire-and-forget:请求留痕在 transport,一切异常就地吞掉。 */
export class HttpReporterBase {
  protected readonly transport: Transport;
  protected readonly resource: DiagnosticsResource;
  protected readonly endpoint: string;

  constructor(transport: Transport, resource: DiagnosticsResource, endpoint: string) {
    this.transport = transport;
    this.resource = resource;
    this.endpoint = endpoint;
  }

  /** 运行时关闭回调(清采集上下文;契约:关闭即清空)。 */
  onDisabled(): void {}

  protected post(payload: Record<string, unknown>): void {
    this.postRaw(JSON.stringify(payload));
  }

  // 显式弃置任务:内部吞掉一切异常(网络错/非 2xx 都不反噬调用方)。
  protected postRaw(json: string): void {
    void this.sendAsync(json).catch(() => {});
  }

  private async sendAsync(json: string): Promise<void> {
    const request: TransportRequest = {
      method: "POST",
      url: this.endpoint,
      headers: { "Content-Type": "application/json" },
      jsonBody: json,
      timeoutMs: 10000,
    };
    await this.transport.send(request);
    // 2xx = 受理(响应体丢弃);其余丢弃该条——不重试、不缓存(红线 6)。
  }
}

// Crash / Error:breadcrumb 环形 20 条,报告即随附并清空。
export class CrashReporter extends HttpReporterBase {
  static readonly MaxBreadcrumbs = 20;
  private breadcrumbs: string[] = [];

  breadcrumb(text: string): void {
    if (text == null || text === "") {
      return;
    }
    this.breadcrumbs.push(text);
    while (this.breadcrumbs.length > CrashReporter.MaxBreadcrumbs) {
      this.breadcrumbs.shift();
    }
  }

  report(message: string, stack: string | null, traceId: string | null): void {
    const crumbs = this.breadcrumbs;
    this.breadcrumbs = []; // 已随本次报告送出
    // 字段缺省即不下发(契约「空值与枚举」)。
    const payload: Record<string, unknown> = { message, stack, breadcrumbs: crumbs };
    if (this.resource.sessionId != null) payload.sessionId = this.resource.sessionId;
    if (this.resource.platform != null) payload.platform = this.resource.platform;
    if (this.resource.appVersion != null) payload.appVersion = this.resource.appVersion;
    if (this.resource.engineVersion != null) payload.engineVersion = this.resource.engineVersion;
    if (this.resource.osVersion != null) payload.osVersion = this.resource.osVersion;
    if (this.resource.deviceModel != null) payload.deviceModel = this.resource.deviceModel;
    if (traceId != null) payload.traceId = traceId;
    this.post(payload);
  }

  override onDisabled(): void {
    this.breadcrumbs = []; // 关闭即清空(契约红线 4)
  }
}

// Trace:URL 只记 path 不记 query(契约数据范围);空名 span 无意义,丢弃。
export class TraceReporter extends HttpReporterBase {
  span(name: string, durationMs: number, path: string): void {
    if (name == null || name === "") {
      return;
    }
    this.post({
      // resource 固定属性集;未采集的字段不入 wire(契约:字段缺省即未设置)。
      resource: resourceAttrs(this.resource),
      name,
      durationMs,
      path: stripQuery(path),
    });
  }
}

function stripQuery(path: string): string {
  if (path == null || path === "") {
    return path;
  }
  const i = path.indexOf("?");
  return i < 0 ? path : path.slice(0, i);
}

// 固定集逐项补(null 字段不入 wire)。
function resourceAttrs(resource: DiagnosticsResource): Record<string, string> {
  const attrs: Record<string, string> = { "service.name": "courier.sdk" };
  if (resource.gameId != null) attrs["courier.game_id"] = resource.gameId;
  if (resource.env != null) attrs["courier.env"] = resource.env;
  if (resource.platform != null) attrs["platform"] = resource.platform;
  if (resource.appVersion != null) attrs["app.version"] = resource.appVersion;
  if (resource.engineVersion != null) attrs["engine.version"] = resource.engineVersion;
  return attrs;
}

// Performance:三个注册指标(契约指标名注册表),维度 platform + appVersion。
export class PerformanceReporter extends HttpReporterBase {
  startup(ms: number): void {
    this.post(metric("startup_duration_ms", ms, this.resource));
  }

  jank(ms: number): void {
    this.post(metric("frame_jank_ms", ms, this.resource));
  }

  rtt(ms: number): void {
    this.post(metric("network_rtt_ms", ms, this.resource));
  }
}

function metric(name: string, value: number, resource: DiagnosticsResource): Record<string, unknown> {
  const dims: Record<string, string> = {};
  if (resource.platform != null) dims.platform = resource.platform;
  if (resource.appVersion != null) dims.appVersion = resource.appVersion;
  return { metric: name, value, dims };
}

const MaxEventName = 64;
const MaxBodyBytes = 1024;
const encoder = new TextEncoder();

// Analytics:props 平铺、原始类型(string/number/bool)、单条 ≤1KB;
// 违例 = 使用方错误(抛参数错,不静默截断)。props 键名接入方登记,原样平铺不改写。
export class AnalyticsReporter extends HttpReporterBase {
  track(eventName: string, props: Record<string, string | number | boolean> | null): void {
    const name = eventName == null ? "" : eventName.trim();
    if (name.length < 1 || name.length > MaxEventName) {
      throw new Error("eventName 须为 1-64 字符(修剪后),契约 diagnostics.md");
    }
    const payload: Record<string, unknown> = { eventName: name };
    if (this.resource.sessionId != null) {
      payload.sessionId = this.resource.sessionId;
    }
    if (props != null) {
      for (const key of Object.keys(props)) {
        if (key === "") {
          throw new Error("props 键须非空,契约 diagnostics.md");
        }
        const value = props[key];
        // 值类型运行时校验:TS 类型在 erasable 语法下会被剥掉,契约约束须落地到运行时。
        if (typeof value !== "string" && typeof value !== "number" && typeof value !== "boolean") {
          throw new Error("props 值须为 string/number/boolean,契约 diagnostics.md");
        }
        payload[key] = value; // 平铺原样键(不走 camelCase 改写)
      }
    }
    const json = JSON.stringify(payload);
    if (encoder.encode(json).length > MaxBodyBytes) {
      throw new Error("埋点单条总大小须 ≤ 1KB(含 eventName/sessionId/props),契约 diagnostics.md");
    }
    this.postRaw(json);
  }
}
