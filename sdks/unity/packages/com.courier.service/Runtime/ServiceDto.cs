// M2 服务域 DTO(契约 announcement.md / support.md Frozen v1)。
// wire camelCase 与字段语义与网关响应逐一对齐;nullable = 契约可选字段缺省。
using System.Collections.Generic;

namespace Courier.Service
{
    /// <summary>公告(契约数据模型;严重度大写枚举字符串)。</summary>
    public sealed class AnnouncementDto
    {
        public string Id { get; set; }
        public string Title { get; set; }
        public string Body { get; set; }
        public string Severity { get; set; }
        public string StartAt { get; set; }
        public string EndAt { get; set; }
        public string PublishedAt { get; set; }
    }

    /// <summary>分页(items + nextCursor;空串 = 末页,契约 primitives.md)。</summary>
    public sealed class PageDto<T>
    {
        public List<T> Items { get; set; }
        public string NextCursor { get; set; }

        public bool IsLastPage { get { return string.IsNullOrEmpty(NextCursor); } }
    }

    /// <summary>工单(状态 OPEN/REPLIED/CLOSED)。</summary>
    public sealed class TicketDto
    {
        public string Id { get; set; }
        public string Title { get; set; }
        public string Status { get; set; }
        public string Category { get; set; }
        public string CreatedAt { get; set; }
        public string UpdatedAt { get; set; }
    }

    /// <summary>工单消息(senderType: PLAYER/AGENT/SYSTEM,createdAt 升序)。</summary>
    public sealed class TicketMessageDto
    {
        public string SenderType { get; set; }
        public string Body { get; set; }
        public string CreatedAt { get; set; }
    }

    /// <summary>工单详情(ticket + messages)。</summary>
    public sealed class TicketDetailDto
    {
        public TicketDto Ticket { get; set; }
        public List<TicketMessageDto> Messages { get; set; }
    }

    /// <summary>FAQ 知识条目。</summary>
    public sealed class FaqDto
    {
        public string Id { get; set; }
        public string Question { get; set; }
        public string Answer { get; set; }
        public List<string> Keywords { get; set; }
    }

    /// <summary>提单请求(POST /support/tickets;category 缺省 OTHER)。</summary>
    public sealed class CreateTicketRequest
    {
        public string Title { get; set; }
        public string Body { get; set; }
        public string Category { get; set; }
    }

    /// <summary>追加消息请求(POST /support/tickets/{id}/messages)。</summary>
    public sealed class AppendMessageRequest
    {
        public string Body { get; set; }
    }
}
