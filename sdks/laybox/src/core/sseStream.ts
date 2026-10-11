// L2 Core:messages SSE 流式传输接缝(契约 messages.md)+ fetch 默认实现。
// 与 unity Core/SseStream.cs 同构:接缝定在 Core(平台接线实现不引 Service),
// 重连编排(依赖 SseParser)在 Service 的 SseStreamClient——core 不反向依赖。
// 取消经 AbortSignal(与 FetchTransport 同约);网络失败统一抛 TransportError;
// fetch 流读要求服务端 chunked/流式分框(自建网关 Go net/http 在 Content-Length
// 缺省时自动补 chunked,天然满足);行切分按 \n 并剥尾 \r,多字节经 TextDecoder
// (stream:true)跨 chunk 安全。

import { TransportError } from "./transport.ts";

/** 一次流连接尝试的请求。client 已按每次尝试现取 token 拼入 headers 的
 *  `Authorization`(契约:新连接按当前 token 校验);source 只透传 headers,
 *  不重复调用 tokenProvider——否则首连就取走第二个 token(与 unity 同构约定)。 */
export interface SseStreamRequest {
  readonly url: string;
  readonly headers?: Readonly<Record<string, string>>;
  /** 每次连接现取 token;编排层(SseStreamClient)负责调用并拼头。 */
  readonly tokenProvider?: () => string | null;
}

/** 一次已建立的流连接:状态码 + 头(键小写归一)+ 逐行读(流尾 resolve null)。 */
export interface SseStreamConnection {
  readonly statusCode: number;
  readonly headers: Readonly<Record<string, string>>;
  /** 读一行(不含换行符);流尾(服务端关流)resolve null;网络中断抛 TransportError。 */
  readLine(): Promise<string | null>;
  close(): void;
}

/** 平台接线面:单次连接尝试(网络失败抛 TransportError;建连后按响应码分流)。 */
export interface SseSource {
  connect(request: SseStreamRequest, signal?: AbortSignal): Promise<SseStreamConnection>;
}

/** 通道收尾原因(onClosed 收)。 */
export const SseCloseReasons = {
  CapabilityDisabled: "capability_disabled",
  Unauthenticated: "unauthenticated",
  Stopped: "stopped",
  Disconnected: "disconnected",
} as const;

/** 重连退避(纯函数):base·2^(attempt-1) 封顶 max;Retry-After(毫秒)优先。 */
export function sseBackoffDelayMs(attempt: number, retryAfterMs: number, baseMs: number, maxMs: number): number {
  if (retryAfterMs > 0) {
    return retryAfterMs; // 契约语义规则 3:Retry-After 头优先
  }
  if (attempt < 1) attempt = 1;
  const cap = Math.max(maxMs, 1);
  let delay = Math.min(Math.max(baseMs, 1), cap);
  for (let i = 0; i < attempt - 1 && delay < cap; i++) {
    delay = Math.min(delay * 2, cap);
  }
  return delay;
}

/** fetch 流式源(默认实现):runtime 须有 WHATWG fetch + ReadableStream 流读
 *  (Cocos 3.x web/原生 JSB、微信基础库 2.10+、Node 18+ 均可);平台特化
 *  (wx.request enableChunked 等)另实现 SseSource,编排层不变。 */
export class FetchSseSource implements SseSource {
  private readonly fetchImpl: typeof fetch;

  constructor(fetchImpl?: typeof fetch) {
    this.fetchImpl = fetchImpl ?? globalThis.fetch.bind(globalThis);
  }

  async connect(request: SseStreamRequest, signal?: AbortSignal): Promise<SseStreamConnection> {
    const headers: Record<string, string> = { Accept: "text/event-stream" };
    for (const [key, value] of Object.entries(request.headers ?? {})) {
      headers[key] = value;
    }

    let response: Response;
    try {
      response = await this.fetchImpl(request.url, { method: "GET", headers, signal });
    } catch (e) {
      // 取消与网络失败都归一 TransportError;client 以 signal.aborted 分流净停/重连。
      throw new TransportError("sse connect failed: " + errorMessage(e));
    }
    if (!response.body) {
      throw new TransportError("sse stream requires a streamable body");
    }

    const responseHeaders: Record<string, string> = {};
    response.headers.forEach((value, key) => {
      responseHeaders[key.toLowerCase()] = value; // 键小写归一(契约 transport)
    });
    return new FetchSseConnection(response.status, responseHeaders, response.body.getReader(), signal);
  }
}

class FetchSseConnection implements SseStreamConnection {
  readonly statusCode: number;
  readonly headers: Readonly<Record<string, string>>;

  private readonly reader: ReadableStreamDefaultReader<Uint8Array>;
  private readonly signal?: AbortSignal;
  private readonly decoder = new TextDecoder("utf-8");
  private readonly lines: string[] = [];
  private buffer = "";
  private done = false;
  private failure: TransportError | null = null;

  constructor(statusCode: number, headers: Record<string, string>,
    reader: ReadableStreamDefaultReader<Uint8Array>, signal?: AbortSignal) {
    this.statusCode = statusCode;
    this.headers = headers;
    this.reader = reader;
    this.signal = signal;
  }

  async readLine(): Promise<string | null> {
    for (;;) {
      const line = this.lines.shift();
      if (line !== undefined) {
        return line;
      }
      if (this.failure) {
        throw this.failure;
      }
      if (this.done) {
        if (this.buffer.length > 0) {
          const rest = this.buffer; // 流尾残余半行按行吐出(SSE 规范容忍)
          this.buffer = "";
          return rest;
        }
        return null; // 流尾 = 断流
      }
      let chunk: ReadableStreamReadResult<Uint8Array>;
      try {
        chunk = await this.reader.read(); // abort 时以 AbortError 拒绝,下方归一
      } catch (e) {
        this.failure = new TransportError("sse stream broken: " + errorMessage(e));
        throw this.failure;
      }
      if (chunk.done) {
        this.done = true;
        continue;
      }
      this.buffer += this.decoder.decode(chunk.value, { stream: true });
      let nl = this.buffer.indexOf("\n");
      while (nl >= 0) {
        let line = this.buffer.slice(0, nl);
        if (line.endsWith("\r")) {
          line = line.slice(0, -1); // CRLF 容忍(SSE 规范行终止含 \r\n)
        }
        this.lines.push(line);
        this.buffer = this.buffer.slice(nl + 1);
        nl = this.buffer.indexOf("\n");
      }
    }
  }

  close(): void {
    if (this.done) {
      return;
    }
    this.done = true;
    this.reader.cancel().catch(() => {}); // 净停:未决 read 拒绝由调用方经 signal.aborted 分流
  }
}

function errorMessage(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}
