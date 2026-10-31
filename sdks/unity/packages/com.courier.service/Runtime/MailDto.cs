// F62 邮件域 DTO(契约 mail.md Frozen v1)。邮件只是附件载体;发放归接入方钩子。
using System.Collections.Generic;

namespace Courier.Service
{
    /// <summary>附件(不透明物品描述;无第二价值单位)。</summary>
    public sealed class MailAttachmentDto
    {
        public string Id { get; set; }
        public string Type { get; set; }
        public long Amount { get; set; }
        public string Currency { get; set; }
    }

    /// <summary>邮件(列表/详情;createdAt 倒序)。</summary>
    public sealed class MailDto
    {
        public string Id { get; set; }
        public string Title { get; set; }
        public string Body { get; set; }
        public List<MailAttachmentDto> Attachments { get; set; }
        public string CreatedAt { get; set; }
        public string ExpiresAt { get; set; }
    }

    public sealed class MailPageDto
    {
        public List<MailDto> Items { get; set; }
        public string NextCursor { get; set; }
    }
}
