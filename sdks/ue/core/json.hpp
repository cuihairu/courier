// L2 Core:最小 JSON(ISO C++17,零依赖)——wire 形状已知且简单(信封/DTO),
// 不引第三方库。RFC 8259 子集:\uXXXX 转义含代理对;数字整型/浮点双存
// (金额分位 cents 走整型);对象保序(键查找线性,DTO 规模无所谓)。
#ifndef COURIER_CORE_JSON_HPP
#define COURIER_CORE_JSON_HPP

#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <string>
#include <utility>
#include <vector>

namespace courier {
namespace json {

class Value {
public:
    enum class Type { kNull, kBool, kNumber, kString, kArray, kObject };

    Value() : type_(Type::kNull) {}

    static Value null() { return Value(); }
    static Value boolean(bool v) {
        Value x;
        x.type_ = Type::kBool;
        x.bool_ = v;
        return x;
    }
    static Value integer(std::int64_t v) {
        Value x;
        x.type_ = Type::kNumber;
        x.is_int_ = true;
        x.int_ = v;
        return x;
    }
    static Value number(double v) {
        Value x;
        x.type_ = Type::kNumber;
        x.num_ = v;
        return x;
    }
    static Value str(std::string v) {
        Value x;
        x.type_ = Type::kString;
        x.str_ = std::move(v);
        return x;
    }
    static Value array() {
        Value x;
        x.type_ = Type::kArray;
        return x;
    }
    static Value object() {
        Value x;
        x.type_ = Type::kObject;
        return x;
    }

    Type type() const { return type_; }
    bool is_null() const { return type_ == Type::kNull; }
    bool is_bool() const { return type_ == Type::kBool; }
    bool is_number() const { return type_ == Type::kNumber; }
    bool is_string() const { return type_ == Type::kString; }
    bool is_array() const { return type_ == Type::kArray; }
    bool is_object() const { return type_ == Type::kObject; }

    bool as_bool() const { return bool_; }
    std::int64_t as_int() const { return is_int_ ? int_ : static_cast<std::int64_t>(num_); }
    double as_double() const { return is_int_ ? static_cast<double>(int_) : num_; }
    const std::string& as_string() const { return str_; }
    const std::vector<Value>& items() const { return arr_; }

    /// 对象键查找;缺失/非对象 → nullptr。
    const Value* find(const std::string& key) const {
        if (type_ != Type::kObject) {
            return nullptr;
        }
        for (const auto& kv : obj_) {
            if (kv.first == key) {
                return &kv.second;
            }
        }
        return nullptr;
    }

    bool has(const std::string& key) const { return find(key) != nullptr; }

    /// 对象置键(已存在则覆盖;非对象调用为编程错误,无副作用)。
    Value& set(const std::string& key, Value v) {
        if (type_ == Type::kObject) {
            for (auto& kv : obj_) {
                if (kv.first == key) {
                    kv.second = std::move(v);
                    return kv.second;
                }
            }
            obj_.emplace_back(key, std::move(v));
            return obj_.back().second;
        }
        static Value sink;
        return sink;
    }

    /// 数组追加(非数组调用为编程错误,无副作用)。
    void push(Value v) {
        if (type_ == Type::kArray) {
            arr_.push_back(std::move(v));
        }
    }

    std::string dump() const {
        std::string out;
        dump_to(out);
        return out;
    }

private:
    void dump_to(std::string& out) const {
        switch (type_) {
            case Type::kNull:
                out += "null";
                break;
            case Type::kBool:
                out += bool_ ? "true" : "false";
                break;
            case Type::kNumber:
                if (is_int_) {
                    char buf[32];
                    std::snprintf(buf, sizeof(buf), "%lld", static_cast<long long>(int_));
                    out += buf;
                } else {
                    char buf[40];
                    std::snprintf(buf, sizeof(buf), "%.17g", num_);
                    out += buf;
                }
                break;
            case Type::kString:
                dump_string(out, str_);
                break;
            case Type::kArray:
                out += '[';
                for (std::size_t i = 0; i < arr_.size(); i++) {
                    if (i > 0) out += ',';
                    arr_[i].dump_to(out);
                }
                out += ']';
                break;
            case Type::kObject:
                out += '{';
                for (std::size_t i = 0; i < obj_.size(); i++) {
                    if (i > 0) out += ',';
                    dump_string(out, obj_[i].first);
                    out += ':';
                    obj_[i].second.dump_to(out);
                }
                out += '}';
                break;
        }
    }

