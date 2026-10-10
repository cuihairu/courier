// 批次 37 验收:messages SSE 流式传输编排——帧上抛/心跳不出事件、断线退避重连
// (Authorization 每连现取)、501/401 终态不重试、Retry-After 优先、健康连重置退避、
// 连接失败重试、CancellationToken 净停;退避纯函数封顶/钳制。
using System;
using System.Collections.Generic;
using System.Threading;
using System.Threading.Tasks;
using Courier.Core;
using Courier.Service;
using Xunit;

namespace Courier.ServiceTests
{
    /// <summary>脚本化连接:按序回放行(string)或抛异常(Exception);行尽 = 流尾。</summary>
    sealed class FakeConnection : SseStreamConnection
    {
        readonly Queue<object> _script;

        public bool Disposed { get; private set; }

        public FakeConnection(int status, params object[] script)
        {
            StatusCode = status;
            Headers = new Dictionary<string, string>();
            _script = new Queue<object>(script);
        }

        public FakeConnection WithHeader(string key, string value)
        {
            Headers[key] = value;
            return this;
        }

        public override async Task<string> ReadLineAsync(CancellationToken cancellationToken)
        {
            cancellationToken.ThrowIfCancellationRequested();
            if (_script.Count == 0)
            {
                return null; // 流尾 = 断流
            }
            var item = _script.Dequeue();
            if (item is Exception ex)
            {
                throw ex;
            }
            await Task.Yield();
            return (string)item;
        }

        public override void Dispose()
        {
            Disposed = true;
        }
    }

    /// <summary>脚本化接缝:按序回放连接(FakeConnection / Exception);记录每次请求头。</summary>
    sealed class FakeSource : ISseSource
    {
        public readonly List<SseStreamRequest> Requests = new List<SseStreamRequest>();
        public readonly Queue<object> Connections = new Queue<object>();
        public int ConnectCount;

        public Task<SseStreamConnection> ConnectAsync(SseStreamRequest request, CancellationToken cancellationToken)
        {
            ConnectCount++;
            Requests.Add(request);
            cancellationToken.ThrowIfCancellationRequested();
            if (Connections.Count == 0)
            {
                return Task.FromResult<SseStreamConnection>(null);
            }
            var item = Connections.Dequeue();
            if (item is Exception ex)
            {
                throw ex;
            }
            return Task.FromResult((SseStreamConnection)item);
        }
    }

    public class SseStreamTests
    {
        static async Task WaitUntilAsync(Func<bool> cond, int timeoutMs = 5000)
        {
            var deadline = Environment.TickCount + timeoutMs;
            while (!cond())
            {
                if (Environment.TickCount > deadline)
                {
                    throw new TimeoutException("condition not met in time");
                }
                await Task.Delay(10);
            }
        }

        static SseStreamRequest Request(IDictionary<string, string> headers = null)
        {
            return new SseStreamRequest
            {
                Url = "http://gw.example/v1/messages/stream",
                Headers = headers ?? new Dictionary<string, string> { { "X-Courier-Game-Id", "game_demo" } },
                TokenProvider = null
            };
        }

        [Fact]
        public void Backoff_GrowsDoublesCapsAtMax()
        {
            Assert.Equal(1000, SseBackoff.DelayMs(1, 0, 1000, 60000));
            Assert.Equal(2000, SseBackoff.DelayMs(2, 0, 1000, 60000));
            Assert.Equal(4000, SseBackoff.DelayMs(3, 0, 1000, 60000));
            Assert.Equal(8000, SseBackoff.DelayMs(4, 0, 1000, 60000));
            Assert.Equal(60000, SseBackoff.DelayMs(7, 0, 1000, 60000)); // 封顶
            Assert.Equal(60000, SseBackoff.DelayMs(99, 0, 1000, 60000));
        }

        [Fact]
        public void Backoff_RetryAfterWinsAndBaseClamped()
        {
            Assert.Equal(7000, SseBackoff.DelayMs(1, 7000, 1000, 60000)); // Retry-After 优先
            Assert.Equal(500, SseBackoff.DelayMs(1, 0, 500, 60000));
            Assert.Equal(60000, SseBackoff.DelayMs(1, 0, 90000, 60000)); // base 超 max 钳制
            Assert.Equal(1, SseBackoff.DelayMs(0, 0, 0, 0)); // 全零配置不挂死
        }

