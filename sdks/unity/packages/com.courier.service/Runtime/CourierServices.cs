// 服务包门面:从 CourierClient 取 L2 通道,装配域服务(公告/客服/实名/应用状态)。
// 依赖方向 service→core(asmdef/package.json 双约束);core 不感知本包。
// 推送通道帧解析见 SseParser;SSE 流式传输本身归 L3 Adapter(引擎卡点)。
using Courier;
using Courier.Core;

namespace Courier.Service
{
    public sealed class CourierServices
    {
        public AnnouncementService Announcements { get; }
        public SupportService Support { get; }
        public RealNameService RealName { get; }
        public AppService App { get; }
        public AppConfigService Config { get; }
        public BrandingService Branding { get; }

        public CourierServices(CourierClient client)
        {
            var api = client.Api;
            Announcements = new AnnouncementService(api);
            Support = new SupportService(api);
            RealName = new RealNameService(api);
            App = new AppService(api);
            Config = new AppConfigService(api);
            Branding = new BrandingService(api);
        }
    }
}
