// M3 玩家档案客户端(契约 player.md Frozen v1)。四端点全 Bearer;
// 501 能力未接 → null(调用方隐藏档案 UI,能力安静地不存在)。
// 红线同契约:SDK 不采集邮箱/手机/设备/行为字段;角色数据归各游戏。
import type { ApiClient } from "../core/apiClient.ts";
import type { Page } from "../contract/envelope.ts";
import { isCapabilityDisabled } from "../core/courierError.ts";

const prefix = "/v1/player";

/** 账号档案(懒建:首次即默认档案,展示名 Player)。 */
export interface PlayerProfileDto {
  readonly displayName: string;
  readonly avatarUrl?: string;
  readonly createdAt: string;
  readonly updatedAt: string;
}

/** 绑定角色(boundAt 升序;按 scope 隔离)。 */
export interface PlayerCharacterDto {
  readonly playerId: string;
  readonly boundAt: string;
}

export class PlayerService {
  private readonly api: ApiClient;

  constructor(api: ApiClient) {
    this.api = api;
  }

  /** 拉取账号档案(懒建)。 */
  async getProfileAsync(): Promise<PlayerProfileDto | null> {
    return await this.sendOrDisabled<PlayerProfileDto>("GET", prefix + "/profile");
  }

  /** 修改档案:displayName/avatarUrl 传 undefined 表示不改该字段(undefined 不下发);
   *  响应体为准(服务端修剪后的值),调用方以返回值刷新 UI。 */
  async updateProfileAsync(displayName?: string, avatarUrl?: string | null):
    Promise<PlayerProfileDto | null> {
    const body: Record<string, unknown> = {};
    if (displayName !== undefined) body.displayName = displayName;
    if (avatarUrl !== undefined) body.avatarUrl = avatarUrl;
    return await this.sendOrDisabled<PlayerProfileDto>("PATCH", prefix + "/profile", body);
  }

  /** 本游戏已绑定角色映射(boundAt 升序;按 scope 隔离,服务端语义)。 */
  async listCharactersAsync(): Promise<Page<PlayerCharacterDto> | null> {
    return await this.sendOrDisabled<Page<PlayerCharacterDto>>("GET", prefix + "/characters");
  }

  /** 绑定角色:幂等(重复绑定返回既有记录);上限 50/账号/游戏。 */
  async bindCharacterAsync(playerId: string): Promise<PlayerCharacterDto | null> {
    return await this.sendOrDisabled<PlayerCharacterDto>("POST", prefix + "/characters",
      { playerId });
  }

  private async sendOrDisabled<T>(method: string, path: string, body?: unknown):
    Promise<T | null> {
    try {
      return await this.api.request<T>(method, path, body, true);
    } catch (e) {
      if (isCapabilityDisabled(e)) {
        return null;
      }
      throw e;
    }
  }
}
