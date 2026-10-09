// L2 Service:玩家档案客户端(契约 player.md Frozen v1)。四端点全 Bearer;
// 501 能力未接 → null(调用方隐藏档案 UI,能力安静地不存在)。
// 红线同契约:SDK 不采集邮箱/手机/设备/行为字段;角色数据归各游戏。
#ifndef COURIER_SERVICE_PLAYER_SERVICE_HPP
#define COURIER_SERVICE_PLAYER_SERVICE_HPP

#include <cstdint>
#include <optional>
#include <string>
#include <vector>

#include "../core/api_client.hpp"
#include "../core/courier_error.hpp"
#include "../core/types.hpp"
#include "../contract/envelope.hpp"
#include "service_dto.hpp"

namespace courier {

/// 账号档案(懒建:首次即默认档案,展示名 Player)。
struct PlayerProfileDto {
    std::string displayName;
    std::optional<std::string> avatarUrl;
    std::string createdAt;
    std::string updatedAt;
};

/// 绑定角色(boundAt 升序;按 scope 隔离)。
struct PlayerCharacterDto {
    std::string playerId;
    std::string boundAt;
};

inline PlayerProfileDto decode_player_profile(const json::Value& v) {
    PlayerProfileDto d;
    d.displayName = json::get_str(v, "displayName");
    std::string s;
    if (json::get_opt_str(v, "avatarUrl", s)) d.avatarUrl = s;
    d.createdAt = json::get_str(v, "createdAt");
    d.updatedAt = json::get_str(v, "updatedAt");
    return d;
}

inline PlayerCharacterDto decode_player_character(const json::Value& v) {
    PlayerCharacterDto d;
    d.playerId = json::get_str(v, "playerId");
    d.boundAt = json::get_str(v, "boundAt");
    return d;
}

class PlayerService {
public:
    explicit PlayerService(ApiClient* api) : api_(api) {}

    /// 拉取账号档案(懒建)。
    ServiceOutcome<std::optional<PlayerProfileDto>> get_profile() {
        RequestOutcome out = api_->request("GET", "/v1/player/profile", nullptr, true);
        ServiceOutcome<std::optional<PlayerProfileDto>> result;
        if (out.ok) {
            result.ok = true;
            result.data = decode_player_profile(out.data);
        } else if (is_capability_disabled(out.error)) {
            result.ok = true;
            result.data = std::nullopt;
        } else {
            result.error = out.error;
        }
        return result;
    }

    /// 修改档案:displayName/avatarUrl 传空串表示不改该字段(空串不下发);
    /// 响应体为准(服务端修剪后的值),调用方以返回值刷新 UI。
    ServiceOutcome<std::optional<PlayerProfileDto>> update_profile(
        const std::string& display_name, const std::string& avatar_url) {
        json::Value body = json::Value::object();
        if (!display_name.empty()) body.set("displayName", json::Value::str(display_name));
        if (!avatar_url.empty()) body.set("avatarUrl", json::Value::str(avatar_url));
        RequestOutcome out = api_->request("PATCH", "/v1/player/profile", &body, true);
        ServiceOutcome<std::optional<PlayerProfileDto>> result;
        if (out.ok) {
            result.ok = true;
            result.data = decode_player_profile(out.data);
        } else if (is_capability_disabled(out.error)) {
            result.ok = true;
            result.data = std::nullopt;
        } else {
            result.error = out.error;
        }
        return result;
    }

    /// 本游戏已绑定角色映射(boundAt 升序;按 scope 隔离,服务端语义)。
    ServiceOutcome<std::optional<Page<PlayerCharacterDto>>> list_characters() {
        RequestOutcome out = api_->request("GET", "/v1/player/characters", nullptr, true);
        ServiceOutcome<std::optional<Page<PlayerCharacterDto>>> result;
        if (out.ok) {
            result.ok = true;
            result.data = decode_page(out.data, decode_player_character);
        } else if (is_capability_disabled(out.error)) {
            result.ok = true;
            result.data = std::nullopt;
        } else {
            result.error = out.error;
        }
        return result;
    }

    /// 绑定角色:幂等(重复绑定返回既有记录);上限 50/账号/游戏。
    ServiceOutcome<std::optional<PlayerCharacterDto>> bind_character(const std::string& player_id) {
        json::Value body = json::Value::object();
        body.set("playerId", json::Value::str(player_id));
        RequestOutcome out = api_->request("POST", "/v1/player/characters", &body, true);
        ServiceOutcome<std::optional<PlayerCharacterDto>> result;
        if (out.ok) {
            result.ok = true;
            result.data = decode_player_character(out.data);
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

#endif  // COURIER_SERVICE_PLAYER_SERVICE_HPP
