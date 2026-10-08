// 重试策略与信封解析单测(契约 errors.md / primitives.md)。
using System;
using Courier.Core;
using Xunit;

namespace Courier.CoreTests
{
    public class RetryPolicyTests
    {
        static CourierError Retryable()
        {
            return CourierError.Unavailable("boom");
        }

        static CourierError NonRetryable()
        {
            return new CourierError("COMMON_INVALID_ARGUMENT", "bad", false, 400, null, null);
        }

        [Fact]
        public void NonRetryable_NeverRetried()
        {
            var p = new RetryPolicy(3, TimeSpan.FromMilliseconds(10), TimeSpan.FromSeconds(1));
            TimeSpan delay;
            Assert.False(p.ShouldRetry(1, NonRetryable(), out delay));
        }

        [Fact]
        public void Retryable_RetriesUpToMaxAttempts()
        {
            var p = new RetryPolicy(3, TimeSpan.FromMilliseconds(10), TimeSpan.FromSeconds(1));
            TimeSpan delay;
            Assert.True(p.ShouldRetry(1, Retryable(), out delay));
            Assert.True(p.ShouldRetry(2, Retryable(), out delay));
            Assert.False(p.ShouldRetry(3, Retryable(), out delay));  // 第 3 次失败后不再重试
        }

        [Fact]
        public void ExponentialBackoff_WithCap()
        {
            var p = new RetryPolicy(5, TimeSpan.FromMilliseconds(300), TimeSpan.FromSeconds(2));
            TimeSpan delay;
            p.ShouldRetry(1, Retryable(), out delay);
            Assert.Equal(300, delay.TotalMilliseconds);
            p.ShouldRetry(2, Retryable(), out delay);
            Assert.Equal(600, delay.TotalMilliseconds);
            p.ShouldRetry(3, Retryable(), out delay);
            Assert.Equal(1200, delay.TotalMilliseconds);
            p.ShouldRetry(4, Retryable(), out delay);
            Assert.Equal(2000, delay.TotalMilliseconds);  // 封顶 MaxDelay
        }

        [Fact]
        public void RetryAfterHeader_TakesPrecedence_AndCapped()
        {
            var p = new RetryPolicy(3, TimeSpan.FromMilliseconds(10), TimeSpan.FromSeconds(5));
            var err = new CourierError("RATE_LIMITED", "slow down", true, 429, null, 3);
            TimeSpan delay;
            Assert.True(p.ShouldRetry(1, err, out delay));
            Assert.Equal(3, delay.TotalSeconds);

            var huge = new CourierError("RATE_LIMITED", "slow down", true, 429, null, 3600);
            p.ShouldRetry(1, huge, out delay);
            Assert.Equal(5, delay.TotalSeconds);  // 封顶
        }
    }

    public class EnvelopeParserTests
    {
        static TransportResponse Resp(int status, string body)
        {
            return new TransportResponse
            {
                StatusCode = status,
                Headers = new System.Collections.Generic.Dictionary<string, string>(),
                Body = body
            };
        }

        [Fact]
        public void Success_ParsesData()
        {
            var r = EnvelopeParser.Parse(Resp(200, "{\"data\":{\"items\":[1,2]}}"));
            Assert.True(r.IsSuccess);
            Assert.Equal("{\"items\":[1,2]}", r.DataJson);
        }

        [Fact]
        public void Failure_ParsesErrorTraceIdRetryable()
        {
            var r = EnvelopeParser.Parse(Resp(401,
                "{\"error\":{\"code\":\"AUTH_TOKEN_EXPIRED\",\"message\":\"session expired\",\"retryable\":false}," +
                "\"traceId\":\"4bf92f3577b34da6a3ce929d0e0e4736\"}"));
            Assert.False(r.IsSuccess);
            Assert.Equal("AUTH_TOKEN_EXPIRED", r.Error.WireCode);
            Assert.Equal(Contract.ErrorCode.AuthTokenExpired, r.Error.TypedCode);
            Assert.False(r.Error.Retryable);
            Assert.Equal(32, r.Error.TraceId.Length);
        }

        [Fact]
        public void UnknownCode_Tolerated_TypedCodeNull()
        {
            // 契约 versioning.md:未知错误码必须容忍,落兜底分支。
            var r = EnvelopeParser.Parse(Resp(418,
                "{\"error\":{\"code\":\"TEAPOT_BREWING\",\"message\":\"x\",\"retryable\":true},\"traceId\":\"" +
                new string('0', 32) + "\"}"));
            Assert.False(r.IsSuccess);
            Assert.Equal("TEAPOT_BREWING", r.Error.WireCode);
            Assert.Null(r.Error.TypedCode);
            Assert.True(r.Error.Retryable);
        }

        [Fact]
        public void EmptyBody_5xx_FallbackRetryable()
        {
            var r = EnvelopeParser.Parse(Resp(502, ""));
            Assert.False(r.IsSuccess);
            Assert.Equal("COMMON_INTERNAL", r.Error.WireCode);
            Assert.True(r.Error.Retryable);
        }

        [Fact]
        public void EmptyBody_429_PicksRateLimitedWithRetryAfter()
        {
            var resp = Resp(429, "");
            resp.Headers["Retry-After"] = "7";
            var r = EnvelopeParser.Parse(resp);
            Assert.Equal("RATE_LIMITED", r.Error.WireCode);
            Assert.True(r.Error.Retryable);
            Assert.Equal(7, r.Error.RetryAfterSeconds);
        }

        [Fact]
        public void NonJson_Throws()
        {
            Assert.Throws<Newtonsoft.Json.JsonReaderException>(
                () => EnvelopeParser.Parse(Resp(200, "<html>not json</html>")));
        }
    }
}
