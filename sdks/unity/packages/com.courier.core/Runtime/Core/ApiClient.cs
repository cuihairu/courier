// L2 Core:API 客户端——拼 scope 头、发请求、解析信封、按 RetryPolicy 重试、
// AUTH_TOKEN_EXPIRED 时经 RefreshHook 换发后重放一次(自动 refresh,契约 auth.md)。
using System;
using System.Collections.Generic;
using System.Threading;
using System.Threading.Tasks;

namespace Courier.Core
{
    public sealed class ApiClient
    {
        const int DefaultTimeoutMs = 10000;

        readonly CourierConfig _config;
        readonly ITransport _transport;
        readonly RetryPolicy _retry;

        /// <summary>每次请求现取 access token(轮换后自动为新值);null = 匿名请求。</summary>
        public Func<string> AccessTokenProvider { get; set; }

        /// <summary>access 过期(AUTH_TOKEN_EXPIRED)时尝试 refresh;返回 true 则重放一次。</summary>
        public Func<Task<bool>> RefreshHook { get; set; }

        public ApiClient(CourierConfig config, ITransport transport, RetryPolicy retry)
        {
            _config = config;
            _transport = transport;
            _retry = retry ?? RetryPolicy.Default;
        }

        /// <summary>发送请求并解信封;返回 data 原始 JSON;失败抛 CourierException。</summary>
        public async Task<string> SendAsync(string method, string path, object body,
            bool withAuth, CancellationToken cancellationToken)
        {
            var headers = new Dictionary<string, string>
            {
                { ScopeHeaders.GameId, _config.GameId },
                { ScopeHeaders.Env, _config.Env }
            };
            var token = withAuth && AccessTokenProvider != null ? AccessTokenProvider() : null;
            if (withAuth && string.IsNullOrEmpty(token))
            {
                // 本地预检:未认证不发包(契约 COMMON_UNAUTHENTICATED)。
                throw new CourierException(CourierError.Unauthenticated("not authenticated"));
            }
            if (!string.IsNullOrEmpty(token))
            {
                headers["Authorization"] = "Bearer " + token;
            }
            if (body != null)
            {
                headers["Content-Type"] = "application/json";
            }

            var request = new TransportRequest
            {
                Method = method,
                Url = _config.Endpoint.TrimEnd('/') + path,
                Headers = headers,
                JsonBody = body == null ? null : Json.Serialize(body),
                TimeoutMs = DefaultTimeoutMs
            };

            var refreshed = false;
            for (int attempt = 1; ; attempt++)
            {
                CourierError error;
                try
                {
                    var response = await _transport.SendAsync(request, cancellationToken).ConfigureAwait(false);
                    var result = EnvelopeParser.Parse(response);
                    if (result.IsSuccess)
                    {
                        return result.DataJson;
                    }
                    error = result.Error;
                }
                catch (TransportException e)
                {
                    error = CourierError.Unavailable("transport failed: " + e.Message);
                }

                // access 过期:refresh 一次后重放(不计入退避重试次数)。
                if (error.WireCode == "AUTH_TOKEN_EXPIRED" && RefreshHook != null && !refreshed)
                {
                    refreshed = true;
                    if (await RefreshHook().ConfigureAwait(false))
                    {
                        token = AccessTokenProvider != null ? AccessTokenProvider() : null;
                        if (!string.IsNullOrEmpty(token))
                        {
                            headers["Authorization"] = "Bearer " + token;
                        }
                        attempt--;
                        continue;
                    }
                    throw new CourierException(error);
                }

                TimeSpan delay;
                if (_retry.ShouldRetry(attempt, error, out delay))
                {
                    if (delay > TimeSpan.Zero)
                    {
                        await Task.Delay(delay, cancellationToken).ConfigureAwait(false);
                    }
                    continue;
                }
                throw new CourierException(error);
            }
        }
    }
}
