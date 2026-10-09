// 契约测试骨架:UE 端错误枚举/信封 DTO 与 fixture 对齐断言(独立编译,无引擎依赖)。
// 运行:
//   cd sdks/ue/contract
//   g++ -std=c++17 -Wall -Wextra -o /tmp/courier_ue_contract_test contract_test.cpp && /tmp/courier_ue_contract_test
// 注:本文件硬编码期望值 = fixture 快照;docs↔fixture↔生成物的一致性由 tools/contractgen
//     校验,此处是 UE 端的人工评审卡点——新增冻结码时更新 kExpectedCount 与抽查值。
#include "errors.hpp"
#include "envelope.hpp"

#include <cstdio>
#include <cstring>
#include <string>

static int gFailed = 0;

#define CHECK(cond, msg)                                     \
    do {                                                     \
        if (!(cond)) {                                       \
            std::printf("FAIL: %s\n", msg);                  \
            gFailed++;                                       \
        }                                                    \
    } while (0)

int main() {
    using namespace courier;

    // 冻结码数量(docs/contract/fixtures/errors.json v4 = 33)。
    CHECK(static_cast<int>(ErrorCode::kCount) == 33, "kCount == 33");

    // 抽查冻结映射(wire / HTTP / retryable)。
    CHECK(std::strcmp(error_spec(ErrorCode::CommonInternal).wire, "COMMON_INTERNAL") == 0,
          "COMMON_INTERNAL wire");
    CHECK(error_spec(ErrorCode::CommonInternal).http == 500 &&
              error_spec(ErrorCode::CommonInternal).retryable,
          "COMMON_INTERNAL 500 retryable");
    CHECK(error_spec(ErrorCode::RateLimited).http == 429 &&
              error_spec(ErrorCode::RateLimited).retryable,
          "RATE_LIMITED 429 retryable");
    CHECK(error_spec(ErrorCode::CommonCapabilityDisabled).http == 501 &&
              !error_spec(ErrorCode::CommonCapabilityDisabled).retryable,
          "COMMON_CAPABILITY_DISABLED 501");
    CHECK(error_spec(ErrorCode::AuthEmailTaken).http == 409,
          "AUTH_EMAIL_TAKEN 409");
    CHECK(error_spec(ErrorCode::AppVersionUnsupported).http == 426,
          "APP_VERSION_UNSUPPORTED 426");
    CHECK(error_spec(ErrorCode::RealnameCurfewBlocked).http == 403 &&
              !error_spec(ErrorCode::RealnameCurfewBlocked).retryable,
          "REALNAME_CURFEW_BLOCKED 403");
    CHECK(error_spec(ErrorCode::RealnameChargeBlocked).http == 403 &&
              !error_spec(ErrorCode::RealnameChargeBlocked).retryable,
          "REALNAME_CHARGE_BLOCKED 403");
    CHECK(error_spec(ErrorCode::CommonUnavailable).http == 503 &&
              error_spec(ErrorCode::CommonUnavailable).retryable,
          "COMMON_UNAVAILABLE 503 retryable");

    // 全码 parse 往返。
    for (int i = 0; i < static_cast<int>(ErrorCode::kCount); i++) {
        const ErrorCode code = static_cast<ErrorCode>(i);
        ErrorCode out;
        if (!parse_error_code(error_spec(code).wire, out) || out != code) {
            std::printf("FAIL: parse 往返 %s\n", error_spec(code).wire);
            gFailed++;
        }
    }
    ErrorCode junk;
    CHECK(!parse_error_code("NOT_A_CODE", junk), "未知 wire code 返回 false");

    // 域前缀注册表。
    std::size_t prefixCount = 0;
    const char* const* prefixes = error_prefixes(prefixCount);
    CHECK(prefixCount == 13, "域前缀 13 个");
    bool hasRate = false;
    for (std::size_t i = 0; i < prefixCount; i++) {
        if (std::strcmp(prefixes[i], "RATE") == 0) hasRate = true;
    }
    CHECK(hasRate, "域前缀含 RATE");

    // 信封 wire key 常量(primitives.md Frozen v1)。
    CHECK(std::strcmp(ApiError::kKeyCode, "code") == 0, "key: code");
    CHECK(std::strcmp(ApiError::kKeyMessage, "message") == 0, "key: message");
    CHECK(std::strcmp(ApiError::kKeyRetryable, "retryable") == 0, "key: retryable");
    CHECK(std::strcmp(ErrorEnvelope::kKeyError, "error") == 0, "key: error");
    CHECK(std::strcmp(ErrorEnvelope::kKeyTraceId, "traceId") == 0, "key: traceId");
    CHECK(std::strcmp(SuccessEnvelope<int>::kKeyData, "data") == 0, "key: data");
    CHECK(std::strcmp(Page<int>::kKeyItems, "items") == 0, "key: items");
    CHECK(std::strcmp(Page<int>::kKeyNextCursor, "nextCursor") == 0, "key: nextCursor");

    // DTO 结构往返(手工赋值)。
    ErrorEnvelope env;
    env.error.code = "AUTH_TOKEN_EXPIRED";
    env.error.message = "session expired";
    env.error.retryable = false;
    env.traceId = std::string(32, 'a');
    CHECK(env.error.code == "AUTH_TOKEN_EXPIRED" && !env.error.retryable &&
              env.traceId.size() == 32,
          "失败信封往返");

    Page<int> page;
    page.items = {1, 2};
    page.nextCursor = "";  // 空串 = 末页
    CHECK(page.items.size() == 2 && page.nextCursor.empty(), "分页往返");

    if (gFailed > 0) {
        std::printf("ue contract tests: %d failed\n", gFailed);
        return 1;
    }
    std::printf("ue contract tests: all green\n");
    return 0;
}
