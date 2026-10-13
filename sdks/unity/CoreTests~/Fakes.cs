// 纯逻辑单测助手:固定时钟 / 脚本化假传输 / 内存 token 存储。
using System;
using System.Collections.Generic;
using Courier.Core;

namespace Courier.CoreTests
{
    public sealed class FixedClock : IClock
    {
        public DateTimeOffset Now;

        public FixedClock(DateTimeOffset now)
        {
            Now = now;
        }

        public DateTimeOffset UtcNow { get { return Now; } }

        public void Advance(TimeSpan delta)
        {
            Now = Now.Add(delta);
        }
    }

    /// <summary>挂起响应句柄:SetResult 放行挂在传输上的请求(single-flight 等并发重叠场景)。</summary>
    public sealed class PendingResponse
    {
        readonly System.Threading.Tasks.TaskCompletionSource<TransportResponse> _tcs;
        readonly TransportResponse _response;

        internal PendingResponse(System.Threading.Tasks.TaskCompletionSource<TransportResponse> tcs,
            TransportResponse response)
        {
            _tcs = tcs;
            _response = response;
        }

        public System.Threading.Tasks.Task<TransportResponse> Task { get { return _tcs.Task; } }

        public void SetResult() { _tcs.TrySetResult(_response); }
    }

    /// <summary>脚本化传输:按入队顺序响应;耗尽后走 Fallback。</summary>
    public sealed class FakeTransport : ITransport
    {
        public readonly List<TransportRequest> Requests = new List<TransportRequest>();
        readonly Queue<Func<TransportRequest, TransportResponse>> _script =
            new Queue<Func<TransportRequest, TransportResponse>>();
        readonly Queue<PendingResponse> _pending = new Queue<PendingResponse>();
        readonly object _gate = new object();

        public Func<TransportRequest, TransportResponse> Fallback;
        public bool ThrowOnSend;  // 模拟网络故障(TransportException)

        public void Enqueue(Func<TransportRequest, TransportResponse> handler)
        {
            lock (_gate)
            {
                _script.Enqueue(handler);
            }
        }

        /// <summary>入队一个挂起响应:下一个请求拿到未完成任务,SetResult 放行。
        /// 用于构造真实并发重叠(如 refresh 单飞:第二路在第一路挂起期间进入)。</summary>
        public PendingResponse EnqueuePending(TransportResponse response)
        {
            var pending = new PendingResponse(
                new System.Threading.Tasks.TaskCompletionSource<TransportResponse>(
                    System.Threading.Tasks.TaskCreationOptions.RunContinuationsAsynchronously),
                response);
            lock (_gate)
            {
                _pending.Enqueue(pending);
            }
            return pending;
        }

        /// <summary>入队一个 JSON 响应。</summary>
        public void EnqueueJson(int status, string body, string retryAfter = null)
        {
            Enqueue(_ => new TransportResponse
            {
                StatusCode = status,
                Headers = retryAfter == null
                    ? new Dictionary<string, string>()
                    : new Dictionary<string, string> { { "Retry-After", retryAfter } },
                Body = body
            });
        }

        public System.Threading.Tasks.Task<TransportResponse> SendAsync(
            TransportRequest request, System.Threading.CancellationToken cancellationToken)
        {
            PendingResponse pending;
            lock (_gate)
            {
                Requests.Add(request);
                pending = _pending.Count > 0 ? _pending.Dequeue() : null;
            }
            if (pending != null)
            {
                return pending.Task;
            }
            if (ThrowOnSend)
            {
                throw new TransportException("network down", new System.Net.Sockets.SocketException());
            }
            lock (_gate)
            {
                if (_script.Count > 0)
                {
                    return System.Threading.Tasks.Task.FromResult(_script.Dequeue()(request));
                }
            }
            if (Fallback != null)
            {
                return System.Threading.Tasks.Task.FromResult(Fallback(request));
            }
            throw new InvalidOperationException("FakeTransport 脚本耗尽且无 Fallback");
        }
    }

    /// <summary>内存 token 存储(记录 Save/Clear 调用供断言)。</summary>
    public sealed class FakeTokenStore : ITokenStore
    {
        public StoredTokens Saved;
        public int SaveCalls;
        public int ClearCalls;

        public StoredTokens Load()
        {
            return Saved;
        }

        public void Save(StoredTokens tokens)
        {
            Saved = tokens;
            SaveCalls++;
        }

        public void Clear()
        {
            Saved = null;
            ClearCalls++;
        }
    }

    public static class Fixtures
    {
        public static readonly DateTimeOffset BaseTime =
            new DateTimeOffset(2026, 10, 8, 0, 0, 0, TimeSpan.Zero);

        public const string AccessExpires = "2026-10-08T00:14:30.000Z";  // 15 分钟,留 30s skew
        public const string RefreshExpires = "2026-11-07T00:00:00.000Z"; // 30 天

        /// <summary>auth.md 会话响应 wire 形状(camelCase,字段逐一对应契约)。</summary>
        public static string SessionJson(string accountType = "GUEST", string accountId = "acc_01J",
            string email = null, string access = "access-1", string refresh = "refresh-1",
            string deviceId = "device-1")
        {
            var emailPart = email == null ? "" : "\"email\":\"" + email + "\",";
            return "{\"account\":{\"id\":\"" + accountId + "\",\"type\":\"" + accountType + "\"," +
                   emailPart + "\"status\":\"ACTIVE\",\"createdAt\":\"2026-10-08T00:00:00.000Z\"}," +
                   "\"accessToken\":\"" + access + "\"," +
                   "\"accessExpiresAt\":\"" + AccessExpires + "\"," +
                   "\"refreshToken\":\"" + refresh + "\"," +
                   "\"refreshExpiresAt\":\"" + RefreshExpires + "\"," +
                   "\"deviceId\":\"" + deviceId + "\"}";
        }

        public static string Success(string dataJson)
        {
            return "{\"data\":" + dataJson + "}";
        }

        public static string Failure(string code, string message, bool retryable, string traceId = null)
        {
            var tid = traceId ?? new string('a', 32);
            return "{\"error\":{\"code\":\"" + code + "\",\"message\":\"" + message +
                   "\",\"retryable\":" + (retryable ? "true" : "false") + "},\"traceId\":\"" + tid + "\"}";
        }

        public static CourierConfig Config()
        {
            return new CourierConfig("game_demo", "prod", "https://api.example.com");
        }
    }
}
