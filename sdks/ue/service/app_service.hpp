// L2 Service:应用状态客户端(契约 app.md Frozen v1)。三端点匿名可(维护中仍需可查);
// 501 能力未接 → null(跳过版本/维护检查直接登录,能力安静地不存在)。
// 503 APP_MAINTENANCE / 426 APP_VERSION_UNSUPPORTED 照常 typed 错误(信封已登记),
// 调用方据此走维护页/更新引导;SDK 不自动跳转商店。
#ifndef COURIER_SERVICE_APP_SERVICE_HPP
#define COURIER_SERVICE_APP_SERVICE_HPP

#include <optional>
#include <string>

#include "../core/api_client.hpp"
#include "../core/courier_error.hpp"
#include "../core/types.hpp"
#include "app_dto.hpp"
#include "service_dto.hpp"

namespace courier {

class AppService {
public:
    explicit AppService(ApiClient* api) : api_(api) {}

    /// 版本检查:appVersion 上报参与 forceUpdate 判定(缺省 = 不判定);platform 可选。
    ServiceOutcome<std::optional<AppVersionDto>> check_update(const std::string& app_version = "",
                                                             const std::string& platform = "") {
        std::string path = "/v1/app/version";
        std::string query;
        if (!app_version.empty()) {
            query += (query.empty() ? "?" : "&") + std::string("appVersion=") + url_encode(app_version);
        }
        if (!platform.empty()) {
            query += (query.empty() ? "?" : "&") + std::string("platform=") + url_encode(platform);
        }
        RequestOutcome out = api_->request("GET", path + query, nullptr, false);
        ServiceOutcome<std::optional<AppVersionDto>> result;
        if (out.ok) {
            result.ok = true;
            result.data = decode_app_version(out.data);
        } else if (is_capability_disabled(out.error)) {
            result.ok = true;
            result.data = std::nullopt;
        } else {
            result.error = out.error;
        }
        return result;
    }

    /// 维护状态查询:维护页数据源;estimatedRecoveryAt 供轮询节奏。
    ServiceOutcome<std::optional<AppMaintenanceDto>> check_maintenance() {
        RequestOutcome out = api_->request("GET", "/v1/app/maintenance", nullptr, false);
        ServiceOutcome<std::optional<AppMaintenanceDto>> result;
        if (out.ok) {
            result.ok = true;
            result.data = decode_app_maintenance(out.data);
        } else if (is_capability_disabled(out.error)) {
            result.ok = true;
            result.data = std::nullopt;
        } else {
            result.error = out.error;
        }
        return result;
    }

    /// 环境回显:初始化后校验 scope 与网关一致(排查接错环境)。
    ServiceOutcome<std::optional<AppEnvironmentDto>> get_environment() {
        RequestOutcome out = api_->request("GET", "/v1/app/environment", nullptr, false);
        ServiceOutcome<std::optional<AppEnvironmentDto>> result;
        if (out.ok) {
            result.ok = true;
            result.data = decode_app_environment(out.data);
        } else if (is_capability_disabled(out.error)) {
            result.ok = true;
            result.data = std::nullopt;
        } else {
            result.error = out.error;
        }
        return result;
    }

private:
    ApiClient* api_;
};

}  // namespace courier

#endif  // COURIER_SERVICE_APP_SERVICE_HPP
