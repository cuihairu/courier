// L2 Core 传输抽象(unity ITransport 同构):平台各自实现,SDK 其余部分零平台引用。
// Cocos Creator / 小程序 / Node 各自换 Transport 实现;FetchTransport 为默认
// (运行时须有 WHATWG fetch:Cocos 3.x web/原生 JSB、微信基础库 2.10+ 均可)。

export interface TransportRequest {
  readonly method: string;
  readonly url: string;
  readonly headers: Readonly<Record<string, string>>;
  readonly jsonBody?: string;
  readonly timeoutMs: number;
}

export interface TransportResponse {
  readonly statusCode: number;
  readonly headers: Readonly<Record<string, string>>;
  readonly body: string;
}

export interface Transport {
  send(request: TransportRequest): Promise<TransportResponse>;
}

const defaultTimeoutMs = 10000;

/** fetch 适配(默认实现);超时经 AbortSignal,网络失败统一抛 TypeError 形态异常。 */
export class FetchTransport implements Transport {
  private readonly fetchImpl: typeof fetch;

  constructor(fetchImpl?: typeof fetch) {
    this.fetchImpl = fetchImpl ?? globalThis.fetch.bind(globalThis);
  }

  async send(request: TransportRequest): Promise<TransportResponse> {
    const timeoutMs = request.timeoutMs > 0 ? request.timeoutMs : defaultTimeoutMs;
    let resp: Response;
    try {
      resp = await this.fetchImpl(request.url, {
        method: request.method,
        headers: request.headers,
        body: request.jsonBody,
        signal: AbortSignal.timeout(timeoutMs),
      });
    } catch (e) {
      // 网络失败/超时归一为 TransportError(ApiClient 映射 COMMON_UNAVAILABLE 重试)。
      throw new TransportError(e instanceof Error ? e.message : String(e));
    }
    const headers: Record<string, string> = {};
    resp.headers.forEach((v, k) => {
      headers[k.toLowerCase()] = v;
    });
    return { statusCode: resp.status, headers, body: await resp.text() };
  }
}

export class TransportError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "TransportError";
  }
}
