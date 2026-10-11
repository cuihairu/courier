// L3 平台适配(Layabox):SseSource 的 XMLHttpRequest 流式实现(LayaAir Web/小游戏壳)。
// 契约 messages.md:GET /v1/messages/stream,Bearer 必选(头由编排层每连现取拼入)。
// 语义(与微信 enableChunked 相对):
//   · XHR LOADING 态体增量经 onprogress 可见——responseText 已含全部已收字节
//     (浏览器内建流式解码,跨块多字节不撕裂),源按消费偏移切增量;
//   · **有净关流回调**:onload = 服务端关流(DONE),归一为流尾(readLine → null)
//     走编排层重连;onerror/ontimeout 归一断流;onabort 归一取消路径;
//   · 头与状态码在 onprogress/onload 首拍回填连接(XHR 无"头到"独立回调,
//     慢体服务端会延后建连解析——SSE 服务端心跳即首发,实际不构成等待);
//   · timeout 置 0(长连不禁超时;超时归 ontimeout = 断流)。
// 取消:AbortSignal → xhr.abort() → onabort,编排层以 signal.aborted 分流。
import { TransportError } from "../../core/transport.ts";
import type { SseStreamConnection, SseStreamRequest, SseSource } from "../../core/sseStream.ts";
import type { XhrFactory } from "./layaApi.ts";

export function createLayaSseSource(factory: XhrFactory): SseSource {
  return {
    connect(request: SseStreamRequest, signal?: AbortSignal): Promise<SseStreamConnection> {
      return new Promise<SseStreamConnection>((resolve, reject) => {
        if (signal?.aborted) {
          reject(new TransportError("aborted"));
          return;
        }
        const conn = new LayaXhrConnection();
        let xhr: XhrStreamLike;
        try {
          xhr = factory() as XhrStreamLike;
        } catch (e) {
          reject(new TransportError(errorMessage(e)));
          return;
        }
        const onAbort = () => {
          try {
            xhr.abort();
          } catch {
            // abort 归一:xhr 会随后触发 onabort,无需额外处理
          }
        };
        const cleanupAbort = () => {
          signal?.removeEventListener("abort", onAbort);
        };
        signal?.addEventListener("abort", onAbort, { once: true });

        const settleFail = (message: string) => {
          if (!conn.settled) {
            cleanupAbort();
            reject(new TransportError(message)); // 建连失败:走重连
          } else {
            conn.breakStream(message); // 流中断:归一 TransportError 走重连
          }
        };
        xhr.onprogress = () => {
          if (conn.settleIfNeed(xhr)) {
            resolve(conn); // 首拍:建连完成(状态码/头已回填)
          }
          conn.absorb(xhr.responseText);
          if (xhr.readyState === 4) {
            conn.endStream(); // progress 携 DONE:体已尽
          }
        };
        xhr.onload = () => {
          if (conn.settleIfNeed(xhr)) {
            resolve(conn); // 无体直收尾:同样先建连再交流尾
          }
          conn.absorb(xhr.responseText);
          conn.endStream(); // 净关流:流尾 → 重连
        };
        xhr.onerror = () => settleFail("xhr network error");
        xhr.ontimeout = () => settleFail("xhr timeout");
        xhr.onabort = () => settleFail("aborted");

        try {
          conn.bind(xhr);
          xhr.open("GET", request.url);
          xhr.setRequestHeader("Accept", "text/event-stream");
          for (const [key, value] of Object.entries(request.headers ?? {})) {
            xhr.setRequestHeader(key, value);
          }
          xhr.timeout = 0; // 长连不设超时
          xhr.send(null);
        } catch (e) {
          cleanupAbort();
          reject(new TransportError(errorMessage(e)));
          return;
        }
      });
    },
  };
}

/** 流式源用到的 XHR 面(XhrLike + abort;真实 XHR 天然满足)。 */
interface XhrStreamLike {
  open(method: string, url: string): void;
  setRequestHeader(name: string, value: string): void;
  send(body?: string | null): void;
  abort(): void;
  timeout: number;
  status: number;
  readyState: number;
  responseText: string;
  onprogress: (() => void) | null;
  onload: (() => void) | null;
  onerror: (() => void) | null;
  ontimeout: (() => void) | null;
  onabort: (() => void) | null;
}

/** 一次 XHR 流连接:状态码/头延迟回填(settleIfNeed)+ responseText 增量行缓冲。
 *  settled 公开:建连回调在工厂闭包按它分流(已建连 → 断流,未建连 → 建连失败)。 */
class LayaXhrConnection implements SseStreamConnection {
  statusCode = 0;
  headers: Readonly<Record<string, string>> = {};
  settled = false;

  private ended = false;
  private failure: TransportError | null = null;
  private consumed = 0;
  private readonly lines: string[] = [];
  private tail = "";
  private waiter: (() => void) | null = null;
  private xhr: XhrStreamLike | null = null;

  /** 头/状态码首拍回填(onprogress/onload 触发;键小写归一)。首拍返回 true。 */
  settleIfNeed(xhr: XhrStreamLike): boolean {
    if (this.settled) {
      return false;
    }
    this.settled = true;
    this.statusCode = xhr.status;
    this.headers = parseHeaders(xhr.getAllResponseHeaders());
    return true;
  }

  /** 吸收 responseText 新增段:切行入队,唤醒等待者。 */
  absorb(responseText: string): void {
    if (this.ended || responseText.length <= this.consumed) {
      return;
    }
    this.tail += responseText.slice(this.consumed);
    this.consumed = responseText.length;
    let nl = this.tail.indexOf("\n");
    while (nl >= 0) {
      let line = this.tail.slice(0, nl);
      if (line.endsWith("\r")) {
        line = line.slice(0, -1); // CRLF 容忍(SSE 规范行终止含 \r\n)
      }
      this.lines.push(line);
      this.tail = this.tail.slice(nl + 1);
      nl = this.tail.indexOf("\n");
    }
    this.wake();
  }

  /** 流尾(onload/DONE):置净尽,唤醒等待者(readLine → null → 编排层重连)。 */
  endStream(): void {
    if (this.ended) {
      return;
    }
    this.ended = true;
    this.wake();
  }

  /** 断流(onerror/ontimeout/onabort):记为失败,唤醒等待者。 */
  breakStream(message: string): void {
    if (this.ended) {
      return;
    }
    this.ended = true;
    this.failure = new TransportError(message);
    this.wake();
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
      if (this.ended) {
        return null; // 净关流
      }
      await this.wait();
    }
  }

  close(): void {
    this.endStream();
    try {
      this.xhr?.abort(); // 净停:随后 onabort 落 breakStream,已 ended 幂等
    } catch {
      // abort 归一
    }
  }

  bind(xhr: XhrStreamLike): void {
    this.xhr = xhr;
  }

  private wait(): Promise<void> {
    return new Promise<void>((resolve) => {
      this.waiter = resolve;
    });
  }

  private wake(): void {
    const waiter = this.waiter;
    this.waiter = null;
    waiter?.();
  }
}

/** 原始响应头("\r\n" 分隔的 "name: value")→ 小写键映射(与 transport 同口径)。 */
function parseHeaders(raw: string): Record<string, string> {
  const headers: Record<string, string> = {};
  for (const line of raw.split("\r\n")) {
    const idx = line.indexOf(":");
    if (idx > 0) {
      headers[line.slice(0, idx).trim().toLowerCase()] = line.slice(idx + 1).trim();
    }
  }
  return headers;
}

function errorMessage(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}