        [Fact]
        public async Task RunAsync_FramesSurfaced_HeartbeatSilent_ReconnectRefreshesToken()
        {
            var source = new FakeSource();
            var tok = 0;
            var conn1 = new FakeConnection(200,
                "id: 1", "event: announcement.published", "data: {\"id\":\"ann_1\"}", "", ": ping", "");
            var conn2 = new FakeConnection(200,
                "id: 2", "event: config.updated", "data: {\"configVersion\":7}", "");
            source.Connections.Enqueue(conn1);
            source.Connections.Enqueue(conn2);
            var client = new SseStreamClient(source) { BackoffBaseMs = 0 };
            var events = new List<SseEvent>();
            var reasons = new List<string>();
            var request = Request();
            request.TokenProvider = () => "tok-" + (++tok);

            var run = client.RunAsync(request, events.Add, reasons.Add, CancellationToken.None);
            await WaitUntilAsync(() => events.Count >= 1);
            Assert.Equal("announcement.published", events[0].Type);
            Assert.Equal("1", events[0].Id);
            Assert.Equal("{\"id\":\"ann_1\"}", events[0].Data);
            await WaitUntilAsync(() => events.Count >= 2);
            Assert.Equal("config.updated", events[1].Type);
            await WaitUntilAsync(() => source.Requests.Count >= 2);

            Assert.True(conn1.Disposed); // 断流后连接必须释放
            Assert.Equal("Bearer tok-1", source.Requests[0].Headers["Authorization"]); // 首连现取
            Assert.Equal("Bearer tok-2", source.Requests[1].Headers["Authorization"]); // 重连再取(轮换)
            Assert.Equal("game_demo", source.Requests[1].Headers["X-Courier-Game-Id"]); // scope 头透传
            Assert.Empty(reasons); // 断线是重连语义,非终态

            // 净停:取消后循环退出,连接释放,不再发终态
            var cts = new CancellationTokenSource();
            var run2 = client.RunAsync(Request(), events.Add, reasons.Add, cts.Token);
            await WaitUntilAsync(() => source.ConnectCount >= 3);
            cts.Cancel();
            await WaitUntilAsync(() => run2.IsCompleted);
            Assert.Empty(reasons);
        }

        [Fact]
        public async Task RunAsync_501_CapabilityDisabledTerminal_NoRetry()
        {
            var source = new FakeSource();
            source.Connections.Enqueue(new FakeConnection(501));
            var client = new SseStreamClient(source);
            var reasons = new List<string>();
            await client.RunAsync(Request(), _ => { }, reasons.Add, CancellationToken.None);

            Assert.Single(reasons);
            Assert.Equal(SseStreamCloseReasons.CapabilityDisabled, reasons[0]); // 转纯拉取信号
            Assert.Equal(1, source.ConnectCount); // 不重试
        }

        [Fact]
        public async Task RunAsync_401_UnauthenticatedTerminal_NoRetry()
        {
            var source = new FakeSource();
            source.Connections.Enqueue(new FakeConnection(401));
            var client = new SseStreamClient(source);
            var reasons = new List<string>();
            await client.RunAsync(Request(), _ => { }, reasons.Add, CancellationToken.None);

            Assert.Single(reasons);
            Assert.Equal(SseStreamCloseReasons.Unauthenticated, reasons[0]);
            Assert.Equal(1, source.ConnectCount);
        }

        [Fact]
        public async Task RunAsync_Non200_RetryAfterPriorityObservable()
        {
            var source = new FakeSource();
            source.Connections.Enqueue(new FakeConnection(429).WithHeader("Retry-After", "7"));
            var client = new SseStreamClient(source) { BackoffBaseMs = 0 };
            var cts = new CancellationTokenSource();
            var run = client.RunAsync(Request(), _ => { }, _ => { }, cts.Token);
            await WaitUntilAsync(() => client.LastDelayMs == 7000); // Retry-After 秒 → 毫秒
            cts.Cancel();
            await WaitUntilAsync(() => run.IsCompleted);
            Assert.Equal(1, source.ConnectCount);
        }

        [Fact]
        public async Task RunAsync_ConnectThrowRetries_AndStoppedPath()
        {
            var source = new FakeSource();
            source.Connections.Enqueue(new TransportException("net down", null)); // 建连网络故障
            var conn = new FakeConnection(200, "id: 9", "event: support.ticket_replied", "data: {}", "");
            source.Connections.Enqueue(conn);
            var client = new SseStreamClient(source) { BackoffBaseMs = 0 };
            var events = new List<SseEvent>();
            var connectAtFirstEvent = -1;
            var cts = new CancellationTokenSource();

            var run = client.RunAsync(Request(), evt =>
            {
                if (connectAtFirstEvent < 0)
                {
                    connectAtFirstEvent = source.ConnectCount; // 自旋会继续计数,事件点才确定
                }
                events.Add(evt);
            }, _ => { }, cts.Token);
            await WaitUntilAsync(() => events.Count >= 1); // 故障后重连成功
            Assert.Equal("support.ticket_replied", events[0].Type);
            Assert.Equal(2, connectAtFirstEvent); // 首事件恰在第 2 次建连
            cts.Cancel();
            await WaitUntilAsync(() => run.IsCompleted);
        }

        [Fact]
        public async Task RunAsync_CancelledBeforeStart_NoConnect()
        {
            var source = new FakeSource();
            var client = new SseStreamClient(source);
            var cts = new CancellationTokenSource();
            cts.Cancel();
            await client.RunAsync(Request(), _ => { }, _ => { }, cts.Token);
            Assert.Equal(0, source.ConnectCount);
        }
    }
}
