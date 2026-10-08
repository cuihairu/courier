// L3 Adapter:UnityWebRequest 实现 ITransport(layers.md L3「网络」)。
// scope 头 / Bearer / 信封解析都归 L2;此处只管发送与超时。
// 4xx/5xx 有响应,原样返回交给 L2 解信封;仅网络层失败(无响应)抛 TransportException。
using System;
using System.Collections.Generic;
using System.Threading;
using System.Threading.Tasks;
using Courier.Core;
using UnityEngine.Networking;

namespace Courier.Adapter
{
    public sealed class UnityWebRequestTransport : ITransport
    {
        public async Task<TransportResponse> SendAsync(TransportRequest request,
            CancellationToken cancellationToken)
        {
            var webRequest = new UnityWebRequest(request.Url, request.Method);
            if (!string.IsNullOrEmpty(request.JsonBody))
            {
                webRequest.uploadHandler = new UploadHandlerRaw(
                    System.Text.Encoding.UTF8.GetBytes(request.JsonBody));
                webRequest.uploadHandler.contentType = "application/json";
            }
            webRequest.downloadHandler = new DownloadHandlerBuffer();
            if (request.Headers != null)
            {
                foreach (var kv in request.Headers)
                {
                    webRequest.SetRequestHeader(kv.Key, kv.Value);
                }
            }
            // UnityWebRequest 超时单位为秒;不足 1s 按 1s(契约 timeoutMs 为毫秒)。
            webRequest.timeout = Math.Max(1, request.TimeoutMs / 1000);

            // 取消:Abort 后 SendWebRequest 以 ConnectionError 收场,走 TransportException 归一。
            using (cancellationToken.Register(() => webRequest.Abort()))
            {
                var operation = webRequest.SendWebRequest();
                while (!operation.isDone)
                {
                    await Task.Yield().ConfigureAwait(false);
                }
            }

            try
            {
                if (webRequest.result == UnityWebRequest.Result.ConnectionError ||
                    webRequest.result == UnityWebRequest.Result.DataProcessingError)
                {
                    throw new TransportException(webRequest.error ?? "request failed", null);
                }

                var headers = new Dictionary<string, string>();
                var responseHeaders = webRequest.GetResponseHeaders();
                if (responseHeaders != null)
                {
                    foreach (var kv in responseHeaders)
                    {
                        headers[kv.Key] = kv.Value;
                    }
                }
                return new TransportResponse
                {
                    StatusCode = (int)webRequest.responseCode,
                    Headers = headers,
                    Body = webRequest.downloadHandler.text
                };
            }
            finally
            {
                webRequest.Dispose();
            }
        }
    }
}
