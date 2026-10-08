// M2 公告域客户端(契约 announcement.md:玩家侧只读投影)。
// 列表/详情经 ApiClient(scope 头、Bearer、信封、重试全部归 L2)。
using System.Threading;
using System.Threading.Tasks;
using Courier.Core;

namespace Courier.Service
{
    public sealed class AnnouncementService
    {
        const string Prefix = "/v1/announcements";
        readonly ApiClient _api;

        public AnnouncementService(ApiClient api)
        {
            _api = api;
        }

        /// <summary>可见公告列表(分页;limit 默认 20 最大 100,游标回传 nextCursor)。</summary>
        public async Task<PageDto<AnnouncementDto>> ListAsync(int limit, string cursor,
            CancellationToken ct)
        {
            var path = "?limit=" + limit;
            if (!string.IsNullOrEmpty(cursor))
            {
                path += "&cursor=" + cursor;
            }
            var json = await _api.SendAsync("GET", Prefix + path, null, true, ct)
                .ConfigureAwait(false);
            return Json.Deserialize<PageDto<AnnouncementDto>>(json);
        }

        /// <summary>公告详情;不可见/不存在 → typed ANNOUNCEMENT_NOT_FOUND(404,不重试)。</summary>
        public async Task<AnnouncementDto> GetAsync(string announcementId, CancellationToken ct)
        {
            var json = await _api.SendAsync("GET", Prefix + "/" + announcementId, null, true, ct)
                .ConfigureAwait(false);
            return Json.Deserialize<AnnouncementDto>(json);
        }
    }
}
