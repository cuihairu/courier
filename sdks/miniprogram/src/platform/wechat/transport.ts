// L3 平台适配(微信小程序):Transport 的 wx.request 实现。
// 断线/超时归一为 TransportError(ApiClient 既有重试口径:映射 COMMON_UNAVAILABLE
// 退避重试,Retry-After 优先)——重连策略归 core,适配器只做平台翻译。
// wx 默认把 JSON 响应解析为对象:字符串原样,对象重新序列化(信封解析口径不变)。
import type { Transport, TransportRequest, TransportResponse } from "../../core/transport.ts";
import { TransportError } from "../../core/transport.ts";
import type { WxRequester } from "./wxApi.ts";

const defaultTimeoutMs = 10000;

export function createWechatTransport(wx: WxRequester): Transport {
  return {
    send(request: TransportRequest): Promise<TransportResponse> {
      const timeoutMs = request.timeoutMs > 0 ? request.timeoutMs : defaultTimeoutMs;
      return new Promise<TransportResponse>((resolve, reject) => {
        wx.request({
          url: request.url,
          method: request.method,
          header: { ...request.headers },
          data: request.jsonBody,
          timeout: timeoutMs,
          success(res) {
            const headers: Record<string, string> = {};
            for (const [k, v] of Object.entries(res.header ?? {})) {
              headers[k.toLowerCase()] = String(v);
            }
            const body = typeof res.data === "string"
              ? res.data
              : res.data === undefined || res.data === null ? "" : JSON.stringify(res.data);
            resolve({ statusCode: res.statusCode, headers, body });
          },
          fail(err) {
            // 网络失败/超时归一(ApiClient 映射 COMMON_UNAVAILABLE 重试)。
            reject(new TransportError(err.errMsg));
          },
        });
      });
    },
  };
}
