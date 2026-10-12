// M2 服务域 DTO(契约 announcement.md / support.md Frozen v1.1)。
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
        public Dictionary<string, List<string>> Filters { get; set; } // 定向过滤(v1.1;缺省 = 全量可见)
    }

    /// <summary>列表定向参数(v1.1):缺省维度不过滤;值须与发布 filters 维度值精确匹配。</summary>
    public sealed class AnnouncementQuery
    {
        public string Platform { get; set; }
        public string AppVersion { get; set; }
        public string Region { get; set; }
        public string Language { get; set; }

        internal string ToQueryString()
        {
            var sb = new System.Text.StringBuilder();
            Append(sb, "platform", Platform);
            Append(sb, "appVersion", AppVersion);
            Append(sb, "region", Region);
            Append(sb, "language", Language);
            return sb.ToString();
        }

        static void Append(System.Text.StringBuilder sb, string key, string value)
        {
            if (string.IsNullOrEmpty(value))
            {
                return;
            }
            sb.Append('&').Append(key).Append('=').Append(System.Uri.EscapeDataString(value));
        }
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
        public Dictionary<string, object> Context { get; set; } // 服务端零解释透传(v1.1)
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

    /// <summary>提单请求(POST /support/tickets;category 缺省 OTHER)。Context 为 v1.1
    /// 开集 map:键 1–32 位 [a-zA-Z0-9_.],≤10 键,值 string/number/boolean ≤256 字符,
    /// 整体 ≤1KiB;服务端零解释透传回 ticket.context。</summary>
    public sealed class CreateTicketRequest
    {
        public string Title { get; set; }
        public string Body { get; set; }
        public string Category { get; set; }
        public Dictionary<string, object> Context { get; set; }
    }

    /// <summary>追加消息请求(POST /support/tickets/{id}/messages)。</summary>
    public sealed class AppendMessageRequest
    {
        public string Body { get; set; }
    }
}
