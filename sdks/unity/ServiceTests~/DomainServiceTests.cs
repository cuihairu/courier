// 批次 6 验收:service 包域服务与 announcement/support 契约 wire 对齐。
// 走真 CourierClient(会话态)→ CourierServices 门面 → FakeTransport 断言
// 方法/路径/头/请求体/响应解析/typed 错误,与网关 herald/croupier 路由逐一对齐。
using System.Linq;
using System.Threading;
using System.Threading.Tasks;
using Courier;
using Courier.Core;
using Courier.Identity;
using Courier.Service;
using Courier.CoreTests;
using Xunit;

namespace Courier.ServiceTests
{
    public class DomainServiceTests
    {
        static (CourierServices, FakeTransport) NewServices()
        {
            var transport = new FakeTransport();
            var client = new CourierClient(Fixtures.Config(), transport, new FakeTokenStore(),
                new FixedClock(Fixtures.BaseTime));
            transport.EnqueueJson(200, Fixtures.Success(Fixtures.SessionJson()));
            client.InitAsync(CancellationToken.None).Wait();
            client.GuestAsync(new GuestRequest { DeviceId = "device-1", Platform = "ios" },
                CancellationToken.None).Wait();
            return (new CourierServices(client), transport);
        }

        static string AnnouncementJson(string id = "ann_01J") =>
            "{\"id\":\"" + id + "\",\"title\":\"维护公告\",\"body\":\"10 月 9 日 02:00-04:00 停机维护\"," +
            "\"severity\":\"WARNING\",\"startAt\":\"2026-10-08T00:00:00.000Z\"," +
            "\"endAt\":\"2026-10-09T00:00:00.000Z\",\"publishedAt\":\"2026-10-08T00:00:00.000Z\"}";

        // --- 公告域(herald 路由:GET /v1/announcements、GET /v1/announcements/{id})---

        [Fact]
        public async Task Announcement_List_WireAndParse()
        {
            var (svc, transport) = NewServices();
            transport.EnqueueJson(200, Fixtures.Success(
                "{\"items\":[" + AnnouncementJson() + "],\"nextCursor\":\"\"}"));

            var page = await svc.Announcements.ListAsync(2, null, CancellationToken.None);

            var req = transport.Requests.Last();
            Assert.Equal("GET", req.Method);
            Assert.Equal("https://api.example.com/v1/announcements?limit=2", req.Url);
            Assert.Equal("Bearer access-1", req.Headers["Authorization"]);
            Assert.Equal("game_demo", req.Headers[ScopeHeaders.GameId]);
            // 解析逐字段对齐 announcement.md。
            var item = Assert.Single(page.Items);
            Assert.Equal("ann_01J", item.Id);
            Assert.Equal("维护公告", item.Title);
            Assert.Equal("WARNING", item.Severity);
            Assert.Equal("2026-10-08T00:00:00.000Z", item.PublishedAt);
            Assert.True(page.IsLastPage); // nextCursor 空 = 末页
        }

        [Fact]
        public async Task Announcement_List_CursorPaged()
        {
            var (svc, transport) = NewServices();
            transport.EnqueueJson(200, Fixtures.Success(
                "{\"items\":[" + AnnouncementJson("ann_02") + "],\"nextCursor\":\"2\"}"));

            var page = await svc.Announcements.ListAsync(1, "2", CancellationToken.None);

            var req = transport.Requests.Last();
            Assert.Equal("https://api.example.com/v1/announcements?limit=1&cursor=2", req.Url);
            Assert.False(page.IsLastPage);
            Assert.Equal("2", page.NextCursor);
        }

        [Fact]
        public async Task Announcement_Detail_Parses()
        {
            var (svc, transport) = NewServices();
            transport.EnqueueJson(200, Fixtures.Success(AnnouncementJson("ann_09")));

            var dto = await svc.Announcements.GetAsync("ann_09", CancellationToken.None);

            var req = transport.Requests.Last();
            Assert.Equal("https://api.example.com/v1/announcements/ann_09", req.Url);
            Assert.Equal("ann_09", dto.Id);
            Assert.Equal("2026-10-09T00:00:00.000Z", dto.EndAt); // 字段原样透传,不本地加工
        }

        [Fact]
        public async Task Announcement_NotFound_TypedError_NoRetry()
        {
            var (svc, transport) = NewServices();
            transport.EnqueueJson(404, Fixtures.Failure("ANNOUNCEMENT_NOT_FOUND", "gone", false));

            var ex = await Assert.ThrowsAsync<CourierException>(
                () => svc.Announcements.GetAsync("ann_x", CancellationToken.None));

            Assert.Equal("ANNOUNCEMENT_NOT_FOUND", ex.Error.WireCode);
            Assert.False(ex.Error.Retryable);
            Assert.Equal(2, transport.Requests.Count); // 404 不重试(1 次 = 建会话 + 1 次 = 详情)
        }

