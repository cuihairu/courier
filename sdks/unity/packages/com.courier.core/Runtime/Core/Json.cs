// L2 Core:JSON 线格式策略。Newtonsoft.Json(Unity 端经 com.unity.nuget.newtonsoft-json
// 提供;dotnet 测试经 NuGet 同包)。camelCase 契约键由 resolver 统一映射;
// 未知字段容忍(契约 versioning.md:未知字段必须容忍)= Newtonsoft 默认行为。
using Newtonsoft.Json;
using Newtonsoft.Json.Serialization;

namespace Courier.Core
{
    public static class Json
    {
        public static readonly JsonSerializerSettings Settings = new JsonSerializerSettings
        {
            ContractResolver = new CamelCasePropertyNamesContractResolver(),
            NullValueHandling = NullValueHandling.Ignore,   // 可选字段缺省即未设置(契约 primitives.md)
            DefaultValueHandling = DefaultValueHandling.Ignore,
            // RFC3339 时间串保持原样(契约:毫秒精度 UTC,客户端侧再解析;不做 Date 往返改写)。
            DateParseHandling = DateParseHandling.None
        };

        public static string Serialize(object value)
        {
            return JsonConvert.SerializeObject(value, Settings);
        }

        public static T Deserialize<T>(string json)
        {
            return JsonConvert.DeserializeObject<T>(json, Settings);
        }
    }
}
