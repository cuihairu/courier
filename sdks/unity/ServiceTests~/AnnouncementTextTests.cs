// F36 跑马灯文本单测:仅 CRITICAL 进跑马灯;多条 "  |  " 连接;空/无命中返回 null。
using System.Collections.Generic;
using Courier.Service;
using Courier.UI;
using Xunit;

namespace Courier.ServiceTests
{
    public class AnnouncementTextTests
    {
        static AnnouncementDto Item(string title, string body, string severity)
        {
            return new AnnouncementDto { Id = "ann_" + title, Title = title, Body = body, Severity = severity };
        }

        [Fact]
        public void FormatMarquee_CriticalOnly()
        {
            var items = new List<AnnouncementDto>
            {
                Item("维护公告", "今晚维护", "INFO"),
                Item("限时活动", "双倍经验开启", "CRITICAL"),
            };

            Assert.Equal("限时活动: 双倍经验开启", AnnouncementText.FormatMarquee(items));
        }

        [Fact]
        public void FormatMarquee_JoinsMultipleRows()
        {
            var items = new List<AnnouncementDto>
            {
                Item("紧急维护", "02:00 停服", "CRITICAL"),
                Item("补偿发放", "维护后 48h", "CRITICAL"),
            };

            Assert.Equal("紧急维护: 02:00 停服  |  补偿发放: 维护后 48h",
                AnnouncementText.FormatMarquee(items));
        }

        [Fact]
        public void FormatMarquee_NoCritical_ReturnsNull()
        {
            var items = new List<AnnouncementDto> { Item("日常公告", "版本说明", "INFO") };

            Assert.Null(AnnouncementText.FormatMarquee(items));
            Assert.Null(AnnouncementText.FormatMarquee(new List<AnnouncementDto>()));
            Assert.Null(AnnouncementText.FormatMarquee(null));
        }
    }
}
