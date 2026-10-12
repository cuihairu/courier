// L2 Service:公告域客户端(契约 announcement.md:玩家侧只读投影)。
// 列表/详情经 ApiClient(scope 头、Bearer、信封、重试全部归 L2)。
#ifndef COURIER_SERVICE_ANNOUNCEMENT_SERVICE_HPP
#define COURIER_SERVICE_ANNOUNCEMENT_SERVICE_HPP

#include "../core/api_client.hpp"
#include "../core/types.hpp"
#include "service_dto.hpp"

namespace courier {

class AnnouncementService {
public:
    explicit AnnouncementService(ApiClient* api) : api_(api) {}

    /// 可见公告列表(分页;limit 默认 20 最大 100,游标回传 nextCursor)。
    ServiceOutcome<Page<AnnouncementDto>> list(int limit, const std::string& cursor = "") {
        return list(limit, cursor, AnnouncementQuery{});
    }

    /// 可见公告列表(v1.1 定向):query 缺省维度不过滤;详情被过滤出局
    /// → typed ANNOUNCEMENT_NOT_FOUND。
    ServiceOutcome<Page<AnnouncementDto>> list(int limit, const std::string& cursor,
                                               const AnnouncementQuery& query) {
        std::string path = "/v1/announcements?limit=" + std::to_string(limit);
        if (!cursor.empty()) {
            path += "&cursor=" + url_encode(cursor);
        }
        append_dim(path, "platform", query.platform);
        append_dim(path, "appVersion", query.appVersion);
        append_dim(path, "region", query.region);
        append_dim(path, "language", query.language);
        RequestOutcome out = api_->request("GET", path, nullptr, true);
        ServiceOutcome<Page<AnnouncementDto>> result;
        if (out.ok) {
            result.ok = true;
            result.data = decode_page(out.data, decode_announcement);
        } else {
            result.error = out.error;
        }
        return result;
    }

    /// 公告详情;不可见/不存在 → typed ANNOUNCEMENT_NOT_FOUND(404,不重试)。
    ServiceOutcome<AnnouncementDto> get(const std::string& id) {
        RequestOutcome out =
            api_->request("GET", "/v1/announcements/" + url_encode(id), nullptr, true);
        ServiceOutcome<AnnouncementDto> result;
        if (out.ok) {
            result.ok = true;
            result.data = decode_announcement(out.data);
        } else {
            result.error = out.error;
        }
        return result;
    }

private:
    static void append_dim(std::string& path, const char* key,
                           const std::optional<std::string>& value) {
        if (value.has_value() && !value->empty()) {
            path += std::string("&") + key + "=" + url_encode(*value);
        }
    }

    ApiClient* api_;
};

}  // namespace courier

#endif  // COURIER_SERVICE_ANNOUNCEMENT_SERVICE_HPP
