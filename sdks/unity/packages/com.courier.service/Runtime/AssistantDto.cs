// M4 小助手域 DTO(契约 assistant.md Frozen v1)。answer 命中时才有;
// transferTicket 为预留字段(未命中时的转人工引导,v1 缺省)。
using System.Collections.Generic;

namespace Courier.Service
{
    /// <summary>知识条目快照(命中时原样返回;契约:不改写)。</summary>
    public sealed class AssistantAnswerDto
    {
        public string Id { get; set; }
        public string Question { get; set; }
        public string Answer { get; set; }
        public List<string> Keywords { get; set; }
    }

    /// <summary>查询结果:matched=false 时 answer 为 null 且 suggestTransfer=true
    /// (未命中不是错误,UI 展示转人工引导)。</summary>
    public sealed class AssistantQueryDto
    {
        public bool Matched { get; set; }
        public AssistantAnswerDto Answer { get; set; }
        public bool SuggestTransfer { get; set; }
    }
}