    static void dump_string(std::string& out, const std::string& s) {
        out += '"';
        for (const char ch : s) {
            switch (ch) {
                case '"': out += "\\\""; break;
                case '\\': out += "\\\\"; break;
                case '\b': out += "\\b"; break;
                case '\f': out += "\\f"; break;
                case '\n': out += "\\n"; break;
                case '\r': out += "\\r"; break;
                case '\t': out += "\\t"; break;
                default:
                    if (static_cast<unsigned char>(ch) < 0x20) {
                        char buf[8];
                        std::snprintf(buf, sizeof(buf), "\\u%04x", ch);
                        out += buf;
                    } else {
                        out += ch;  // UTF-8 原样透传
                    }
            }
        }
        out += '"';
    }

    Type type_;
    bool bool_ = false;
    bool is_int_ = false;
    std::int64_t int_ = 0;
    double num_ = 0.0;
    std::string str_;
    std::vector<Value> arr_;
    std::vector<std::pair<std::string, Value>> obj_;
};

namespace detail {

inline void skip_ws(const std::string& s, std::size_t& pos) {
    while (pos < s.size() &&
           (s[pos] == ' ' || s[pos] == '\t' || s[pos] == '\n' || s[pos] == '\r')) {
        pos++;
    }
}

inline void utf8_append(std::string& out, unsigned int cp) {
    if (cp < 0x80) {
        out += static_cast<char>(cp);
    } else if (cp < 0x800) {
        out += static_cast<char>(0xC0 | (cp >> 6));
        out += static_cast<char>(0x80 | (cp & 0x3F));
    } else if (cp < 0x10000) {
        out += static_cast<char>(0xE0 | (cp >> 12));
        out += static_cast<char>(0x80 | ((cp >> 6) & 0x3F));
        out += static_cast<char>(0x80 | (cp & 0x3F));
    } else {
        out += static_cast<char>(0xF0 | (cp >> 18));
        out += static_cast<char>(0x80 | ((cp >> 12) & 0x3F));
        out += static_cast<char>(0x80 | ((cp >> 6) & 0x3F));
        out += static_cast<char>(0x80 | (cp & 0x3F));
    }
}

inline bool parse_hex4(const std::string& s, std::size_t pos, unsigned int& out) {
    if (pos + 4 > s.size()) return false;
    unsigned int v = 0;
    for (std::size_t i = 0; i < 4; i++) {
        const char c = s[pos + i];
        v <<= 4;
        if (c >= '0' && c <= '9') {
            v |= static_cast<unsigned int>(c - '0');
        } else if (c >= 'a' && c <= 'f') {
            v |= static_cast<unsigned int>(c - 'a' + 10);
        } else if (c >= 'A' && c <= 'F') {
            v |= static_cast<unsigned int>(c - 'A' + 10);
        } else {
            return false;
        }
    }
    out = v;
    return true;
}

struct Parser {
    explicit Parser(const std::string& src) : s(src) {}
    const std::string& s;
    std::size_t pos = 0;
    std::string err;
    int depth = 0;

    static constexpr int kMaxDepth = 64;

    bool fail(const char* why) {
        if (err.empty()) {
            err = std::string(why) + " at " + std::to_string(pos);
        }
        return false;
    }

    bool parse_value(Value& out) {
        skip_ws(s, pos);
        if (pos >= s.size()) {
            return fail("unexpected end of input");
        }
        if (++depth > kMaxDepth) {
            return fail("nesting too deep");
        }
        const bool ok = parse_value_inner(out);
        depth--;
        return ok;
    }

