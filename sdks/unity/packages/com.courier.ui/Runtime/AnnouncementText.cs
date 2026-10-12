// F36 跑马灯文本合成(纯 C#,无 UnityEngine——dotnet 可测,同 BrandingCatalog 口径)。
// 零新契约:severity=CRITICAL 公告逐条 "title: body",以 "  |  " 连接为单行滚动文本;
// 无 CRITICAL 返回 null(调用方不闪空跑马灯)。
using System.Collections.Generic;
using Courier.Service;

namespace Courier.UI
{
    public static class AnnouncementText
    {
        /// <summary>跑马灯文本:仅 CRITICAL 公告;无命中返回 null。</summary>
        public static string FormatMarquee(IReadOnlyList<AnnouncementDto> items)
        {
            if (items == null || items.Count == 0)
            {
                return null;
            }
            string joined = null;
            foreach (var item in items)
            {
                if (item == null || item.Severity != "CRITICAL")
                {
                    continue;
                }
                var row = item.Title + ": " + item.Body;
                joined = joined == null ? row : joined + "  |  " + row;
            }
            return joined;
        }
    }
}
