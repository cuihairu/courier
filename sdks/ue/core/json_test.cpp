// json.hpp 单测:解析(wire 形状/转义/代理对/数字/边界拒绝)+ 序列化往返 + DTO 取值辅助。
// 运行:
//   cd sdks/ue/core
//   g++ -std=c++17 -Wall -Wextra -o /tmp/courier_ue_json_test json_test.cpp && /tmp/courier_ue_json_test
#include "json.hpp"

#include <cstdio>
#include <string>

static int gFailed = 0;

#define CHECK(cond, msg)                                     \
    do {                                                     \
        if (!(cond)) {                                       \
            std::printf("FAIL: %s\n", msg);                  \
            gFailed++;                                       \
        }                                                    \
    } while (0)

using courier::json::Value;

static bool ok_parse(const char* text, Value& out) {
    std::string err;
    const bool ok = courier::json::parse(text, out, err);
    if (!ok) std::printf("  (parse err: %s)\n", err.c_str());
    return ok;
}

static bool bad_parse(const char* text) {
    Value out;
    std::string err;
    return !courier::json::parse(text, out, err) && !err.empty();
}

int main() {
    // ---- wire 形状(信封) ----
    Value v;
    CHECK(ok_parse("{\"data\":{\"accessToken\":\"a\",\"amountCents\":600}}", v), "parse success envelope");
    CHECK(v.is_object() && v.has("data"), "envelope object with data");
    const Value* data = v.find("data");
    CHECK(data && data->is_object(), "data is object");
    CHECK(courier::json::get_str(*data, "accessToken") == "a", "get_str");
    CHECK(courier::json::get_i64(*data, "amountCents", 0) == 600, "get_i64 int");
    CHECK(courier::json::get_i64(*data, "missing", 7) == 7, "get_i64 default");

    CHECK(ok_parse("{\"data\":null}", v), "parse data null");
    CHECK(v.find("data") && v.find("data")->is_null(), "data null value");

    CHECK(ok_parse(
        "{\"error\":{\"code\":\"AUTH_TOKEN_EXPIRED\",\"message\":\"expired\",\"retryable\":false},"
        "\"traceId\":\"tr-1\"}", v), "parse error envelope");
    const Value* errObj = v.find("error");
    CHECK(errObj && courier::json::get_str(*errObj, "code") == "AUTH_TOKEN_EXPIRED", "error code");
    CHECK(courier::json::get_bool(*errObj, "retryable", true) == false, "error retryable");
    CHECK(courier::json::get_str(v, "traceId") == "tr-1", "traceId top-level");

    // ---- 分页形状 ----
    CHECK(ok_parse("{\"items\":[{\"id\":\"a\"},{\"id\":\"b\"}],\"nextCursor\":\"c1\"}", v), "parse page");
    const Value* items = v.find("items");
    CHECK(items && items->is_array() && items->items().size() == 2, "page items array");
    CHECK(courier::json::get_str(items->items()[1], "id") == "b", "page item field");

    // ---- 字符串转义与 UTF-8 ----
    CHECK(ok_parse("\"a\\\"b\\\\c\\nd\\te\\u0041\"", v) &&
              v.as_string() == "a\"b\\c\nd\teA", "escape forms");
    CHECK(ok_parse("\"\\ud83d\\ude00\"", v) && v.as_string() == "\xF0\x9F\x98\x80",
          "surrogate pair → UTF-8 emoji");
    CHECK(ok_parse("\"中文\"", v) && v.as_string() == "中文", "raw UTF-8 passthrough");
    CHECK(ok_parse("\"\\u4e2d\"", v) && v.as_string() == "中", "\\u CJK");
    CHECK(bad_parse("\"\\ud83d\""), "lone high surrogate rejected");
    CHECK(bad_parse("\"\\ude00\""), "lone low surrogate rejected");
    CHECK(bad_parse("\"unterminated"), "unterminated string rejected");
    CHECK(bad_parse("\"a\x01\""), "raw control char rejected");

    // ---- 数字 ----
    CHECK(ok_parse("[0,-5,600,3.14,1e3,-2.5E-2]", v), "number forms");
    const std::vector<Value>& nums = v.items();
    CHECK(nums[0].as_int() == 0 && nums[1].as_int() == -5 && nums[2].as_int() == 600,
          "integers");
    CHECK(nums[2].as_double() == 600.0, "int as_double");
    CHECK(nums[3].as_double() == 3.14 && !nums[3].is_number() == false, "float");
    CHECK(nums[4].as_int() == 1000, "1e3 as_int truncates via double");
    CHECK(nums[5].as_double() > -0.026 && nums[5].as_double() < -0.024, "exponent");
    CHECK(bad_parse("01"), "leading zero rejected");
    CHECK(bad_parse("1."), "trailing dot rejected");
    CHECK(bad_parse("-"), "bare minus rejected");
    CHECK(bad_parse("+1"), "leading plus rejected");

    // ---- 结构边界(拒绝) ----
    CHECK(bad_parse(""), "empty rejected");
    CHECK(bad_parse("   "), "ws-only rejected");
    CHECK(bad_parse("{"), "unclosed object rejected");
    CHECK(bad_parse("{\"a\":1,}"), "trailing comma in object rejected");
    CHECK(bad_parse("[1,]"), "trailing comma in array rejected");
    CHECK(bad_parse("{\"a\" 1}"), "missing colon rejected");
    CHECK(bad_parse("[1] extra"), "trailing garbage rejected");
    CHECK(bad_parse("tru"), "bad literal rejected");
    CHECK(ok_parse("  [ 1 , 2 ]  ", v) && v.items().size() == 2, "ws tolerated");
    CHECK(ok_parse("{}", v) && v.is_object() && !v.has("x"), "empty object");
    CHECK(ok_parse("[]", v) && v.is_array() && v.items().empty(), "empty array");

    // 深度上限(65 层拒,64 层收)。
    {
        std::string deep65, deep64;
        for (int i = 0; i < 65; i++) deep65 += "[";
        for (int i = 0; i < 65; i++) deep65 += "]";
        for (int i = 0; i < 64; i++) deep64 += "[";
        for (int i = 0; i < 64; i++) deep64 += "]";
        CHECK(bad_parse(deep65.c_str()), "depth 65 rejected");
        CHECK(ok_parse(deep64.c_str(), v), "depth 64 accepted");
    }

    // ---- 序列化(dump) ----
    {
        Value obj = Value::object();
        obj.set("s", Value::str("a\"b\nc\xd"));
        obj.set("ctl", Value::str(std::string(1, '\x01')));
        obj.set("i", Value::integer(-600));
        obj.set("d", Value::number(3.5));
        obj.set("b", Value::boolean(true));
        obj.set("n", Value::null());
        Value arr = Value::array();
        arr.push(Value::integer(1));
        arr.push(Value::str("x"));
        obj.set("arr", std::move(arr));

        const std::string dumped = obj.dump();
        Value back;
        CHECK(ok_parse(dumped.c_str(), back), "dump round-trip parses");
        CHECK(courier::json::get_str(back, "s") == "a\"b\nc\xd", "dump string round-trip");
        CHECK(courier::json::get_str(back, "ctl") == std::string(1, '\x01'), "control char \\u00xx round-trip");
        CHECK(courier::json::get_i64(back, "i", 0) == -600, "dump int round-trip");
        CHECK(courier::json::get_bool(back, "b", false), "dump bool round-trip");
        CHECK(back.find("n") && back.find("n")->is_null(), "dump null round-trip");
        CHECK(back.find("arr") && back.find("arr")->items().size() == 2, "dump array round-trip");
        // 数字转义形态:控制字符必须已成 \u00xx。
        CHECK(dumped.find("\\u0001") != std::string::npos, "ctl dumped as \\u escape");
        // 中文透传(不被转义)。
        Value cjk = Value::object();
        cjk.set("q", Value::str("维护公告"));
        const std::string cjkDump = cjk.dump();
        CHECK(cjkDump.find("维护公告") != std::string::npos, "CJK passthrough in dump");
    }

    // ---- 覆盖键:后值胜 ----
    CHECK(ok_parse("{\"k\":1,\"k\":2}", v) && courier::json::get_i64(v, "k", 0) == 2,
          "duplicate key last wins");

    // ---- 取值辅助:类型不符安全 ----
    CHECK(ok_parse("{\"s\":\"x\",\"i\":3,\"b\":true}", v), "helper fixture");
    CHECK(courier::json::get_str(v, "i").empty(), "get_str type mismatch → empty");
    CHECK(courier::json::get_i64(v, "s", 9) == 9, "get_i64 type mismatch → default");
    CHECK(courier::json::get_bool(v, "s", true), "get_bool type mismatch → default");
    std::string opt;
    CHECK(!courier::json::get_opt_str(v, "i", opt), "get_opt_str type mismatch → false");
    CHECK(courier::json::get_opt_str(v, "s", opt) && opt == "x", "get_opt_str hit");
    CHECK(!courier::json::has_field(v, "nope"), "has_field miss");
    CHECK(v.find("s") != nullptr && v.find("nope") == nullptr, "find hit/miss");
    Value scalar = Value::integer(5);
    CHECK(scalar.find("k") == nullptr, "find on non-object → nullptr");

    if (gFailed > 0) {
        std::printf("ue json tests: %d failed\n", gFailed);
        return 1;
    }
    std::printf("ue json tests: all green\n");
    return 0;
}
