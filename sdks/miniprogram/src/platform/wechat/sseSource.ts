// L3 平台适配(微信小程序):SseSource 的 wx.request enableChunked 实现。
// 契约 messages.md:GET /v1/messages/stream,Bearer 必选(头由编排层每连现取拼入)。
// 平台语义(结构类型真机待验,随部署面):
//   · success 在响应头到达时触发(statusCode/header 可取),体按块经 onChunkReceived 交付;
//   · **无"服务端净关流"回调**——wx 对 chunked 流只以 fail(或静默)呈现断流,真机表现
//     待验;SDK 侧已按 fail 归一断流重连,静默场景依赖心跳注释与生命周期断流兜底;
//   · 分块 ArrayBuffer 无 TextDecoder 环境安全(小程序基库不全有),UTF-8 解码走
//     core/utf8.ts 手写增量解码器(跨块多字节不撕裂)。
// 取消:AbortSignal → task.abort() → wx 以 fail 收场,编排层以 signal.aborted 分流。
import { TransportError } from "../../core/transport.ts";
import { IncrementalUtf8Decoder } from "../../core/utf8.ts";
import type { SseStreamConnection, SseStreamRequest, SseSource } from "../../core/sseStream.ts";
import type { WxChunkedRequestTask, WxChunkedRequester } from "./wxApi.ts";

export function createWechatSseSource(wx: WxChunkedRequester): SseSource {
  return {
    connect(request: SseStreamRequest, signal?: AbortSignal): Promise<SseStreamConnection> {
      return new Promise<SseStreamConnection>((resolve, reject) => {
        if (signal?.aborted) {
          reject(new TransportError("aborted"));
          return;
        }
        const conn = new WxChunkedConnection();
        let task: WxChunkedRequestTask | undefined;
        const onAbort = () => {
          try {
            task?.abort();
          } catch {
            // abort 归一:wx 会随后触发 fail,无需额外处理
          }
        };
        const cleanupAbort = () => {
          signal?.removeEventListener("abort", onAbort);
        };
        signal?.addEventListener("abort", onAbort, { once: true });
        try {
          task = wx.request({
            url: request.url,
            method: "GET",
            header: { Accept: "text/event-stream", ...request.headers },
            enableChunked: true,
            success(res) {
              if (conn.settled) {
                return;
              }
              conn.setHead(res.statusCode, lowercased(res.header));
              resolve(conn);
            },
            fail(err) {
              if (conn.settled) {
                conn.breakStream(err.errMsg); // 流中断:归一 TransportError 走重连
              } else {
                cleanupAbort();
                reject(new TransportError(err.errMsg)); // 建连失败:走重连
              }
            },
          });
        } catch (e) {
          cleanupAbort();
          reject(new TransportError(errorMessage(e)));
          return;
        }
        task.onChunkReceived((res) => conn.push(res.data));
      });
    },
  };
}

/** 一次 chunked 流连接:行缓冲(解码→切行)+ 阻塞式读(块回调唤醒)。
 *  settled 公开:建连回调在工厂闭包按它分流(已建连 → 断流,未建连 → 建连失败)。 */
class WxChunkedConnection implements SseStreamConnection {
  statusCode = 0;
  headers: Readonly<Record<string, string>> = {};
  settled = false;
  private closed = false;
  private failure: TransportError | null = null;
  private readonly decoder = new IncrementalUtf8Decoder();
  private readonly lines: string[] = [];
  private tail = "";
  private waiter: (() => void) | null = null;

  /** 头到(建连 success):回填状态码与头(键小写归一),唤醒等待中的读。 */
  setHead(statusCode: number, headers: Readonly<Record<string, string>>): void {
    this.statusCode = statusCode;
    this.headers = headers;
    this.settled = true;
    this.wake();
  }

  /** 收一块(ArrayBuffer):增量解码后按行入队,唤醒等待者。 */
  push(data: ArrayBuffer): void {
    if (this.closed || data.byteLength === 0) {
      return;
    }
    this.tail += this.decoder.decode(new Uint8Array(data));
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

  /** 服务端关流(wx fail):记为断流,唤醒等待者。 */
  breakStream(message: string): void {
    if (this.closed) {
      return;
    }
    this.closed = true;
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
      if (this.closed) {
        return null; // 净关流
      }
      await this.wait();
    }
  }

  close(): void {
    if (this.closed) {
      return;
    }
    this.closed = true;
    this.wake();
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

function lowercased(headers: Record<string, string> | undefined): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [key, value] of Object.entries(headers ?? {})) {
    out[key.toLowerCase()] = value;
  }
  return out;
}

function errorMessage(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}