        // --- 客服域(croupier 路由:POST/GET tickets、detail、messages、faq)---

        [Fact]
        public async Task Support_CreateTicket_WireAndParse()
        {
            var (svc, transport) = NewServices();
            transport.EnqueueJson(200, Fixtures.Success(
                "{\"id\":\"tkt_01J\",\"title\":\"闪退\",\"status\":\"OPEN\"," +
                "\"category\":\"BUG\",\"createdAt\":\"2026-10-08T00:00:00.000Z\"," +
                "\"updatedAt\":\"2026-10-08T00:00:00.000Z\"}"));

            var ticket = await svc.Support.CreateTicketAsync(
                new CreateTicketRequest { Title = "闪退", Body = "进副本必闪退", Category = "BUG" },
                CancellationToken.None);

            var req = transport.Requests.Last();
            Assert.Equal("POST", req.Method);
            Assert.Equal("https://api.example.com/v1/support/tickets", req.Url);
            Assert.Equal("{\"title\":\"闪退\",\"body\":\"进副本必闪退\",\"category\":\"BUG\"}",
                req.JsonBody);
            Assert.Equal("tkt_01J", ticket.Id);
            Assert.Equal("OPEN", ticket.Status);
        }

        [Fact]
        public async Task Support_Detail_MessagesAscending()
        {
            var (svc, transport) = NewServices();
            transport.EnqueueJson(200, Fixtures.Success(
                "{\"ticket\":{\"id\":\"tkt_01J\",\"title\":\"闪退\",\"status\":\"REPLIED\"," +
                "\"category\":\"BUG\",\"createdAt\":\"2026-10-08T00:00:00.000Z\"," +
                "\"updatedAt\":\"2026-10-08T00:05:00.000Z\"}," +
                "\"messages\":[" +
                "{\"senderType\":\"PLAYER\",\"body\":\"进副本必闪退\",\"createdAt\":\"2026-10-08T00:00:00.000Z\"}," +
                "{\"senderType\":\"AGENT\",\"body\":\"已定位,请更新包体\",\"createdAt\":\"2026-10-08T00:05:00.000Z\"}]}"));

            var detail = await svc.Support.GetTicketAsync("tkt_01J", CancellationToken.None);

            var req = transport.Requests.Last();
            Assert.Equal("https://api.example.com/v1/support/tickets/tkt_01J", req.Url);
            Assert.Equal("REPLIED", detail.Ticket.Status);
            Assert.Equal(2, detail.Messages.Count);
            Assert.Equal("PLAYER", detail.Messages[0].SenderType);
            Assert.Equal("AGENT", detail.Messages[1].SenderType); // 升序:玩家在前坐席在后
        }

        [Fact]
        public async Task Support_Append_Closed_TypedError()
        {
            var (svc, transport) = NewServices();
            transport.EnqueueJson(409, Fixtures.Failure("SUPPORT_TICKET_CLOSED", "closed", false));

            var ex = await Assert.ThrowsAsync<CourierException>(
                () => svc.Support.AppendMessageAsync("tkt_01J", "还在闪退", CancellationToken.None));

            Assert.Equal("SUPPORT_TICKET_CLOSED", ex.Error.WireCode);
            Assert.False(ex.Error.Retryable);
            Assert.Equal(2, transport.Requests.Count); // 409 不重试
        }

        [Fact]
        public async Task Support_Faq_KeywordUrlEncoded()
        {
            var (svc, transport) = NewServices();
            transport.EnqueueJson(200, Fixtures.Success(
                "{\"items\":[{\"id\":\"faq_01J\",\"question\":\"如何充值?\"," +
                "\"answer\":\"游戏内商城\",\"keywords\":[\"充值\",\"支付\"]}],\"nextCursor\":\"\"}"));

            var page = await svc.Support.FaqAsync("充值", 10, CancellationToken.None);

            var req = transport.Requests.Last();
            Assert.Equal("https://api.example.com/v1/support/faq?limit=10&keyword=" +
                System.Uri.EscapeDataString("充值"), req.Url);
            Assert.Equal("如何充值?", page.Items[0].Question);
            Assert.Equal(2, page.Items[0].Keywords.Count);
        }

        [Fact]
        public async Task Support_RateLimited_TypedError()
        {
            var (svc, transport) = NewServices();
            transport.EnqueueJson(429, Fixtures.Failure("RATE_LIMITED", "too many", false),
                retryAfter: "30");

            var ex = await Assert.ThrowsAsync<CourierException>(
                () => svc.Support.CreateTicketAsync(
                    new CreateTicketRequest { Title = "t", Body = "b" }, CancellationToken.None));

            Assert.Equal("RATE_LIMITED", ex.Error.WireCode);
            Assert.Equal(30, ex.Error.RetryAfterSeconds);
        }
    }
}
