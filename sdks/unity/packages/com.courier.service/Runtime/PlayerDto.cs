// M3 玩家档案域 DTO(契约 player.md Frozen v1)。wire camelCase 由 Core Json
// resolver 统一映射;createdAt/updatedAt 为服务端时间戳字符串,SDK 不解释。
using System.Collections.Generic;

namespace Courier.Service
{
    /// <summary>账号档案:懒建(默认展示名 Player),displayName 1-30 字符(服务端修剪)。</summary>
    public sealed class PlayerProfileDto
    {
        public string DisplayName { get; set; }
        public string AvatarUrl { get; set; }
        public string CreatedAt { get; set; }
        public string UpdatedAt { get; set; }
    }

    /// <summary>已绑定角色:账号↔角色映射(角色数据归各游戏,本域只存映射)。</summary>
    public sealed class PlayerCharacterDto
    {
        public string PlayerId { get; set; }
        public string BoundAt { get; set; }
    }

    /// <summary>角色映射分页:v1 网关单页返回(nextCursor 恒空,字段为向前兼容保留)。</summary>
    public sealed class PlayerCharactersPageDto
    {
        public List<PlayerCharacterDto> Items { get; set; }
        public string NextCursor { get; set; }
    }
}
