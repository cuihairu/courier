// L2 Core:messages SSE 流式传输接口面(纯逻辑,无平台引用)。
// 平台接线(ISseSource 实现)归 L3 Adapter;重连编排(依赖 SseParser)在 Service
// 的 SseStreamClient——Core 定义接缝,不反向依赖 Service。
// 语义见 SseStreamClient;契约 messages.md 客户端要求:501 → 降级转纯拉取、
// 401 → 终态、断线指数退避重连、Retry-After 优先、净停经 CancellationToken。
using System;
using System.Collections.Generic;
using System.Threading;
using System.Threading.Tasks;

namespace Courier.Core
{
    /// <summary>一次流连接尝试的请求(基础头不含 Authorization,由 client 每次现取拼接)。</summary>
    public sealed class SseStreamRequest
    {
        public string Url { get; set; }
        public IDictionary<string, string> Headers { get; set; }
        public Func<string> TokenProvider { get; set; }
    }

    /// <summary>一次已建立的流连接:状态码 + 头(实现键小写归一)+ 逐行读(流尾回 null)。</summary>
    public abstract class SseStreamConnection : IDisposable
    {
        public int StatusCode { get; set; }
        public IDictionary<string, string> Headers { get; set; }

        /// <summary>读一行(不含换行符);流尾(连接关闭)回 null;网络中断抛 TransportException。</summary>
        public abstract Task<string> ReadLineAsync(CancellationToken cancellationToken);

        public abstract void Dispose();
    }

    /// <summary>平台接线面:单次连接尝试(网络失败抛 TransportException;建连后按响应码分流)。</summary>
    public interface ISseSource
    {
        Task<SseStreamConnection> ConnectAsync(SseStreamRequest request, CancellationToken cancellationToken);
    }

    /// <summary>通道收尾原因(onClosed 收)。</summary>
    public static class SseStreamCloseReasons
    {
        public const string CapabilityDisabled = "capability_disabled";
        public const string Unauthenticated = "unauthenticated";
        public const string Stopped = "stopped";
        public const string Disconnected = "disconnected";
    }

    /// <summary>重连退避(纯函数):base·2^(attempt-1) 封顶 max;Retry-After(毫秒)优先。</summary>
    public static class SseBackoff
    {
        public static int DelayMs(int attempt, int retryAfterMs, int baseMs, int maxMs)
        {
            if (retryAfterMs > 0)
            {
                return retryAfterMs; // 契约语义规则 3:Retry-After 头优先
            }
            if (attempt < 1)
            {
                attempt = 1;
            }
            maxMs = Math.Max(maxMs, 1);
            var d = Math.Min(Math.Max(baseMs, 1), maxMs);
            for (var i = 0; i < attempt - 1 && d < maxMs; i++)
            {
                d = Math.Min(d * 2, maxMs);
            }
            return d;
        }
    }
}
