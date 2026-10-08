// 批次 6 UI 包首发:公告栏面板骨架(todo 批次 6「公告栏」)。
// 边界:包内只做编排与纯文本产出,控件绑定经 ListRendered 事件交游戏侧,
// 不绑死特定 UI 框架(UGUI/TMP/FairyGUI 均可挂)。
// 品牌定制:标题消费 Branding 契约——M3 冻结前用默认标 BrandTitle,
// Config 通道通后由宿主注入覆盖(参考调研 XDSDK UI 剥离做法)。
using System;
using System.Collections.Generic;
using System.Threading;
using System.Threading.Tasks;
using Courier.Service;
using UnityEngine;

namespace Courier.UI
{
    public sealed class AnnouncementPanel : MonoBehaviour
    {
        /// <summary>Branding 默认标(M3 Config 通道接通后可被远端覆盖)。</summary>
        public const string DefaultTitle = "公告";

        [Tooltip("宿主装配:与登录后的 CourierClient 同生命周期")]
        public CourierServices Services;
        public string BrandTitle = DefaultTitle;
        [Tooltip("首页拉取条数(契约默认 20,最大 100)")]
        public int PageSize = 20;

        /// <summary>渲染出口:游戏侧订阅后把文本投到目标控件。</summary>
        public event Action<string> ListRendered;

        /// <summary>打点出口:点开详情时回调公告 id(埋点归游戏)。</summary>
        public event Action<string> AnnouncementOpened;

        CancellationTokenSource _cts;

        void OnEnable()
        {
            _cts = new CancellationTokenSource();
            _ = RefreshAsync(_cts.Token);
        }

        void OnDisable()
        {
            _cts?.Cancel();
            _cts?.Dispose();
            _cts = null;
        }

        /// <summary>拉取可见公告并渲染(下拉刷新也走这里)。</summary>
        public async Task RefreshAsync(CancellationToken ct)
        {
            if (Services == null)
            {
                return; // 未装配:面板静默,不抛——UI 骨架允许先挂场景后接服务
            }
            var page = await Services.Announcements.ListAsync(PageSize, null, ct);
            ListRendered?.Invoke(Format(page.Items));
        }

        /// <summary>查看详情:渲染单条全文并回调 id。</summary>
        public async Task OpenAsync(string announcementId, CancellationToken ct)
        {
            if (Services == null)
            {
                return;
            }
            var dto = await Services.Announcements.GetAsync(announcementId, ct);
            AnnouncementOpened?.Invoke(dto.Id);
            ListRendered?.Invoke(BrandTitle + "\n" + Format(new List<AnnouncementDto> { dto }));
        }

        string Format(List<AnnouncementDto> items)
        {
            if (items == null || items.Count == 0)
            {
                return BrandTitle + "\n(暂无公告)";
            }
            var sb = new System.Text.StringBuilder();
            sb.Append(BrandTitle);
            foreach (var item in items)
            {
                sb.Append('\n').Append('[').Append(item.Severity).Append("] ")
                  .Append(item.Title).Append('\n').Append(item.Body);
            }
            return sb.ToString();
        }
    }
}
