// L3 平台适配(Layabox):Transport 的 XMLHttpRequest 实现。
// 请求头透传/响应头小写归一/超时经 xhr.timeout/网络失败归一 TransportError
// 走 core 既有重试口径——重连策略归 core。
import type { Transport, TransportRequest, TransportResponse } from "../../core/transport.ts";
import { TransportError } from "../../core/transport.ts";
import type { XhrFactory } from "./layaApi.ts";

export function createLayaboxTransport(factory: XhrFactory): Transport {
  return {
    send(request: TransportRequest): Promise<TransportResponse> {
      const xhr = factory();
      return new Promise<TransportResponse>((resolve, reject) => {
        xhr.open(request.method, request.url);
        for (const name of Object.keys(request.headers)) {
          xhr.setRequestHeader(name, request.headers[name]);
        }
        xhr.timeout = request.timeoutMs > 0 ? request.timeoutMs : 0; // 0 = 不设超时
        xhr.onload = () => {
          resolve({
            statusCode: xhr.status,
            headers: parseHeaders(xhr.getAllResponseHeaders()),
            body: xhr.responseText ?? "",
          });
        };
        xhr.onerror = () => reject(new TransportError("xhr network error"));
        xhr.ontimeout = () => reject(new TransportError("xhr timeout"));
        xhr.send(request.jsonBody ?? null);
      });
    },
  };
}

/** 原始响应头("\r\n" 分隔的 "name: value")→ 小写键映射。 */
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
