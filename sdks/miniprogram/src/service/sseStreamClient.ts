// L2 messages 流式传输编排(纯逻辑,无平台引用):契约 messages.md 的
// GET /v1/messages/stream 长连客户端。与 unity SseStreamClient / godot
// CourierSseStream 同构。平台网络细节在 SseSource 实现(默认 FetchSseSource);
// 本件管重连编排:501 → capability_disabled(降级转纯拉取,不重试)、
// 401 → unauthenticated,两者终态经 onClosed 收尾;断线指数退避重连
// (1s 起、上限 60s,Retry-After 秒头优先);收过事件的连接断开即重置退避
// (健康连≠失败连);净停经 AbortSignal;Authorization 每次尝试按 tokenProvider
// 现取(契约:新连接按当前 token 校验);逐行喂 SseParser,未知 event 容忍。
import { SseCloseReasons, sseBackoffDelayMs,
  type SseStreamConnection, type SseStreamRequest, type SseSource } from "../core/sseStream.ts";
import { TransportError } from "../core/transport.ts";
import { SseParser, type SseEvent } from "./sseParser.ts";

export interface SseStreamClientOptions {
  readonly source: SseSource;
  readonly backoffBaseMs?: number;
  readonly backoffMaxMs?: number;
  /** 退避睡眠(测试注入;默认真实 setTimeout,abort 即拒绝)。 */
  readonly sleep?: (ms: number, signal?: AbortSignal) => Promise<void>;
}

/** 一次连接尝试的结局:reason 收尾原因 / healthy 收过事件(重置退避)/ retryAfterMs。 */
interface SseAttemptOutcome {
  reason: string;
  healthy: boolean;
  retryAfterMs: number;
}

export class SseStreamClient {
  private readonly source: SseSource;
  private readonly backoffBaseMs: number;
  private readonly backoffMaxMs: number;
  private readonly sleep: (ms: number, signal?: AbortSignal) => Promise<void>;

  /** 上一次重连前算得的退避毫秒(诊断/测试观察面;含 Retry-After 优先值)。 */
  lastDelayMs = 0;

  constructor(options: SseStreamClientOptions) {
    if (!options.source) {
      throw new TypeError("SseStreamClient: source required");
    }
    this.source = options.source;
    this.backoffBaseMs = options.backoffBaseMs ?? 1000;
    this.backoffMaxMs = options.backoffMaxMs ?? 60000;
    this.sleep = options.sleep ?? defaultSleep;
  }

  /** 长连循环:直到 signal 取消或终态(501/401)。onEvent 收解析帧,onClosed 收收尾原因。 */
  async run(request: SseStreamRequest, onEvent: (evt: SseEvent) => void,
    onClosed: (reason: string) => void, signal?: AbortSignal): Promise<void> {
    if (!request) {
      throw new TypeError("SseStreamClient: request required");
    }
    let attempt = 0;
    while (!signal?.aborted) {
      const outcome = await this.attemptOnce(request, onEvent, signal);
      if (outcome.reason === SseCloseReasons.Stopped) {
        return;
      }
      if (outcome.reason === SseCloseReasons.CapabilityDisabled ||
        outcome.reason === SseCloseReasons.Unauthenticated) {
        onClosed?.(outcome.reason);
        return;
      }
      attempt = outcome.healthy ? 0 : attempt + 1;
      this.lastDelayMs = sseBackoffDelayMs(attempt, outcome.retryAfterMs, this.backoffBaseMs, this.backoffMaxMs);
      if (this.lastDelayMs > 0) {
        try {
          await this.sleep(this.lastDelayMs, signal);
        } catch {
          return; // 退避中被取消:净停
        }
      }
    }
  }

  private async attemptOnce(request: SseStreamRequest, onEvent: (evt: SseEvent) => void,
    signal?: AbortSignal): Promise<SseAttemptOutcome> {
    const conn = await this.connectSafe(request, signal);
    if (conn === null) {
      return signal?.aborted
        ? { reason: SseCloseReasons.Stopped, healthy: false, retryAfterMs: 0 }
        : { reason: SseCloseReasons.Disconnected, healthy: false, retryAfterMs: 0 };
    }
    const retryAfterMs = parseRetryAfterMs(lowerKeys(conn.headers));
    if (conn.statusCode === 501) {
      conn.close();
      return terminal(SseCloseReasons.CapabilityDisabled);
    }
    if (conn.statusCode === 401) {
      conn.close();
      return terminal(SseCloseReasons.Unauthenticated);
    }
    let healthy = false;
    if (conn.statusCode === 200) {
      const parser = new SseParser();
      try {
        for (;;) {
          if (signal?.aborted) {
            conn.close();
            return { reason: SseCloseReasons.Stopped, healthy: false, retryAfterMs: 0 };
          }
          const line = await conn.readLine();
          if (line === null) {
            break; // 流尾 = 断流,走重连
          }
          const evt = parser.feedLine(line);
          if (evt !== null) {
            healthy = true;
            onEvent?.(evt);
          }
        }
      } catch (e) {
        conn.close();
        if (signal?.aborted) {
          return { reason: SseCloseReasons.Stopped, healthy: false, retryAfterMs: 0 };
        }
        if (e instanceof TransportError) {
          // 中途断流:走重连
        } else {
          throw e;
        }
      }
    }
    conn.close();
    return { reason: SseCloseReasons.Disconnected, healthy, retryAfterMs };
  }

  private async connectSafe(request: SseStreamRequest,
    signal?: AbortSignal): Promise<SseStreamConnection | null> {
    const headers: Record<string, string> = {};
    for (const [key, value] of Object.entries(request.headers ?? {})) {
      headers[key] = value;
    }
    const token = request.tokenProvider?.() ?? null;
    if (token) {
      headers["Authorization"] = "Bearer " + token; // 每连现取(契约:新连接按当前 token 校验)
    }
    const attemptRequest: SseStreamRequest = {
      url: request.url,
      headers,
      tokenProvider: request.tokenProvider,
    };
    try {
      return await this.source.connect(attemptRequest, signal);
    } catch (e) {
      if (signal?.aborted) {
        return null; // 取消:净停
      }
      if (e instanceof TransportError) {
        return null; // 网络失败 = 断流语义,走退避重连
      }
      throw e;
    }
  }
}

function terminal(reason: string): SseAttemptOutcome {
  return { reason, healthy: false, retryAfterMs: 0 };
}

function parseRetryAfterMs(headers: Readonly<Record<string, string>>): number {
  const raw = headers["retry-after"];
  if (!raw) {
    return 0;
  }
  const seconds = Number(raw);
  return Number.isFinite(seconds) && seconds > 0 ? seconds * 1000 : 0;
}

/** 头键小写归一(契约 transport 键规范;与 unity SseStreamClient.Lowercased 同构)。 */
function lowerKeys(headers: Readonly<Record<string, string>>): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [key, value] of Object.entries(headers ?? {})) {
    out[key.toLowerCase()] = value;
  }
  return out;
}

function defaultSleep(ms: number, signal?: AbortSignal): Promise<void> {
  return new Promise<void>((resolve, reject) => {
    if (signal?.aborted) {
      reject(new TransportError("aborted"));
      return;
    }
    const timer = setTimeout(resolve, ms);
    signal?.addEventListener("abort", () => {
      clearTimeout(timer);
      reject(new TransportError("aborted"));
    }, { once: true });
  });
}
