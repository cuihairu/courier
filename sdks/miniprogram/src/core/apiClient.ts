// L2 Core:API 客户端(unity ApiClient 同构)——拼 scope 头、发请求、解析信封、
// 按 retryable 退避重试、AUTH_TOKEN_EXPIRED 时经 refreshHook 换发后重放一次
// (自动 refresh,契约 auth.md「access 过期」)。
import { ErrorSpecs } from "../contract/errors.ts";
import type { CourierConfig } from "./types.ts";
import { CourierApiError } from "./courierError.ts";
import { TransportError, type Transport, type TransportRequest } from "./transport.ts";

/** scope 头(契约 scope.md:每次请求必带)。 */
export const ScopeHeaders = {
  GameId: "X-Courier-Game-Id",
  Env: "X-Courier-Env",
} as const;

/** 退避策略(retryable 才重试;Retry-After 秒优先,否则固定间隔)。 */
export interface RetryPolicy {
  readonly maxAttempts: number;
  readonly baseDelayMs: number;
}

export const defaultRetryPolicy: RetryPolicy = { maxAttempts: 3, baseDelayMs: 300 };

export interface ApiClientOptions {
  readonly config: CourierConfig;
  readonly transport: Transport;
  readonly retry?: RetryPolicy;
  /** 每次请求现取 access token(轮换后自动为新值);null = 匿名请求。 */
  readonly accessTokenProvider?: () => string | null;
  /** access 过期(AUTH_TOKEN_EXPIRED)时尝试 refresh;返回 true 则重放一次。 */
  readonly refreshHook?: () => Promise<boolean>;
  /** 退避睡眠(测试注入;默认真实 setTimeout)。 */
  readonly sleep?: (ms: number) => Promise<void>;
}

interface Envelope {
  data?: unknown;
  error?: { code?: string; message?: string; retryable?: boolean };
  traceId?: string;
}

const defaultSleep = (ms: number) => new Promise<void>((r) => setTimeout(r, ms));

export class ApiClient {
  private readonly config: CourierConfig;
  private readonly transport: Transport;
  private readonly retry: RetryPolicy;
  private readonly accessTokenProvider?: () => string | null;
  private readonly refreshHook?: () => Promise<boolean>;
  private readonly sleep: (ms: number) => Promise<void>;

  constructor(options: ApiClientOptions) {
    this.config = options.config;
    this.transport = options.transport;
    this.retry = options.retry ?? defaultRetryPolicy;
    this.accessTokenProvider = options.accessTokenProvider;
    this.refreshHook = options.refreshHook;
    this.sleep = options.sleep ?? defaultSleep;
  }

  /** 发送请求并解信封,返回 data;失败抛 CourierApiError(分支只认 e.code)。 */
  async request<T>(method: string, path: string, body?: unknown, withAuth = true): Promise<T> {
    const headers: Record<string, string> = {
      [ScopeHeaders.GameId]: this.config.gameId,
      [ScopeHeaders.Env]: this.config.env,
    };
    let token = withAuth && this.accessTokenProvider ? this.accessTokenProvider() : null;
    if (withAuth && !token) {
      // 本地预检:未认证不发包(契约 COMMON_UNAUTHENTICATED)。
      throw new CourierApiError("COMMON_UNAUTHENTICATED", "not authenticated", 401, false);
    }
    if (token) {
      headers["Authorization"] = "Bearer " + token;
    }
    if (body !== undefined) {
      headers["Content-Type"] = "application/json";
    }

    const request: TransportRequest = {
      method,
      url: this.config.endpoint.replace(/\/+$/, "") + path,
      headers,
      jsonBody: body === undefined ? undefined : JSON.stringify(body),
      timeoutMs: 10000,
    };

    let refreshed = false;
    for (let attempt = 1; ; attempt++) {
      let error: CourierApiError;
      try {
        const response = await this.transport.send(request);
        const result = parseEnvelope(response);
        if (result.error === undefined) {
          return result.data as T;
        }
        error = result.error;
      } catch (e) {
        if (e instanceof CourierApiError) {
          error = e;
        } else {
          // 网络失败/超时:依赖不可用(可重试)。
          error = new CourierApiError("COMMON_UNAVAILABLE",
            "transport failed: " + (e instanceof Error ? e.message : String(e)), 503, true);
        }
      }

      // access 过期:refresh 一次后重放(不计入退避重试次数)。
      if (error.wire === "AUTH_TOKEN_EXPIRED" && this.refreshHook && !refreshed) {
        refreshed = true;
        if (await this.refreshHook()) {
          token = this.accessTokenProvider ? this.accessTokenProvider() : null;
          if (token) {
            headers["Authorization"] = "Bearer " + token;
          }
          attempt--;
          continue;
        }
        throw error;
      }

      if (error.retryable && attempt < this.retry.maxAttempts) {
        await this.sleep(retryDelayMs(error, this.retry));
        continue;
      }
      throw error;
    }
  }
}

