// L3 messages 流式传输编排(纯逻辑,无平台引用):契约 messages.md 的
// GET /v1/messages/stream 长连客户端。平台网络细节在 ISseSource 实现
// (Unity 接线见 core Adapter/UnityWebRequestSseSource.cs);本件管重连编排:
// 501 → capability_disabled(降级转纯拉取,不重试)、401 → unauthenticated,
// 两者终态经 onClosed 收尾;断线指数退避重连(SseBackoff,1s 起、上限 60s、
// Retry-After 秒头优先);收过事件的连接断开即重置退避(健康连≠失败连);
// CancellationToken 净停;Authorization 每次尝试按 token provider 现取
// (契约:新连接按当前 token 校验);逐行喂 SseParser,未知 event 容忍。
using System;
using System.Collections.Generic;
using System.Threading;
using System.Threading.Tasks;
using Courier.Core;

namespace Courier.Service
{
    public sealed class SseStreamClient
    {
        readonly ISseSource _source;

        public int BackoffBaseMs { get; set; } = 1000;
        public int BackoffMaxMs { get; set; } = 60000;

        /// <summary>上一次重连前算得的退避毫秒(诊断/测试观察面;含 Retry-After 优先值)。</summary>
        public int LastDelayMs { get; private set; }

        public SseStreamClient(ISseSource source)
        {
            _source = source ?? throw new ArgumentNullException(nameof(source));
        }

        /// <summary>长连循环:直到 ct 取消或终态(501/401)。onEvent 收解析帧,onClosed 收收尾原因。</summary>
        public async Task RunAsync(SseStreamRequest request, Action<SseEvent> onEvent,
            Action<string> onClosed, CancellationToken cancellationToken)
        {
            if (request == null)
            {
                throw new ArgumentNullException(nameof(request));
            }
            var attempt = 0;
            while (!cancellationToken.IsCancellationRequested)
            {
                var outcome = await AttemptOnceAsync(request, onEvent, cancellationToken).ConfigureAwait(false);
                if (outcome.Reason == SseStreamCloseReasons.Stopped)
                {
                    return;
                }
                if (outcome.Reason == SseStreamCloseReasons.CapabilityDisabled ||
                    outcome.Reason == SseStreamCloseReasons.Unauthenticated)
                {
                    onClosed?.Invoke(outcome.Reason);
                    return;
                }
                attempt = outcome.Healthy ? 0 : attempt + 1;
                LastDelayMs = SseBackoff.DelayMs(attempt, outcome.RetryAfterMs, BackoffBaseMs, BackoffMaxMs);
                if (LastDelayMs > 0)
                {
                    try
                    {
                        await Task.Delay(LastDelayMs, cancellationToken).ConfigureAwait(false);
                    }
                    catch (OperationCanceledException)
                    {
                        return;
                    }
                }
            }
        }

        async Task<Attempt> AttemptOnceAsync(SseStreamRequest request, Action<SseEvent> onEvent,
            CancellationToken ct)
        {
            var conn = await ConnectSafeAsync(request, ct).ConfigureAwait(false);
            if (conn == null)
            {
                return Attempt.Disconnected();
            }
            var headers = Lowercased(conn.Headers);
            var code = conn.StatusCode;
            var retryAfterMs = ParseRetryAfterMs(headers);
            if (code == 501)
            {
                conn.Dispose();
                return Attempt.Terminal(SseStreamCloseReasons.CapabilityDisabled);
            }
            if (code == 401)
            {
                conn.Dispose();
                return Attempt.Terminal(SseStreamCloseReasons.Unauthenticated);
            }
            var healthy = false;
            if (code == 200)
            {
                var parser = new SseParser();
                try
                {
                    while (true)
                    {
                        ct.ThrowIfCancellationRequested();
                        var line = await conn.ReadLineAsync(ct).ConfigureAwait(false);
                        if (line == null)
                        {
                            break; // 流尾 = 断流,走重连
                        }
                        var evt = parser.FeedLine(line);
                        if (evt != null)
                        {
                            healthy = true;
                            onEvent?.Invoke(evt);
                        }
                    }
                }
                catch (OperationCanceledException)
                {
                    conn.Dispose();
                    return Attempt.Stopped();
                }
                catch (TransportException)
                {
                    // 中途断流:走重连
                }
            }
            conn.Dispose();
            return new Attempt
            {
                Reason = SseStreamCloseReasons.Disconnected,
                Healthy = healthy,
                RetryAfterMs = retryAfterMs
            };
        }

        async Task<SseStreamConnection> ConnectSafeAsync(SseStreamRequest request, CancellationToken ct)
        {
            var headers = new Dictionary<string, string>();
            if (request.Headers != null)
            {
                foreach (var kv in request.Headers)
                {
                    headers[kv.Key] = kv.Value;
                }
            }
            if (request.TokenProvider != null)
            {
                var token = request.TokenProvider() ?? "";
                if (token.Length > 0)
                {
                    headers["Authorization"] = "Bearer " + token; // 每连现取(契约:新连接按当前 token 校验)
                }
            }
            var attemptRequest = new SseStreamRequest
            {
                Url = request.Url,
                Headers = headers,
                TokenProvider = request.TokenProvider
            };
            try
            {
                return await _source.ConnectAsync(attemptRequest, ct).ConfigureAwait(false);
            }
            catch (OperationCanceledException)
            {
                return null;
            }
            catch (TransportException)
            {
                return null; // 网络失败 = 断流语义,走退避重连
            }
        }

        static IDictionary<string, string> Lowercased(IDictionary<string, string> headers)
        {
            var outHeaders = new Dictionary<string, string>();
            if (headers != null)
            {
                foreach (var kv in headers)
                {
                    outHeaders[kv.Key.ToLowerInvariant()] = kv.Value;
                }
            }
            return outHeaders;
        }

        static int ParseRetryAfterMs(IDictionary<string, string> headers)
        {
            if (headers != null && headers.TryGetValue("retry-after", out var value))
            {
                return int.TryParse(value.Trim(), out var seconds) && seconds > 0 ? seconds * 1000 : 0;
            }
            return 0;
        }

        sealed class Attempt
        {
            public string Reason { get; set; }
            public bool Healthy { get; set; }
            public int RetryAfterMs { get; set; }

            public static Attempt Disconnected()
            {
                return new Attempt { Reason = SseStreamCloseReasons.Disconnected };
            }

            public static Attempt Terminal(string reason)
            {
                return new Attempt { Reason = reason };
            }

            public static Attempt Stopped()
            {
                return new Attempt { Reason = SseStreamCloseReasons.Stopped };
            }
        }
    }
}
