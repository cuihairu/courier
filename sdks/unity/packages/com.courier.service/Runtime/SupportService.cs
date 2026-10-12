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

        /// <summary>申请附件直传(v1.1):返回短时效预签名 URL,客户端 PUT 直传存储;
        /// 超限 409 SUPPORT_ATTACHMENT_LIMIT、超限体积 413 SUPPORT_ATTACHMENT_TOO_LARGE、
        /// 存储未配置 501 COMMON_CAPABILITY_DISABLED。</summary>
        public async Task<AttachmentUploadResult> RequestAttachmentUploadAsync(string ticketId,
            AttachmentUploadRequest request, CancellationToken ct)
        {
            var json = await _api.SendAsync("POST",
                Prefix + "tickets/" + ticketId + "/attachments", request, true, ct)
                .ConfigureAwait(false);
            return Json.Deserialize<AttachmentUploadResult>(json);
        }

        /// <summary>提交举报(F45):受理即返回 id;进展不向举报人反馈(防骚扰设计),
        /// 无查询端点。</summary>
        public async Task<ReportResult> CreateReportAsync(ReportRequest request,
            CancellationToken ct)
        {
            var json = await _api.SendAsync("POST", Prefix + "reports", request, true, ct)
                .ConfigureAwait(false);
            return Json.Deserialize<ReportResult>(json);
        }

        /// <summary>我的处罚列表(F45,issuedAt 倒序;处罚判定与执行全在管理面,
        /// 此处只读投影)。</summary>
        public async Task<PageDto<PenaltyDto>> ListPenaltiesAsync(int limit, string cursor,
            CancellationToken ct)
        {
            var path = Prefix + "penalties?limit=" + limit;
            if (!string.IsNullOrEmpty(cursor))
            {
                path += "&cursor=" + cursor;
            }
            var json = await _api.SendAsync("GET", path, null, true, ct).ConfigureAwait(false);
            return Json.Deserialize<PageDto<PenaltyDto>>(json);
        }

        /// <summary>处罚详情(F45,含申诉状态);非本人 / 不存在一律 typed
        /// SUPPORT_PENALTY_NOT_FOUND(404,不泄露存在性)。</summary>
        public async Task<PenaltyDto> GetPenaltyAsync(string penaltyId, CancellationToken ct)
        {
            var json = await _api.SendAsync("GET", Prefix + "penalties/" + penaltyId, null, true, ct)
                .ConfigureAwait(false);
            return Json.Deserialize<PenaltyDto>(json);
        }

        /// <summary>提交申诉(F45):每处罚至多一个进行中申诉,重复 → typed
        /// SUPPORT_APPEAL_EXISTS(409,不重试);判定结果以拉取详情为准。</summary>
        public async Task<PenaltyDto> AppealPenaltyAsync(string penaltyId, CancellationToken ct)
        {
            var json = await _api.SendAsync("POST",
                Prefix + "penalties/" + penaltyId + "/appeal", null, true, ct)
                .ConfigureAwait(false);
            return Json.Deserialize<PenaltyDto>(json);
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
