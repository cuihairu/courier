// M3 UI 包品牌目录(契约 branding.md:兜底规则落 UI——Core/Service 零解释)。
// 宿主在启动与 branding.updated 事件到达时调 Apply(热切换:version 相同即忽略);
// 面板经 Get*/TryGetString 读当前品牌,任何字段缺失回落 Courier 内置默认标。
using Courier.Service;
using Newtonsoft.Json.Linq;

namespace Courier.UI
{
    public static class BrandingCatalog
    {
        // Courier 默认标(契约「兜底规则」:整能力未接/字段缺失时的内置默认)。
        public const string DefaultCompanyName = "Courier";
        public const string DefaultProductTitle = "Courier";
        public const string DefaultPrimaryColor = "#4C8DFF";
        public const string DefaultSupportLabel = "联系客服";

        static BrandingDto _current;

        /// <summary>当前生效的品牌版本;-1 = 从未应用(全默认)。</summary>
        public static long Version { get; private set; } = -1L;

        /// <summary>应用品牌物料(热切换入口):null 或 version 相同即忽略。</summary>
        public static void Apply(BrandingDto branding)
        {
            if (branding == null || branding.Version == Version)
            {
                return;
            }
            _current = branding;
            Version = branding.Version;
        }

        /// <summary>读字符串字段(可能为 null)——消费方自行串兜底链用。</summary>
        public static bool TryGetString(string field, out string value)
        {
            value = StringField(field);
            return value != null;
        }

        /// <summary>公司名(缺失 → Courier 默认)。</summary>
        public static string GetCompanyName()
        {
            return StringField("companyName") ?? DefaultCompanyName;
        }

        /// <summary>产品名(缺失 → Courier 默认)。</summary>
        public static string GetProductTitle()
        {
            return StringField("productName") ?? DefaultProductTitle;
        }

        /// <summary>主题色(缺失 → Courier 默认;#RRGGBB)。</summary>
        public static string GetPrimaryColor()
        {
            var color = NestedString("theme", "primaryColor");
            return color ?? DefaultPrimaryColor;
        }

        /// <summary>客服入口文案(缺失 → Courier 默认)。</summary>
        public static string GetSupportLabel()
        {
            var label = NestedString("supportEntry", "label");
            return label ?? DefaultSupportLabel;
        }

        /// <summary>客服入口 URL(缺失 → null,由消费方决定是否隐藏入口)。</summary>
        public static string GetSupportUrl()
        {
            return NestedString("supportEntry", "url");
        }

        static string StringField(string field)
        {
            var token = Root(field);
            return token != null && token.Type == JTokenType.String
                ? token.ToObject<string>()
                : null;
        }

        static string NestedString(string parent, string field)
        {
            var root = Root(parent);
            if (root == null || root.Type != JTokenType.Object)
            {
                return null;
            }
            var token = root[field];
            return token != null && token.Type == JTokenType.String
                ? token.ToObject<string>()
                : null;
        }

        static JToken Root(string field)
        {
            if (_current == null || _current.Fields == null)
            {
                return null;
            }
            JToken token;
            return _current.Fields.TryGetValue(field, out token) ? token : null;
        }
    }
}
