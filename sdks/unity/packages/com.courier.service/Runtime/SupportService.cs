// M2 客服域客户端(契约 support.md:玩家侧工单/消息/FAQ)。
// 坐席回复的实时感知走 messages 通道(见 SseParser);通道关闭 → 拉取详情兜底。
using System.Threading;
using System.Threading.Tasks;
using Courier.Core;

namespace Courier.Service
{
    public sealed class SupportService
    {
        const string Prefix = "/v1/support/";
        readonly ApiClient _api;

        public SupportService(ApiClient api)
        {
            _api = api;
        }

        /// <summary>提单(category 可空 = OTHER);限流超限 typed RATE_LIMITED。</summary>
        public async Task<TicketDto> CreateTicketAsync(CreateTicketRequest request,
            CancellationToken ct)
        {
            var json = await _api.SendAsync("POST", Prefix + "tickets", request, true, ct)
                .ConfigureAwait(false);
            return Json.Deserialize<TicketDto>(json);
        }

        /// <summary>我的工单列表(updatedAt 倒序)。</summary>
        public async Task<PageDto<TicketDto>> ListTicketsAsync(int limit, string cursor,
            CancellationToken ct)
        {
            var path = Prefix + "tickets?limit=" + limit;
            if (!string.IsNullOrEmpty(cursor))
            {
                path += "&cursor=" + cursor;
            }
            var json = await _api.SendAsync("GET", path, null, true, ct)
                .ConfigureAwait(false);
            return Json.Deserialize<PageDto<TicketDto>>(json);
        }

        /// <summary>工单详情(ticket + messages 升序);非本人 → typed SUPPORT_TICKET_NOT_FOUND。</summary>
        public async Task<TicketDetailDto> GetTicketAsync(string ticketId, CancellationToken ct)
        {
            var json = await _api.SendAsync("GET", Prefix + "tickets/" + ticketId, null, true, ct)
                .ConfigureAwait(false);
            return Json.Deserialize<TicketDetailDto>(json);
        }

        /// <summary>追加玩家消息;工单 CLOSED → typed SUPPORT_TICKET_CLOSED(409,不重试)。</summary>
        public async Task<TicketMessageDto> AppendMessageAsync(string ticketId, string body,
            CancellationToken ct)
        {
            var json = await _api.SendAsync("POST", Prefix + "tickets/" + ticketId + "/messages",
                new AppendMessageRequest { Body = body }, true, ct).ConfigureAwait(false);
            return Json.Deserialize<TicketMessageDto>(json);
        }

        /// <summary>FAQ 检索(关键词命中为空是常态,不是错误)。</summary>
        public async Task<PageDto<FaqDto>> FaqAsync(string keyword, int limit,
            CancellationToken ct)
        {
            var path = Prefix + "faq?limit=" + limit;
            if (!string.IsNullOrEmpty(keyword))
            {
                path += "&keyword=" + System.Uri.EscapeDataString(keyword);
            }
            var json = await _api.SendAsync("GET", path, null, true, ct)
                .ConfigureAwait(false);
            return Json.Deserialize<PageDto<FaqDto>>(json);
        }
    }
}
