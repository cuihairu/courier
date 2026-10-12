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
        readonly string _defaultLanguage;

        public AnnouncementService(ApiClient api, string defaultLanguage = null)
        {
            _api = api;
            _defaultLanguage = defaultLanguage;
        }

        /// <summary>可见公告列表(分页;limit 默认 20 最大 100,游标回传 nextCursor)。</summary>
        public Task<PageDto<AnnouncementDto>> ListAsync(int limit, string cursor,
            CancellationToken ct)
        {
            return ListAsync(limit, cursor, null, ct);
        }

        /// <summary>可见公告列表(v1.1 定向):query 缺省维度不过滤;详情被过滤出局
        /// → typed ANNOUNCEMENT_NOT_FOUND。</summary>
        public async Task<PageDto<AnnouncementDto>> ListAsync(int limit, string cursor,
            AnnouncementQuery query, CancellationToken ct)
        {
            var path = "?limit=" + limit;
            if (!string.IsNullOrEmpty(cursor))
            {
                path += "&cursor=" + cursor;
            }
            if (query != null)
            {
                path += query.ToQueryString();
            }
            // F76 多语内容:配置 locale 自动附带 language(仅列表);显式 query.Language 覆写,
            // 配置缺省不附带(与 v1 行为一致)。行内 language 元数据不参与匹配。
            if (string.IsNullOrEmpty(query?.Language) && !string.IsNullOrEmpty(_defaultLanguage))
            {
                path += "&language=" + _defaultLanguage;
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