/** 按 HTTP 状态挑兜底 wire code(网络/网关异常无 JSON body 时的保守映射)。 */
function fallbackWire(status: number): { wire: string; retryable: boolean } {
  if (status === 401) return { wire: "COMMON_UNAUTHENTICATED", retryable: false };
  if (status === 403) return { wire: "COMMON_PERMISSION_DENIED", retryable: false };
  if (status === 404) return { wire: "COMMON_NOT_FOUND", retryable: false };
  if (status === 429) return { wire: "RATE_LIMITED", retryable: true };
  if (status === 501) return { wire: "COMMON_CAPABILITY_DISABLED", retryable: false };
  if (status >= 500) return { wire: "COMMON_UNAVAILABLE", retryable: true };
  return { wire: "COMMON_INTERNAL", retryable: status >= 500 || status === 429 };
}

function parseEnvelope(response: { statusCode: number; headers: Readonly<Record<string, string>>; body: string }):
  { data?: unknown; error?: CourierApiError } {
  // 网络/网关异常可能没有 JSON body(5xx 网关直返、连接中断):按状态兜底。
  if (!response.body.trim()) {
    const fb = fallbackWire(response.statusCode);
    return {
      error: new CourierApiError(fb.wire,
        "empty response body (HTTP " + response.statusCode + ")",
        response.statusCode, fb.retryable || response.statusCode >= 500),
    };
  }
  let envelope: Envelope;
  try {
    envelope = JSON.parse(response.body) as Envelope;
  } catch {
    const fb = fallbackWire(response.statusCode);
    return {
      error: new CourierApiError(fb.wire, "non-JSON response body (HTTP " + response.statusCode + ")",
        response.statusCode, fb.retryable || response.statusCode >= 500),
    };
  }
  if (envelope.error) {
    const wire = envelope.error.code ?? "";
    const spec = ErrorSpecs[wire as keyof typeof ErrorSpecs];
    // 信封 retryable 为准;登记码的冻结映射兜底缺省字段。
    return {
      error: new CourierApiError(wire, envelope.error.message ?? "",
        response.statusCode, envelope.error.retryable ?? spec?.retryable ?? false,
        envelope.traceId, retryAfterMs(response.headers)),
    };
  }
  return { data: envelope.data };
}

/** Retry-After header(契约单位:秒)→ 毫秒;缺省/非法 = undefined。 */
function retryAfterMs(headers: Readonly<Record<string, string>>): number | undefined {
  const raw = headers["retry-after"];
  if (!raw) return undefined;
  const seconds = Number(raw);
  return Number.isFinite(seconds) && seconds >= 0 ? seconds * 1000 : undefined;
}

/** 重试间隔:Retry-After(秒)优先,否则 baseDelayMs。 */
function retryDelayMs(error: CourierApiError, policy: RetryPolicy): number {
  return error.retryAfterMs ?? policy.baseDelayMs;
}

export { parseEnvelope };
