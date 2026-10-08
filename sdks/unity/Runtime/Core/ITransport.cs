// L2 Core:传输接口(平台无关)。L3 Adapter 以各平台网络栈实现
// (Unity:UnityWebRequest;HTTP/JSON 见 docs/contract/primitives.md)。
using System.Collections.Generic;
using System.Threading;
using System.Threading.Tasks;

namespace Courier.Core
{
    /// <summary>一次 HTTP 请求(JSON 线格式,契约 primitives.md)。</summary>
    public sealed class TransportRequest
    {
        public string Method { get; set; }
        public string Url { get; set; }
        public IDictionary<string, string> Headers { get; set; }
        public string JsonBody { get; set; }
        public int TimeoutMs { get; set; }
    }

    /// <summary>一次 HTTP 响应(原样文本,信封解析归 Core)。</summary>
    public sealed class TransportResponse
    {
        public int StatusCode { get; set; }
        public IDictionary<string, string> Headers { get; set; }
        public string Body { get; set; }
    }

    public interface ITransport
    {
        /// <summary>发送请求;网络层异常由 Adapter 归一为 TransportException,Core 按 COMMON_UNAVAILABLE 语义重试。</summary>
        Task<TransportResponse> SendAsync(TransportRequest request, CancellationToken cancellationToken);
    }

    /// <summary>网络层异常(Adapter 抛出,Core 捕获后按 retryable 处理)。</summary>
    public class TransportException : System.Exception
    {
        public TransportException(string message, System.Exception inner) : base(message, inner) { }
    }
}
