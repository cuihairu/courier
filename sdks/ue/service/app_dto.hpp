// L2 Service App/Config/Branding 域 DTO(契约 app.md / config.md / branding.md Frozen v1)。
// items/fields 为任意 JSON 值原样透传(SDK 不做二次校验——接入方结构变更是使用方错误)。
#ifndef COURIER_SERVICE_APP_DTO_HPP
#define COURIER_SERVICE_APP_DTO_HPP

#include <optional>
#include <string>

#include "../core/json.hpp"

namespace courier {

/// 远程配置快照:configVersion(热生效判据)+ 命中条件的键值全量。
struct AppConfigDto {
    std::int64_t configVersion = 0;
    json::Value items = json::Value::object();
};

/// 品牌物料:version(热生效判据)+ 业务字段透传(SDK 零解释;消费全在接入方 UI)。
struct BrandingDto {
    std::int64_t version = 0;
    json::Value fields = json::Value::object();
};

/// 版本检查:forceUpdate = appVersion < minVersion(服务端判定)。
struct AppVersionDto {
    std::string latestVersion;
    std::string minVersion;
    std::optional<std::string> updateUrl;  // 可选(payload 缺省即缺键,契约 app.md)
    bool forceUpdate = false;
};

/// 维护状态:estimatedRecoveryAt/message 可选(缺省即缺键)。
struct AppMaintenanceDto {
    bool inMaintenance = false;
    std::optional<std::string> estimatedRecoveryAt;
    std::optional<std::string> message;
};

/// 环境回显:初始化校验(排查接错环境)。
struct AppEnvironmentDto {
    std::string gameId;
    std::string env;
};

// ---- 解码(wire JSON → DTO;字段缺失安全) ----

inline AppConfigDto decode_app_config(const json::Value& v) {
    AppConfigDto d;
    d.configVersion = json::get_i64(v, "configVersion", 0);
    if (const json::Value* items = v.find("items"); items && items->is_object()) {
        d.items = *items;
    }
    return d;
}

inline BrandingDto decode_branding(const json::Value& v) {
    BrandingDto d;
    d.version = json::get_i64(v, "version", 0);
    d.fields = v;
    return d;
}

inline AppVersionDto decode_app_version(const json::Value& v) {
    AppVersionDto d;
    d.latestVersion = json::get_str(v, "latestVersion");
    d.minVersion = json::get_str(v, "minVersion");
    std::string s;
    if (json::get_opt_str(v, "updateUrl", s)) d.updateUrl = s;
    d.forceUpdate = json::get_bool(v, "forceUpdate", false);
    return d;
}

inline AppMaintenanceDto decode_app_maintenance(const json::Value& v) {
    AppMaintenanceDto d;
    d.inMaintenance = json::get_bool(v, "inMaintenance", false);
    std::string s;
    if (json::get_opt_str(v, "estimatedRecoveryAt", s)) d.estimatedRecoveryAt = s;
    if (json::get_opt_str(v, "message", s)) d.message = s;
    return d;
}

inline AppEnvironmentDto decode_app_environment(const json::Value& v) {
    AppEnvironmentDto d;
    d.gameId = json::get_str(v, "gameId");
    d.env = json::get_str(v, "env");
    return d;
}

}  // namespace courier

#endif  // COURIER_SERVICE_APP_DTO_HPP
