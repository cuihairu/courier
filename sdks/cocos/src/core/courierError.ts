// 契约错误(错误体 code/message/retryable + traceId,errors.md Frozen v1)。
// 分支判断只认 wire code(errors.md:message 人读可变,不得用于分支);
// 未登记的 code 容忍落兜底(契约 versioning.md:未知容忍)。
import { errorCodeOf, type ErrorCode } from "../contract/errors.ts";

export class CourierApiError extends Error {
  /** wire code 原文(未登记 code 原样保留——容忍未知由调用方兜底)。 */
  readonly wire: string;
  /** 登记过的码映射为枚举;未登记 = undefined。 */
  readonly code: ErrorCode | undefined;
  readonly http: number;
  readonly retryable: boolean;
  readonly traceId: string | undefined;
  /** Retry-After header(毫秒;契约 errors.md:配合退避重试)。 */
  readonly retryAfterMs: number | undefined;

  constructor(wire: string, message: string, http: number, retryable: boolean,
    traceId?: string, retryAfterMs?: number) {
    super(message);
    this.name = "CourierApiError";
    this.wire = wire;
    this.code = errorCodeOf(wire);
    this.http = http;
    this.retryable = retryable;
    this.traceId = traceId;
    this.retryAfterMs = retryAfterMs;
  }
}

/** 降级信号判断:501 COMMON_CAPABILITY_DISABLED(能力未接,调用方隐藏入口)。 */
export function isCapabilityDisabled(e: unknown): boolean {
  return e instanceof CourierApiError && e.wire === "COMMON_CAPABILITY_DISABLED";
}
