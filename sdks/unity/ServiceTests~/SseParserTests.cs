// 批次 6 验收:SSE 帧解析器对齐 messages.md 契约——
// 帧结构 event/data/id、心跳注释忽略、未知字段容忍、data 多行连接、Reset 复用。
using Courier.Service;
using Xunit;

namespace Courier.ServiceTests
{
    public class SseParserTests
    {
        static SseEvent Feed(SseParser parser, params string[] lines)
        {
            SseEvent evt = null;
            foreach (var line in lines)
            {
                var result = parser.FeedLine(line);
                if (result != null)
                {
                    evt = result;
                }
            }
            return evt;
        }

        [Fact]
        public void FullFrame_ParsesTypeDataId()
        {
            var parser = new SseParser();
            var evt = Feed(parser,
                "event: announcement.published",
                "id: 7",
                "data: {\"announcement\":{\"id\":\"ann_01J\"}}",
                "");

            Assert.NotNull(evt);
            Assert.Equal("announcement.published", evt.Type); // 两段小写契约
            Assert.Equal("{\"announcement\":{\"id\":\"ann_01J\"}}", evt.Data);
            Assert.Equal("7", evt.Id);
        }

        [Fact]
        public void HeartbeatAndConnected_CommentsIgnored()
        {
            var parser = new SseParser();
            // : connected 初始帧 + : ping 心跳——均无业务事件。
            Assert.Null(parser.FeedLine(": connected"));
            Assert.Null(parser.FeedLine(""));
            Assert.Null(parser.FeedLine(": ping"));
            Assert.Null(parser.FeedLine(""));
            Assert.Null(parser.FeedLine(": ping"));
            Assert.Null(parser.FeedLine(""));
        }

        [Fact]
        public void BusinessEventAfterHeartbeat_Parses()
        {
            var parser = new SseParser();
            Feed(parser, ": connected", "", ": ping", "");
            var evt = Feed(parser,
                "event: support.ticket_replied",
                "id: 1",
                "data: {\"ticketId\":\"tkt_01J\"}",
                "");

            Assert.NotNull(evt);
            Assert.Equal("support.ticket_replied", evt.Type);
            Assert.Equal("tkt_01J", SseDataTicketId(evt.Data));
        }

        static string SseDataTicketId(string data)
        {
            // 最小解包:不引 Newtonsoft,直接抽 ticketId 断言 data JSON 形状。
            var idx = data.IndexOf("\"ticketId\":\"");
            var rest = data.Substring(idx + "\"ticketId\":\"".Length);
            return rest.Substring(0, rest.IndexOf('"'));
        }

        [Fact]
        public void MultiDataLines_JoinedWithNewline()
        {
            var parser = new SseParser();
            var evt = Feed(parser,
                "event: a.b",
                "data: line1",
                "data: line2",
                "");

            Assert.Equal("line1\nline2", evt.Data); // SSE 规范拼接
        }

        [Fact]
        public void NoSpaceAfterColon_Tolerated()
        {
            var parser = new SseParser();
            var evt = Feed(parser,
                "event:a.b",
                "id:1",
                "data:{\"x\":1}",
                "");

            Assert.Equal("a.b", evt.Type);
            Assert.Equal("{\"x\":1}", evt.Data);
            Assert.Equal("1", evt.Id);
        }

        [Fact]
        public void UnknownField_Tolerated()
        {
            var parser = new SseParser();
            var evt = Feed(parser,
                "retry: 3000",          // SSE 标准字段,本契约未用
                "x-custom: whatever",   // 未来扩展字段
                "event: c.d",
                "data: {}",
                "");

            Assert.NotNull(evt);
            Assert.Equal("c.d", evt.Type);
        }

        [Fact]
        public void ConsecutiveFrames_Independent()
        {
            var parser = new SseParser();
            var first = Feed(parser, "event: a.b", "id: 1", "data: 1", "");
            var second = Feed(parser, "event: c.d", "id: 2", "data: 2", "");

            Assert.Equal("1", first.Data);
            Assert.Equal("2", second.Data); // 帧间无串扰
            Assert.Equal("2", second.Id);
        }

        [Fact]
        public void Reset_ClearsPartialFrame()
        {
            var parser = new SseParser();
            parser.FeedLine("event: a.b");
            parser.FeedLine("data: half"); // 断线:帧未结算
            parser.Reset();

            var evt = Feed(parser, "event: c.d", "data: fresh", "");
            Assert.Equal("fresh", evt.Data); // 残留不带入新帧
            Assert.Equal("c.d", evt.Type);
        }
    }
}
