// L2 Core:token 存储接口(cocos tokenStore.ts / unity ITokenStore 同构)。
// 平台持久化实现(UE 侧平台安全存储)由 L3 适配器提供;MemoryTokenStore 供
// 测试与无持久化场景。冷启动恢复 = load 有值即已认证。
#ifndef COURIER_CORE_TOKEN_STORE_HPP
#define COURIER_CORE_TOKEN_STORE_HPP

#include <optional>

#include "types.hpp"

namespace courier {

struct TokenStore {
    virtual ~TokenStore() = default;
    virtual std::optional<SessionDto> load() = 0;
    virtual void save(const SessionDto& session) = 0;
    virtual void clear() = 0;
};

/// 内存实现(测试/无持久化场景;进程退出即失)。
class MemoryTokenStore : public TokenStore {
public:
    std::optional<SessionDto> load() override { return session_; }
    void save(const SessionDto& session) override { session_ = session; }
    void clear() override { session_.reset(); }

private:
    std::optional<SessionDto> session_;
};

}  // namespace courier

#endif  // COURIER_CORE_TOKEN_STORE_HPP
