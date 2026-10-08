// L2 Core:重试与超时(errors.md:retryable=true 可按退避自动重试,配合 Retry-After)。
// 只重试「可重试错误」:网络异常、5xx、429;幂等性由调用方保证(读操作默认可重试)。
using System;

namespace Courier.Core
{
    public sealed class RetryPolicy
    {
        public int MaxAttempts { get; }
        public TimeSpan BaseDelay { get; }
        public TimeSpan MaxDelay { get; }

        public static readonly RetryPolicy Default = new RetryPolicy(3, TimeSpan.FromMilliseconds(300), TimeSpan.FromSeconds(5));

        public RetryPolicy(int maxAttempts, TimeSpan baseDelay, TimeSpan maxDelay)
        {
            MaxAttempts = maxAttempts < 1 ? 1 : maxAttempts;
            BaseDelay = baseDelay;
            MaxDelay = maxDelay;
        }

        /// <summary>第 attempt 次失败后(1 起算)是否重试;delay 为建议等待时长。</summary>
        public bool ShouldRetry(int attempt, CourierError error, out TimeSpan delay)
        {
            if (attempt >= MaxAttempts || error == null || !error.Retryable)
            {
                delay = TimeSpan.Zero;
                return false;
            }
            // 指数退避:base * 2^(attempt-1),封顶 MaxDelay。
            var backoff = TimeSpan.FromMilliseconds(BaseDelay.TotalMilliseconds * Math.Pow(2, attempt - 1));
            if (backoff > MaxDelay) backoff = MaxDelay;
            // 服务端给了 Retry-After:以其为准(也不超过 MaxDelay)。
            if (error.RetryAfterSeconds.HasValue)
            {
                var server = TimeSpan.FromSeconds(error.RetryAfterSeconds.Value);
                if (server > MaxDelay) server = MaxDelay;
                if (server > backoff) backoff = server;
            }
            delay = backoff;
            return true;
        }
    }
}
