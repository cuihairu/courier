// L2 Core:错误归一(errors.md)——同一 wire code 在各端映射为同一枚举、同一重试语义。
// code 是客户端分支判断的唯一依据;message 人读,不得用于分支(errors.md)。
namespace Courier.Core
{
    /// <summary>归一后的契约错误。</summary>
    public sealed class CourierError
    {
        /// <summary>wire code(如 AUTH_TOKEN_EXPIRED);未知码容忍,分支走兜底。</summary>
        public string WireCode { get; }

        /// <summary>类型化枚举;未知码为 null(调用方以 WireCode 兜底)。</summary>
        public Contract.ErrorCode? TypedCode { get; }

        public string Message { get; }
        public bool Retryable { get; }
        public int Http { get; }

        /// <summary>失败响应 body 的 traceId;客户端上报问题时附带(契约 primitives.md)。</summary>
        public string TraceId { get; }

        /// <summary>Retry-After 响应头(秒);429 等场景由服务端给出。</summary>
        public int? RetryAfterSeconds { get; }

        public CourierError(string wireCode, string message, bool retryable, int http,
            string traceId, int? retryAfterSeconds)
        {
            WireCode = wireCode;
            Contract.ErrorCode typed;
            TypedCode = Contract.ErrorSpecs.TryParse(wireCode, out typed) ? (Contract.ErrorCode?)typed : null;
            Message = message;
            Retryable = retryable;
            Http = http;
            TraceId = traceId;
            RetryAfterSeconds = retryAfterSeconds;
        }

        public static CourierError InvalidArgument(string message)
        {
            return new CourierError(Contract.ErrorSpecs.All[(int)Contract.ErrorCode.CommonInvalidArgument].Wire,
                message, false, 400, null, null);
        }

        public static CourierError Unavailable(string message)
        {
            return new CourierError(Contract.ErrorSpecs.All[(int)Contract.ErrorCode.CommonUnavailable].Wire,
                message, true, 503, null, null);
        }

        public static CourierError Unauthenticated(string message)
        {
            return new CourierError("COMMON_UNAUTHENTICATED", message, false, 401, null, null);
        }

        public override string ToString()
        {
            return WireCode + " (" + Http + ", retryable=" + (Retryable ? "true" : "false") + "): " + Message;
        }
    }

    /// <summary>SDK 统一异常:携带归一错误;客户端 catch 后按 Error.WireCode/TypedCode 分支。</summary>
    public class CourierException : System.Exception
    {
        public CourierError Error { get; }

        public CourierException(CourierError error) : base(error.ToString())
        {
            Error = error;
        }
    }
}
