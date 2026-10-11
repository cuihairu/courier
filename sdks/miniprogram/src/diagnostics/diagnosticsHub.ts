// 诊断门面(契约 diagnostics.md「SDK 侧接口语义」;unity DiagnosticsHub 同构)。
// 零开销哲学的落点:options 为 null / transport 为 null → 返回共享 Disabled
// (零实例零线程零队列);全关 options → 轻量枢纽(行为 no-op,reporter 懒建,
// 为运行时开闸保留端点);某类未开启 → 该类属性返回共享 no-op 实现,不建实例。
// 运行时 setEnabled:关立即生效(后续调用 no-op,breadcrumb 清空);开需要端点已配,
// 未配端点保持 no-op(契约:只开开关不配端点 = 关,开启必须同时配置端点)。
import type { Transport } from "../core/transport.ts";
import {
  categoryOptionsOf,
  type DiagnosticsCategory,
  type DiagnosticsOptions,
  type DiagnosticsResource,
  newDiagnosticsOptions,
} from "./diagnosticsTypes.ts";
import { AnalyticsReporter, CrashReporter, PerformanceReporter, TraceReporter } from "./httpReporters.ts";

// ---- SDK 侧统一接口(契约「SDK 侧接口语义」;全部 fire-and-forget 非阻塞) ----

/** Crash / Error:错误上报 + breadcrumb(为下一次报告附加上下文,环形 20 条)。 */
export interface CrashDiagnostics {
  report(message: string, stack: string | null, traceId: string | null): void;
  breadcrumb(text: string): void;
}

/** Trace:上报已结束的 span;URL 只记 path 不记 query(契约数据范围)。 */
export interface TraceDiagnostics {
  span(name: string, durationMs: number, path: string): void;
}

/** Performance:三个注册指标(startup/frame_jank/network_rtt,ms)。 */
export interface PerformanceDiagnostics {
  startup(ms: number): void;
  jank(ms: number): void;
  rtt(ms: number): void;
}

/** Analytics:埋点;props 仅原始类型,单条 ≤1KB(违例抛参数错,使用方错误)。 */
export interface AnalyticsDiagnostics {
  track(eventName: string, props: Record<string, string | number | boolean> | null): void;
}

// ---- no-op 实现(共享单例;零副作用) ----

const noopCrash: CrashDiagnostics = { report() {}, breadcrumb() {} };
const noopTrace: TraceDiagnostics = { span() {} };
const noopPerformance: PerformanceDiagnostics = { startup() {}, jank() {}, rtt() {} };
const noopAnalytics: AnalyticsDiagnostics = { track() {} };

function noopOf(category: DiagnosticsCategory): CrashDiagnostics | TraceDiagnostics | PerformanceDiagnostics | AnalyticsDiagnostics {
  switch (category) {
    case "crash": return noopCrash;
    case "trace": return noopTrace;
    case "performance": return noopPerformance;
    default: return noopAnalytics;
  }
}

type AnyReporter = CrashReporter | TraceReporter | PerformanceReporter | AnalyticsReporter;

export class DiagnosticsHub {
  /** 全关共享实例:所有属性为 no-op(未配置即零开销)。 */
  static readonly Disabled: DiagnosticsHub = new DiagnosticsHub(null, null, null);

  private readonly transport: Transport | null;
  private readonly resource: DiagnosticsResource;
  private readonly options: DiagnosticsOptions; // 拷贝为内部状态(运行时可变)
  private readonly reporters: Record<string, AnyReporter | null> = {};

  private constructor(transport: Transport | null, options: DiagnosticsOptions | null, resource: DiagnosticsResource | null) {
    this.transport = transport;
    this.resource = resource ?? {};
    this.options = newDiagnosticsOptions();
    if (options != null) {
      // 拷贝为内部状态(运行时 setEnabled 可变),不改调用方配置对象;
      // 开了开关但没配端点 = 未配置(安全侧归零)。
      for (const category of Object.keys(this.options) as DiagnosticsCategory[]) {
        const o = categoryOptionsOf(options, category);
        const mine = categoryOptionsOf(this.options, category);
        mine.enabled = o.enabled && o.endpoint != null && o.endpoint !== "";
        mine.endpoint = o.endpoint;
      }
    }
  }

  /** 创建诊断门面:未配置(options/transport 为 null)→ Disabled 共享实例;
   *  其余配置(含全关)建轻量枢纽——行为 no-op,reporter 懒建零实例。 */
  static create(options: DiagnosticsOptions | null, transport: Transport | null,
    resource: DiagnosticsResource | null): DiagnosticsHub {
    if (transport == null || options == null) {
      return DiagnosticsHub.Disabled;
    }
    return new DiagnosticsHub(transport, options, resource);
  }

  get crash(): CrashDiagnostics {
    return this.get("crash") as CrashDiagnostics;
  }

  get trace(): TraceDiagnostics {
    return this.get("trace") as TraceDiagnostics;
  }

  get performance(): PerformanceDiagnostics {
    return this.get("performance") as PerformanceDiagnostics;
  }

  get analytics(): AnalyticsDiagnostics {
    return this.get("analytics") as AnalyticsDiagnostics;
  }

  isEnabled(category: DiagnosticsCategory): boolean {
    return this.effective(category);
  }

  /** 运行时开关:关闭立即生效(丢弃未发上下文,v1 无未发缓冲);
   *  开启要求端点已配置,否则维持关闭。 */
  setEnabled(category: DiagnosticsCategory, enabled: boolean): void {
    const o = categoryOptionsOf(this.options, category);
    if (!enabled) {
      o.enabled = false;
      const reporter = this.reporters[category];
      if (reporter != null) {
        reporter.onDisabled(); // 清 breadcrumb 等采集上下文(契约:关闭即清空)
      }
      return;
    }
    if (o.endpoint != null && o.endpoint !== "") {
      o.enabled = true;
    }
  }

  // 未开启 → no-op 实现(共享,零实例开销);开启 → 懒建 Http reporter。
  private get(category: DiagnosticsCategory): CrashDiagnostics | TraceDiagnostics | PerformanceDiagnostics | AnalyticsDiagnostics {
    if (!this.effective(category)) {
      return noopOf(category);
    }
    if (this.reporters[category] == null) {
      this.reporters[category] = this.buildReporter(category);
    }
    return this.reporters[category]!;
  }

  private buildReporter(category: DiagnosticsCategory): AnyReporter {
    const endpoint = categoryOptionsOf(this.options, category).endpoint as string;
    switch (category) {
      case "crash": return new CrashReporter(this.transport as Transport, this.resource, endpoint);
      case "trace": return new TraceReporter(this.transport as Transport, this.resource, endpoint);
      case "performance": return new PerformanceReporter(this.transport as Transport, this.resource, endpoint);
      default: return new AnalyticsReporter(this.transport as Transport, this.resource, endpoint);
    }
  }

  // 有效开启 = 开关开 + 端点在(Disabled 枢纽全 false)。
  private effective(category: DiagnosticsCategory): boolean {
    const o = categoryOptionsOf(this.options, category);
    return this.transport != null && o.enabled && o.endpoint != null && o.endpoint !== "";
  }
}
