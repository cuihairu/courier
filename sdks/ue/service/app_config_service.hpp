// L2 Service:远程配置客户端(契约 config.md Frozen v1)。
// 客户端要求:启动/进前台拉取;config.updated 事件到达后经 needs_refetch 判重拉
// (相同 version 即忽略);重拉失败沿用缓存(配置是加速器,不是依赖)。
#ifndef COURIER_SERVICE_APP_CONFIG_SERVICE_HPP
#define COURIER_SERVICE_APP_CONFIG_SERVICE_HPP

#include <string>

#include "../core/api_client.hpp"
#include "../core/courier_error.hpp"
#include "../core/types.hpp"
#include "app_dto.hpp"
#include "service_dto.hpp"

namespace courier {

class AppConfigService {
public:
    explicit AppConfigService(ApiClient* api) : api_(api) {}

    /// 最近一次成功拉取(缓存);nullptr = 从未拉到。
    const AppConfigDto* cached_config() const {
        return has_cached_ ? &cached_ : nullptr;
    }

    /// 拉取命中当前条件的键值全量(platform/appVersion/region 可选;缺省维度不参与过滤)。
    /// 501 能力未接 → 空结果(v0 + 空 items,接入方使用内建默认值,不进报错路径)。
    ServiceOutcome<AppConfigDto> fetch(const std::string& platform = "",
                                       const std::string& app_version = "",
                                       const std::string& region = "") {
        std::string path = "/v1/app/config";
        std::string query;
        if (!platform.empty()) {
            query += (query.empty() ? "?" : "&") + std::string("platform=") + url_encode(platform);
        }
        if (!app_version.empty()) {
            query += (query.empty() ? "?" : "&") + std::string("appVersion=") + url_encode(app_version);
        }
        if (!region.empty()) {
            query += (query.empty() ? "?" : "&") + std::string("region=") + url_encode(region);
        }
        RequestOutcome out = api_->request("GET", path + query, nullptr, true);
        ServiceOutcome<AppConfigDto> result;
        if (out.ok) {
            result.ok = true;
            result.data = decode_app_config(out.data);
            cached_ = result.data;
            has_cached_ = true;
        } else if (is_capability_disabled(out.error)) {
            result.ok = true;
            result.data = AppConfigDto{};  // v0 + 空 items
        } else {
            result.error = out.error;
        }
        return result;
    }

    /// config.updated 事件到达后的重拉判据:事件 version 与缓存不同才需要重拉
    /// (相同即忽略,不重渲染);从未拉到过 → 一律重拉。
    bool needs_refetch(std::int64_t event_config_version) const {
        return !has_cached_ || cached_.configVersion != event_config_version;
    }

    /// 取值;键缺失返回 nullptr(值原样,类型由接入方收窄)。
    const json::Value* get(const std::string& key) const {
        if (has_cached_) {
            return cached_.items.find(key);
        }
        return nullptr;
    }

private:
    ApiClient* api_;
    AppConfigDto cached_;
    bool has_cached_ = false;
};

}  // namespace courier

#endif  // COURIER_SERVICE_APP_CONFIG_SERVICE_HPP
