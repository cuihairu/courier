// L2 Core:平台无关配置与 scope 头常量(契约 scope.md Frozen v1)。
// 不 import 任何平台 API;JSON/传输/存储经注入接口(见 ITransport/ITokenStore)。
namespace Courier.Core
{
    /// <summary>scope 传输头(scope.md):URL path 与 payload 一律不携带 scope 字段。</summary>
    public static class ScopeHeaders
    {
        public const string GameId = "X-Courier-Game-Id";
        public const string Env = "X-Courier-Env";
    }

    /// <summary>SDK 初始化配置:CourierClient.Init({ gameId, env, endpoint }),之后对调用方不可见。</summary>
    public sealed class CourierConfig
    {
        public const string EnvDev = "dev";
        public const string EnvStaging = "staging";
        public const string EnvProd = "prod";

        public string GameId { get; }
        public string Env { get; }
        public string Endpoint { get; }

        public CourierConfig(string gameId, string env, string endpoint)
        {
            GameId = gameId;
            Env = env;
            Endpoint = endpoint;
        }

        /// <summary>校验 scope 契约:game_id 非空,env 在注册表内(dev/staging/prod,scope.md)。</summary>
        public void Validate()
        {
            if (string.IsNullOrWhiteSpace(GameId))
                throw new CourierException(CourierError.InvalidArgument("gameId required"));
            if (!IsValidEnv(Env))
                throw new CourierException(CourierError.InvalidArgument(
                    "env must be one of dev/staging/prod, got: " + (Env ?? "null")));
            if (string.IsNullOrWhiteSpace(Endpoint))
                throw new CourierException(CourierError.InvalidArgument("endpoint required"));
        }

        public static bool IsValidEnv(string env)
        {
            return env == EnvDev || env == EnvStaging || env == EnvProd;
        }
    }
}
