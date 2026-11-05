// L3 文件 TokenStore 单测(cocos native.test.ts 同构):save/load 往返、
// clear 删除、缺失返回 nullopt、损坏 JSON 清场、读写异常降级、与核心解码往返。
// 运行:
//   cd sdks/ue/core
//   g++ -std=c++17 -Wall -Wextra -I. -I../contract -o /tmp/courier_ue_token_store_test token_store_test.cpp && /tmp/courier_ue_token_store_test
#include <cstdio>
#include <map>
#include <string>

#include "file_token_store.hpp"

static int gFailed = 0;

#define CHECK(cond, msg)                                     \
    do {                                                     \
        if (!(cond)) {                                       \
            std::printf("FAIL: %s\n", msg);                  \
            gFailed++;                                       \
        }                                                    \
    } while (0)

using namespace courier;

// 内存文件系统 fake(注入;模拟损坏/不可读场景)。
class MemoryFs : public FileSystem {
public:
    std::map<std::string, std::string> files;
    bool read_fail = false;
    bool write_fail = false;
    bool remove_fail = false;

    bool read_file(const std::string& path, std::string& out) override {
        if (read_fail) return false;
        auto it = files.find(path);
        if (it == files.end()) return false;
        out = it->second;
        return true;
    }
    bool write_file(const std::string& path, const std::string& contents) override {
        if (write_fail) return false;
        files[path] = contents;
        return true;
    }
    bool remove_file(const std::string& path) override {
        if (remove_fail) return false;
        files.erase(path);
        return true;
    }
};

static SessionDto make_session() {
    SessionDto s;
    AccountDto acc;
    acc.id = "acc_1";
    acc.type = "GUEST";
    acc.status = "ACTIVE";
    acc.created_at = "t";
    s.account = acc;
    s.access_token = "access-1";
    s.access_expires_at = "t";
    s.refresh_token = "refresh-1";
    s.refresh_expires_at = "t";
    s.device_id = "device-1";
    return s;
}

int main() {
    const std::string path = "Saved/courier/session.json";

    // ---- save/load 往返;clear 移除;缺失返回 nullopt ----
    {
        MemoryFs fs;
        FileTokenStore store(&fs, path);

        CHECK(!store.load().has_value(), "empty store → nullopt");
        const SessionDto session = make_session();
        store.save(session);
        CHECK(fs.files.count(path) == 1, "save writes the file");
        CHECK(fs.files[path].find("\"accessToken\":\"access-1\"") != std::string::npos,
              "serialized session carries accessToken");
        const std::optional<SessionDto> loaded = store.load();
        CHECK(loaded.has_value(), "load after save returns session");
        CHECK(loaded->access_token == "access-1", "round-trip access_token");
        CHECK(loaded->refresh_token == "refresh-1", "round-trip refresh_token");
        CHECK(loaded->device_id == "device-1", "round-trip device_id");
        CHECK(loaded->account.has_value() && loaded->account->id == "acc_1", "round-trip account");

        store.clear();
        CHECK(fs.files.count(path) == 0, "clear removes the file");
        CHECK(!store.load().has_value(), "load after clear → nullopt");
    }

    // ---- 损坏 JSON:清场返回 nullopt(不反噬) ----
    {
        MemoryFs fs;
        fs.files[path] = "{not-json";
        FileTokenStore store(&fs, path);
        CHECK(!store.load().has_value(), "corrupt JSON → nullopt");
        CHECK(fs.files.count(path) == 0, "corrupt JSON is cleared");
    }

    // ---- 合法 JSON 但非会话对象(字符串/数字/数组/`{}`):按未认证清场 ----
    {
        const char* bad_shapes[] = {"\"access-1\"", "42", "[]", "{}"};
        for (const char* shape : bad_shapes) {
            MemoryFs fs;
            fs.files[path] = shape;
            FileTokenStore store(&fs, path);
            CHECK(!store.load().has_value(), "non-session shape → nullopt");
            CHECK(fs.files.count(path) == 0, "non-session shape is cleared");
        }
    }

    // ---- 读写异常:安全降级(不抛)+ 失败经错误回调可观测 ----
    {
        MemoryFs fs;
        fs.files[path] = make_session().access_token;  // 内容非对象(类型不符)
        fs.read_fail = true;
        FileTokenStore store(&fs, path);
        CHECK(!store.load().has_value(), "read failure → nullopt");

        MemoryFs fs2;
        fs2.write_fail = true;
        int write_errors = 0;
        FileTokenStore store2(&fs2, path, [&write_errors](const std::string&) { write_errors++; });
        store2.save(make_session());  // 写入失败:静默(不抛)
        CHECK(fs2.files.empty(), "write failure leaves no file");
        CHECK(write_errors == 1, "write failure reported via error callback");

        int clear_errors = 0;
        FileTokenStore store3(&fs2, path, [&clear_errors](const std::string&) { clear_errors++; });
        store3.clear();  // clear 失败:remove_file 的 erase 不失败,此处覆盖成功分支
        CHECK(clear_errors == 0, "clear success reports no error");

        int clear_failure_errors = 0;
        FileTokenStore store3_fail(&fs2, path,
                                   [&clear_failure_errors](const std::string&) { clear_failure_errors++; });
        fs2.remove_fail = true;
        store3_fail.clear();  // clear 失败:静默(不抛),错误回调可观测
        CHECK(clear_failure_errors == 1, "clear failure reported via error callback");
        fs2.remove_fail = false;

        MemoryFs fs4;
        fs4.files[path] = "{}";
        int load_clear_errors = 0;
        FileTokenStore store4(&fs4, path,
                              [&load_clear_errors](const std::string&) { load_clear_errors++; });
        CHECK(!store4.load().has_value(), "empty-object shape → nullopt");
        CHECK(load_clear_errors == 0, "non-session clear success reports no error");
    }

    // ---- 空文件:按未认证 ----
    {
        MemoryFs fs;
        fs.files[path] = "";
        FileTokenStore store(&fs, path);
        CHECK(!store.load().has_value(), "empty file → nullopt");
    }

    if (gFailed > 0) {
        std::printf("ue token store tests: %d failed\n", gFailed);
        return 1;
    }
    std::printf("ue token store tests: all green\n");
    return 0;
}
