// L2 Core:响应信封解析(契约 primitives.md Frozen v1)。
// 成功 {"data":...};失败 {"error":{code,message,retryable},"traceId":...};二者互斥。
// 未知字段容忍;未知错误码容忍(TypedCode=null,wire 保留,兜底分支)。
using System.Collections.Generic;

namespace Courier.Core
{
    /// <summary>信封解析结果:Data 与 Error 互斥。</summary>
    public sealed class EnvelopeResult
    {
        public string DataJson { get; }
        public CourierError Error { get; }

        public bool IsSuccess { get { return Error == null; } }

        public EnvelopeResult(string dataJson, CourierError error)
        {
            DataJson = dataJson;
            Error = error;
        }
    }

    public static class EnvelopeParser
    {
        public static EnvelopeResult Parse(TransportResponse response)
        {
            // 网络/网关异常可能没有 JSON body(5xx 网关直返、连接中断)。
            if (string.IsNullOrWhiteSpace(response.Body))
            {
                var fallback = new CourierError(
                    WireOf(response.StatusCode),
                    "empty response body (HTTP " + response.StatusCode + ")",
                    response.StatusCode >= 500 || response.StatusCode == 429,
                    response.StatusCode, null, RetryAfterOf(response.Headers));
                return new EnvelopeResult(null, fallback);
            }

            Newtonsoft.Json.Linq.JToken token;
            using (var reader = new Newtonsoft.Json.JsonTextReader(new System.IO.StringReader(response.Body)))
            {
                // RFC3339 时间串保持原样(JToken.Parse 默认会改写日期,契约要求毫秒精度 UTC 原文)。
                reader.DateParseHandling = Newtonsoft.Json.DateParseHandling.None;
                token = Newtonsoft.Json.Linq.JToken.ReadFrom(reader);
            }

            var error = token["error"];
            if (error != null)
            {
                var courierError = new CourierError(
                    (string)error["code"] ?? "",
                    (string)error["message"] ?? "",
                    (bool?)error["retryable"] ?? false,
                    response.StatusCode,
                    (string)token["traceId"],
                    RetryAfterOf(response.Headers));
                return new EnvelopeResult(null, courierError);
            }

            var data = token["data"];
            return new EnvelopeResult(data == null ? "{}" : data.ToString(Newtonsoft.Json.Formatting.None), null);
        }

        /// <summary>无信封时的兜底 wire code(errors.md HTTP 映射)。</summary>
        static string WireOf(int statusCode)
        {
            switch (statusCode)
            {
                case 400: return "COMMON_INVALID_ARGUMENT";
                case 401: return "COMMON_UNAUTHENTICATED";
                case 403: return "COMMON_PERMISSION_DENIED";
                case 404: return "COMMON_NOT_FOUND";
                case 429: return "RATE_LIMITED";
                case 501: return "COMMON_CAPABILITY_DISABLED";
                case 503: return "COMMON_UNAVAILABLE";
                default: return "COMMON_INTERNAL";
            }
        }

        static int? RetryAfterOf(IDictionary<string, string> headers)
        {
            if (headers == null) return null;
            string v;
            if (!headers.TryGetValue("Retry-After", out v) && !headers.TryGetValue("Retry-After".ToLowerInvariant(), out v))
            {
                return null;
            }
            int seconds;
            return int.TryParse(v, out seconds) ? (int?)seconds : null;
        }
    }
}
