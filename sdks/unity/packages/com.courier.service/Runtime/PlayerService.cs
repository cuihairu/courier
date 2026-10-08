// M3 玩家档案客户端(契约 player.md Frozen v1)。四端点全 Bearer;
// 501 能力未接 → null(调用方隐藏档案 UI,能力安静地不存在)。
// 红线同契约:SDK 不采集邮箱/手机/设备/行为字段;角色数据归各游戏。
using System.Threading;
using System.Threading.Tasks;
using Courier.Core;

namespace Courier.Service
{
    /// <summary>档案修改请求(PATCH):null 字段不下发(只改提供的字段)。</summary>
    public sealed class PlayerProfileUpdateRequest
    {
        public string DisplayName { get; set; }
        public string AvatarUrl { get; set; }
    }

    public sealed class PlayerService
    {
        const string Prefix = "/v1/player";
        const string CodeCapabilityDisabled = "COMMON_CAPABILITY_DISABLED";

        readonly ApiClient _api;

        public PlayerService(ApiClient api)
        {
            _api = api;
        }

        /// <summary>拉取账号档案(懒建:首次即默认档案,展示名 Player)。</summary>
        public async Task<PlayerProfileDto> GetProfileAsync(CancellationToken ct)
        {
            return await SendOrDisabled<PlayerProfileDto>("GET", Prefix + "/profile", null, ct)
                .ConfigureAwait(false);
        }

        /// <summary>修改档案:displayName/avatarUrl 传 null 表示不改该字段;
        /// 响应体为准(服务端修剪后的值),调用方以返回值刷新 UI。</summary>
        public async Task<PlayerProfileDto> UpdateProfileAsync(string displayName, string avatarUrl,
            CancellationToken ct)
        {
            return await SendOrDisabled<PlayerProfileDto>("PATCH", Prefix + "/profile",
                new PlayerProfileUpdateRequest { DisplayName = displayName, AvatarUrl = avatarUrl },
                ct).ConfigureAwait(false);
        }

        /// <summary>本游戏已绑定角色映射(boundAt 升序;按 scope 隔离,服务端语义)。</summary>
        public async Task<PlayerCharactersPageDto> ListCharactersAsync(CancellationToken ct)
        {
            return await SendOrDisabled<PlayerCharactersPageDto>("GET", Prefix + "/characters",
                null, ct).ConfigureAwait(false);
        }

        /// <summary>绑定角色:幂等(重复绑定返回既有记录);上限 50/账号/游戏。</summary>
        public async Task<PlayerCharacterDto> BindCharacterAsync(string playerId,
            CancellationToken ct)
        {
            return await SendOrDisabled<PlayerCharacterDto>("POST", Prefix + "/characters",
                new BindCharacterRequest { PlayerId = playerId }, ct).ConfigureAwait(false);
        }

        // 降级语义:501 能力关闭 → null(未启用态,不进报错路径);其余错误照常抛。
        async Task<T> SendOrDisabled<T>(string method, string path, object body,
            CancellationToken ct) where T : class
        {
            try
            {
                var json = await _api.SendAsync(method, path, body, true, ct)
                    .ConfigureAwait(false);
                return Json.Deserialize<T>(json);
            }
            catch (CourierException ex)
            {
                if (ex.Error.WireCode == CodeCapabilityDisabled)
                {
                    return null;
                }
                throw;
            }
        }

        sealed class BindCharacterRequest
        {
            public string PlayerId { get; set; }
        }
    }
}
