// 诊断可选包配置(契约 diagnostics.md「配置形状」;unity DiagnosticsOptions 同构)。
// 四类同构:enabled + endpoint;默认全关(零开销哲学:未配置即 no-op)。
// 资源上下文由宿主注入,不采集则缺省(null/undefined 不下发)。

/** 四类诊断能力(契约「四类能力与 Provider 生态」)。 */
export type DiagnosticsCategory = "crash" | "trace" | "performance" | "analytics";

/** 全类清单(遍历用)。 */
export const DiagnosticsCategories: readonly DiagnosticsCategory[] = [
  "crash", "trace", "performance", "analytics",
];

/** 单类开关:endpoint 为空时按未配置处理(契约:开启必须同时配置端点)。 */
export interface DiagnosticsCategoryOptions {
  enabled: boolean;
  endpoint: string | null;
}

/** 四类独立开关,缺省全关。 */
export interface DiagnosticsOptions {
  crash: DiagnosticsCategoryOptions;
  trace: DiagnosticsCategoryOptions;
  performance: DiagnosticsCategoryOptions;
  analytics: DiagnosticsCategoryOptions;
}

/** 全关缺省配置(显式 new,字段同构)。 */
export function newDiagnosticsOptions(): DiagnosticsOptions {
  const off = (): DiagnosticsCategoryOptions => ({ enabled: false, endpoint: null });
  return { crash: off(), trace: off(), performance: off(), analytics: off() };
}

export function categoryOptionsOf(options: DiagnosticsOptions, category: DiagnosticsCategory): DiagnosticsCategoryOptions {
  switch (category) {
    case "crash": return options.crash;
    case "trace": return options.trace;
    case "performance": return options.performance;
    default: return options.analytics;
  }
}

/** 任一类有效开启(开启且配了端点)= 需要建实例;否则全 no-op。 */
export function anyEnabled(options: DiagnosticsOptions): boolean {
  for (const category of DiagnosticsCategories) {
    const o = categoryOptionsOf(options, category);
    if (o.enabled && o.endpoint != null && o.endpoint !== "") {
      return true;
    }
  }
  return false;
}

/** 资源上下文(宿主注入;不采集的字段留 null,序列化时不下发)。
 *  敏感接口报文永不进入诊断采集(契约红线 5,本包无此数据路径)。 */
export interface DiagnosticsResource {
  gameId?: string | null;
  env?: string | null;
  platform?: string | null;
  appVersion?: string | null;
  engineVersion?: string | null;
  sessionId?: string | null;
  osVersion?: string | null;
  deviceModel?: string | null;
}
