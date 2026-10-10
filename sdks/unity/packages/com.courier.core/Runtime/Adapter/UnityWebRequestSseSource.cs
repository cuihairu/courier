// L3 Adapter:UnityWebRequest 流式 SSE 接线(ISseSource 实现,messages 通道)。
// 编排(退避/重连/解析)全在纯逻辑件(Core/SseStream + Service/SseStreamClient,
// dotnet 单测覆盖);本件只做引擎连接与逐行读。
// 约束:SendWebRequest 须主线程调用(Unity 限制),RunAsync 由此从主线程起跑;
// 长连不设超时,净停靠 CancellationToken(Abort 归一为 TransportException);
// 服务端必须 chunked 分框(自建网关 Go net/http 在 Content-Length 缺省时自动补)。
using System;
using System.Collections.Generic;
using System.Text;
using System.Threading;
using System.Threading.Tasks;
using Courier.Core;
using UnityEngine.Networking;

namespace Courier.Adapter
{
    public sealed class UnityWebRequestSseSource : ISseSource
    {
        public async Task<SseStreamConnection> ConnectAsync(SseStreamRequest request,
            CancellationToken cancellationToken)
        {
            var webRequest = new UnityWebRequest(request.Url, UnityWebRequest.kHttpVerbGET);
            var handler = new SseBufferHandler();
            webRequest.downloadHandler = handler;
            if (request.Headers != null)
            {
                foreach (var kv in request.Headers)
                {
                    webRequest.SetRequestHeader(kv.Key, kv.Value);
                }
            }

            UnityWebRequestAsyncOperation operation;
            using (cancellationToken.Register(() => webRequest.Abort()))
            {
                operation = webRequest.SendWebRequest();
                // 等响应头(responseCode 置位)或失败;头到即回,体走增量读。
                while (webRequest.responseCode == 0 && !operation.isDone)
                {
                    cancellationToken.ThrowIfCancellationRequested();
                    await Task.Yield().ConfigureAwait(false);
                }
            }
            if (webRequest.result == UnityWebRequest.Result.ConnectionError ||
                webRequest.result == UnityWebRequest.Result.DataProcessingError)
            {
                var message = webRequest.error ?? "sse connect failed";
                webRequest.Dispose();
                throw new TransportException(message, null);
            }
            _ = operation; // 头等待循环已用;连接侧只看 webRequest.isDone
            return new SseWebRequestConnection(webRequest, handler);
        }
    }

    /// <summary>增量收体缓冲(主线程 ReceiveData 写;读侧加锁按 \n 切行,多字节安全)。</summary>
    sealed class SseBufferHandler : DownloadHandlerScript
    {
        readonly object _gate = new object();
        readonly List<byte> _buffer = new List<byte>();

        public string TryTakeLine()
        {
            lock (_gate)
            {
                var nl = _buffer.IndexOf((byte)'\n');
                if (nl < 0)
                {
                    return null;
                }
                var line = Encoding.UTF8.GetString(_buffer.GetRange(0, nl).ToArray());
                _buffer.RemoveRange(0, nl + 1);
                return line;
            }
        }

        protected override bool ReceiveData(byte[] data, int dataLength)
        {
            lock (_gate)
            {
                for (var i = 0; i < dataLength; i++)
                {
                    _buffer.Add(data[i]);
                }
            }
            return true;
        }
    }

    sealed class SseWebRequestConnection : SseStreamConnection
    {
        readonly UnityWebRequest _webRequest;
        readonly SseBufferHandler _handler;

        public SseWebRequestConnection(UnityWebRequest webRequest, SseBufferHandler handler)
        {
            _webRequest = webRequest;
            _handler = handler;
            StatusCode = (int)webRequest.responseCode;
            Headers = new Dictionary<string, string>();
            var raw = webRequest.GetResponseHeaders();
            if (raw != null)
            {
                foreach (var kv in raw)
                {
                    Headers[kv.Key.ToLowerInvariant()] = kv.Value; // 键小写归一(契约 transport)
                }
            }
        }

        public override async Task<string> ReadLineAsync(CancellationToken cancellationToken)
        {
            while (true)
            {
                cancellationToken.ThrowIfCancellationRequested();
                var line = _handler.TryTakeLine();
                if (line != null)
                {
                    return line;
                }
                if (_webRequest.isDone)
                {
                    if (_webRequest.result == UnityWebRequest.Result.Success)
                    {
                        return null; // 流尾(chunked 终止块收讫/服务端关流)
                    }
                    throw new TransportException(_webRequest.error ?? "sse stream broken", null);
                }
                await Task.Delay(50, cancellationToken).ConfigureAwait(false);
            }
        }

        public override void Dispose()
        {
            _webRequest.Dispose();
        }
    }
}
