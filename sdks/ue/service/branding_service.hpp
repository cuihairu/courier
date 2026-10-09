// L2 Service:品牌客户端(契约 branding.md Frozen v1)。SDK 透传原始 JSON——零解释、
// 零渲染、零缓存策略(缓存归宿主/接入方);兜底规则与消费全在接入方 UI。
// 501 能力未接 → null(UI 全默认)。匿名可:登录页就要显示品牌。
#ifndef COURIER_SERVICE_BRANDING_SERVICE_HPP
#define COURIER_SERVICE_BRANDING_SERVICE_HPP

#include <optional>
#include <string>

#include "../core/api_client.hpp"
#include "../core/courier_error.hpp"
#include "app_dto.hpp"
#include "service_dto.hpp"

namespace courier {

class BrandingService {
public:
    explicit BrandingService(ApiClient* api) : api_(api) {}

    /// 最近一次成功拉取;nullptr = 从未拉到。
    const BrandingDto* cached_branding() const {
        return has_cached_ ? &cached_ : nullptr;
    }

    /// 拉取品牌物料(version + 业务字段透传)。501 → null(未启用态,UI 全默认)。
    ServiceOutcome<std::optional<BrandingDto>> fetch() {
        RequestOutcome out = api_->request("GET", "/v1/app/branding", nullptr, false);
        ServiceOutcome<std::optional<BrandingDto>> result;
        if (out.ok) {
            result.ok = true;
            result.data = decode_branding(out.data);
            cached_ = *result.data;
            has_cached_ = true;
        } else if (is_capability_disabled(out.error)) {
            result.ok = true;
            result.data = std::nullopt;
        } else {
            result.error = out.error;
        }
        return result;
    }

    /// branding.updated 事件到达后的重拉判据:事件 version 与缓存不同才需要重拉
    /// (相同即忽略,不重渲染);从未拉到过 → 一律重拉。
    bool needs_refetch(std::int64_t event_version) const {
        return !has_cached_ || cached_.version != event_version;
    }

private:
    ApiClient* api_;
    BrandingDto cached_;
    bool has_cached_ = false;
};

}  // namespace courier

#endif  // COURIER_SERVICE_BRANDING_SERVICE_HPP
