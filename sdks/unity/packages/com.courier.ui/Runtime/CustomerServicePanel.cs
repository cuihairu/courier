// 批次 6 UI 包首发:客服页面板骨架(todo 批次 6「客服页」)。
// 会话流:提单 → 追加消息 → 坐席回复感知。
// 实时感知:游戏侧把 messages 通道(SSE,经 SseParser 解析出
// support.ticket_replied)接到 NotifyTicketReplied → 面板重拉详情;
// 通道不可用时宿主可不接,面板仍有轮询/手动刷新兜底(契约拉取兜底语义)。
using System;
using System.Threading;
using System.Threading.Tasks;
using Courier.Service;
using UnityEngine;

namespace Courier.UI
{
    public sealed class CustomerServicePanel : MonoBehaviour
    {
        /// <summary>Branding 默认标(M3 Config 通道接通后可被远端覆盖)。</summary>
        public const string DefaultTitle = "客服";

        [Tooltip("宿主装配:与登录后的 CourierClient 同生命周期")]
        public CourierServices Services;
        public string BrandTitle = DefaultTitle;

        /// <summary>渲染出口:详情(工单 + 消息升序)交游戏侧控件。</summary>
        public event Action<TicketDetailDto> DetailRendered;

        /// <summary>错误出口:typed 错误码交游戏侧提示(RATE_LIMITED/CLOSED 等分支)。</summary>
        public event Action<string> ErrorRendered;

        string _currentTicketId;

        /// <summary>当前面板关注的工单(SSE 重拉判断用)。</summary>
        public string CurrentTicketId { get { return _currentTicketId; } }

        /// <summary>品牌兜底链标题:远端 productName → 宿主 BrandTitle → 内置默认
        /// (契约 branding.md「兜底规则」;消费点在 UI 包)。</summary>
        public string ResolveTitle()
        {
            string remote;
            return BrandingCatalog.TryGetString("productName", out remote) &&
                !string.IsNullOrEmpty(remote) ? remote : BrandTitle;
        }

        /// <summary>客服入口文案:branding supportEntry.label → Courier 默认标。</summary>
        public string SupportEntryLabel
        {
            get { return BrandingCatalog.GetSupportLabel(); }
        }

        /// <summary>提单并打开。category 可空(服务端缺省 OTHER)。</summary>
        public async Task OpenTicketAsync(string title, string body, string category,
            CancellationToken ct)
        {
            if (Services == null)
            {
                return;
            }
            try
            {
                var ticket = await Services.Support.CreateTicketAsync(
                    new CreateTicketRequest { Title = title, Body = body, Category = category }, ct);
                await LoadTicketAsync(ticket.Id, ct);
            }
            catch (CourierException ex)
            {
                ErrorRendered?.Invoke(ex.Error.WireCode);
            }
        }

        /// <summary>拉取工单详情并渲染(手动刷新/轮询兜底走这里)。</summary>
        public async Task LoadTicketAsync(string ticketId, CancellationToken ct)
        {
            if (Services == null)
            {
                return;
            }
            _currentTicketId = ticketId;
            try
            {
                var detail = await Services.Support.GetTicketAsync(ticketId, ct);
                DetailRendered?.Invoke(detail);
            }
            catch (CourierException ex)
            {
                ErrorRendered?.Invoke(ex.Error.WireCode);
            }
        }

        /// <summary>玩家追加消息,成功后重拉详情。CLOSED 工单 → SUPPORT_TICKET_CLOSED 出错误口。</summary>
        public async Task SendMessageAsync(string body, CancellationToken ct)
        {
            if (Services == null || string.IsNullOrEmpty(_currentTicketId))
            {
                return;
            }
            try
            {
                await Services.Support.AppendMessageAsync(_currentTicketId, body, ct);
                await LoadTicketAsync(_currentTicketId, ct);
            }
            catch (CourierException ex)
            {
                ErrorRendered?.Invoke(ex.Error.WireCode);
            }
        }

        /// <summary>FAQ 检索(关键词为空 = 热门问题);命中渲染到详情出口之外的问答文案。</summary>
        public async Task<string> SearchFaqAsync(string keyword, CancellationToken ct)
        {
            if (Services == null)
            {
                return "";
            }
            var page = await Services.Support.FaqAsync(keyword, 10, ct);
            if (page.Items == null || page.Items.Count == 0)
            {
                return "(无匹配问题)";
            }
            var sb = new System.Text.StringBuilder();
            foreach (var faq in page.Items)
            {
                sb.Append("Q: ").Append(faq.Question).Append('\n')
                  .Append("A: ").Append(faq.Answer).Append('\n');
            }
            return sb.ToString();
        }

        /// <summary>messages 通道回调入口:游戏侧解析到 support.ticket_replied 时调用,
        /// 面板自动重拉当前工单(拉取兜底语义的实时面)。</summary>
        public async void NotifyTicketReplied(string ticketId)
        {
            if (ticketId != _currentTicketId || Services == null)
            {
                return;
            }
            await LoadTicketAsync(ticketId, CancellationToken.None);
        }
    }
}