    bool parse_value_inner(Value& out) {
        switch (s[pos]) {
            case '{': return parse_object(out);
            case '[': return parse_array(out);
            case '"': {
                std::string v;
                if (!parse_string(v)) return false;
                out = Value::str(std::move(v));
                return true;
            }
            case 't':
                if (s.compare(pos, 4, "true") == 0) {
                    pos += 4;
                    out = Value::boolean(true);
                    return true;
                }
                return fail("bad literal");
            case 'f':
                if (s.compare(pos, 5, "false") == 0) {
                    pos += 5;
                    out = Value::boolean(false);
                    return true;
                }
                return fail("bad literal");
            case 'n':
                if (s.compare(pos, 4, "null") == 0) {
                    pos += 4;
                    out = Value::null();
                    return true;
                }
                return fail("bad literal");
            default:
                if (s[pos] == '-' || (s[pos] >= '0' && s[pos] <= '9')) {
                    return parse_number(out);
                }
                return fail("unexpected character");
        }
    }

    bool parse_object(Value& out) {
        Value obj = Value::object();
        pos++;  // {
        skip_ws(s, pos);
        if (pos < s.size() && s[pos] == '}') {
            pos++;
            out = std::move(obj);
            return true;
        }
        while (true) {
            skip_ws(s, pos);
            if (pos >= s.size() || s[pos] != '"') {
                return fail("expected object key");
            }
            std::string key;
            if (!parse_string(key)) return false;
            skip_ws(s, pos);
            if (pos >= s.size() || s[pos] != ':') {
                return fail("expected ':'");
            }
            pos++;
            Value v;
            if (!parse_value(v)) return false;
            if (obj.type() == Value::Type::kObject) {
                obj.set(key, std::move(v));
            }
            skip_ws(s, pos);
            if (pos < s.size() && s[pos] == ',') {
                pos++;
                continue;
            }
            if (pos < s.size() && s[pos] == '}') {
                pos++;
                out = std::move(obj);
                return true;
            }
            return fail("expected ',' or '}'");
        }
    }

    bool parse_array(Value& out) {
        Value arr = Value::array();
        pos++;  // [
        skip_ws(s, pos);
        if (pos < s.size() && s[pos] == ']') {
            pos++;
            out = std::move(arr);
            return true;
        }
        while (true) {
            Value v;
            if (!parse_value(v)) return false;
            arr.push(std::move(v));
            skip_ws(s, pos);
            if (pos < s.size() && s[pos] == ',') {
                pos++;
                continue;
            }
            if (pos < s.size() && s[pos] == ']') {
                pos++;
                out = std::move(arr);
                return true;
            }
            return fail("expected ',' or ']'");
        }
    }

    bool parse_string(std::string& out) {
        pos++;  // 开引号
        std::string v;
        while (pos < s.size()) {
            const char c = s[pos];
            if (c == '"') {
                pos++;
                out = std::move(v);
                return true;
            }
            if (c == '\\') {
                pos++;
                if (pos >= s.size()) return fail("bad escape");
                switch (s[pos]) {
                    case '"': v += '"'; break;
                    case '\\': v += '\\'; break;
                    case '/': v += '/'; break;
                    case 'b': v += '\b'; break;
                    case 'f': v += '\f'; break;
                    case 'n': v += '\n'; break;
                    case 'r': v += '\r'; break;
                    case 't': v += '\t'; break;
                    case 'u': {
                        unsigned int cp = 0;
                        if (!parse_hex4(s, pos + 1, cp)) return fail("bad \\u escape");
                        pos += 4;
                        // 代理对:高代理后必须跟 \uDC00-\uDFFF。
                        if (cp >= 0xD800 && cp <= 0xDBFF) {
                            if (pos + 6 > s.size() || s[pos + 1] != '\\' || s[pos + 2] != 'u') {
                                return fail("lone high surrogate");
                            }
                            unsigned int lo = 0;
                            if (!parse_hex4(s, pos + 3, lo) || lo < 0xDC00 || lo > 0xDFFF) {
                                return fail("bad low surrogate");
                            }
                            cp = 0x10000 + ((cp - 0xD800) << 10) + (lo - 0xDC00);
                            pos += 6;
                        } else if (cp >= 0xDC00 && cp <= 0xDFFF) {
                            return fail("lone low surrogate");
                        }
                        utf8_append(v, cp);
                        break;
                    }
                    default:
                        return fail("bad escape");
                }
                pos++;
                continue;
            }
            if (static_cast<unsigned char>(c) < 0x20) {
                return fail("raw control char in string");
            }
            v += c;
            pos++;
        }
        return fail("unterminated string");
    }

