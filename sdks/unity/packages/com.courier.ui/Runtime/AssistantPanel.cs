// 批次 9 UI 包:小助手面板(M4,契约 assistant.md「客户端要求」)。
// 问答流:提问 → 命中渲染答案原样快照;未命中(非错误)→ 转人工引导:
// 一键经 support 域提单(标题带 [助手] 前缀),后续坐席回复走既有客服面板链路
// (support.ticket_replied 实时 / 拉取兜底)。能力未接(501→null)→ QueryFailed
// 出口,UI 隐藏入口。
using System;
using System.Threading;
using System.Threading.Tasks;
using Courier.Service;
using UnityEngine;

namespace Courier.UI
{
    public sealed class AssistantPanel : MonoBehaviour
    {
        /// <summary>Branding 默认标(消费点在 UI 包,兜底链同其余面板)。</summary>
        public const string DefaultTitle = "助手";

        [Tooltip("宿主装配:与登录后的 CourierClient 同生命周期")]
        public CourierServices Services;
        public string BrandTitle = DefaultTitle;

        /// <summary>渲染出口:命中时答案条目;未命中时 transferSuggested=true。</summary>
        public event Action<AssistantQueryDto> AnswerRendered;

        /// <summary>转人工完成:新工单 ID(游戏侧可跳转客服面板)。</summary>
        public event Action<string> TicketCreated;

        /// <summary>错误出口:typed 错误码 + 能力未接(null 结果)。</summary>
        public event Action<string> QueryFailed;

        /// <summary>品牌兜底链标题:远端 productName → 宿主 BrandTitle → 内置默认。</summary>
        public string ResolveTitle()
        {
            string remote;
            return BrandingCatalog.TryGetString("productName", out remote) &&
                !string.IsNullOrEmpty(remote) ? remote : BrandTitle;
        }

        /// <summary>提问。未命中不进错误路径:渲染 SuggestTransfer=true 的结果
        /// (契约「未命中不是错误」)。</summary>
        public async Task AskAsync(string text, CancellationToken ct)
        {
            if (Services == null)
            {
                return;
            }
            try
            {
                var result = await Services.Assistant.QueryAsync(text, ct);
                if (result == null)
                {
                    QueryFailed?.Invoke("COMMON_CAPABILITY_DISABLED"); // 契约:隐藏入口
                    return;
                }
                AnswerRendered?.Invoke(result);
            }
            catch (CourierException ex)
            {
                QueryFailed?.Invoke(ex.Error.WireCode);
            }
        }

        /// <summary>一键转人工(未命中后调用):经 support 域提单,把玩家问题带进工单。
        /// 提单成功后坐席侧照常回复,实时感知复用既有链路。</summary>
        public async Task TransferToHumanAsync(string question, CancellationToken ct)
        {
            if (Services == null)
            {
                return;
            }
            try
            {
                var ticket = await Services.Support.CreateTicketAsync(
                    new CreateTicketRequest
                    {
                        Title = Truncate("[助手] " + question, 120),
                        Body = question,
                    }, ct);
                TicketCreated?.Invoke(ticket.Id);
            }
            catch (CourierException ex)
            {
                QueryFailed?.Invoke(ex.Error.WireCode);
            }
        }

        static string Truncate(string s, int maxRunes)
        {
            if (s.Length <= maxRunes)
            {
                return s;
            }
            return s.Substring(0, maxRunes);
        }
    }
}