    bool parse_number(Value& out) {
        const std::size_t start = pos;
        if (s[pos] == '-') pos++;
        // 整数部分:零 或 馀位非零开头(RFC 8259,拒绝前导零)。
        if (pos >= s.size() || s[pos] < '0' || s[pos] > '9') {
            return fail("bad number");
        }
        if (s[pos] == '0') {
            pos++;
        } else {
            while (pos < s.size() && s[pos] >= '0' && s[pos] <= '9') pos++;
        }
        bool integral = true;
        if (pos < s.size() && s[pos] == '.') {
            integral = false;
            pos++;
            if (pos >= s.size() || s[pos] < '0' || s[pos] > '9') {
                return fail("bad number fraction");
            }
            while (pos < s.size() && s[pos] >= '0' && s[pos] <= '9') pos++;
        }
        if (pos < s.size() && (s[pos] == 'e' || s[pos] == 'E')) {
            integral = false;
            pos++;
            if (pos < s.size() && (s[pos] == '+' || s[pos] == '-')) pos++;
            if (pos >= s.size() || s[pos] < '0' || s[pos] > '9') {
                return fail("bad number exponent");
            }
            while (pos < s.size() && s[pos] >= '0' && s[pos] <= '9') pos++;
        }
        const std::string lexeme = s.substr(start, pos - start);
        if (integral) {
            out = Value::integer(std::strtoll(lexeme.c_str(), nullptr, 10));
        } else {
            out = Value::number(std::strtod(lexeme.c_str(), nullptr));
        }
        return true;
    }
};

}  // namespace detail

/// 解析整个文本(允首尾空白,拒绝尾随垃圾);失败返回 false 且 err 带原因。
inline bool parse(const std::string& text, Value& out, std::string& err) {
    detail::Parser p(text);
    if (!p.parse_value(out)) {
        err = p.err;
        return false;
    }
    detail::skip_ws(text, p.pos);
    if (p.pos != text.size()) {
        err = "trailing garbage at " + std::to_string(p.pos);
        return false;
    }
    return true;
}

// ---- DTO 取值辅助(键缺失/类型不符安全,不抛) ----

inline bool has_field(const Value& v, const char* key) {
    return v.find(key) != nullptr;
}

/// 字符串;缺失/类型不符 → ""。
inline std::string get_str(const Value& v, const char* key) {
    const Value* f = v.find(key);
    return (f && f->is_string()) ? f->as_string() : std::string();
}

inline bool get_bool(const Value& v, const char* key, bool def) {
    const Value* f = v.find(key);
    return (f && f->is_bool()) ? f->as_bool() : def;
}

inline std::int64_t get_i64(const Value& v, const char* key, std::int64_t def) {
    const Value* f = v.find(key);
    return (f && f->is_number()) ? f->as_int() : def;
}

/// 可选字符串:存在且是字符串 → true。
inline bool get_opt_str(const Value& v, const char* key, std::string& out) {
    const Value* f = v.find(key);
    if (f && f->is_string()) {
        out = f->as_string();
        return true;
    }
    return false;
}

}  // namespace json
}  // namespace courier

#endif  // COURIER_CORE_JSON_HPP
