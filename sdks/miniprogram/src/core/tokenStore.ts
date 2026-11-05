// 会话令牌存储抽象(unity ITokenStore 同构):接入方按平台隔离实现
// (Cocos 原生 / 微信小游戏 storage;禁止明文 PlayerPrefs 形态的全局缓存)。
import type { SessionDto } from "./types.ts";

export interface TokenStore {
  load(): SessionDto | null;
  save(session: SessionDto): void;
  clear(): void;
}

/** 会话形状校验(契约 auth.md 会话数据模型):合法会话必须为对象且五个
 *  必填凭证/设备字段均为非空字符串——缺失/空串/非字符串即非会话。
 *  (平台适配器 load 用它区分「合法 JSON 但非会话对象」与真实会话;损坏数据
 *   按「未认证」清场,不抛。) */
export function isSessionDto(v: unknown): v is SessionDto {
  if (typeof v !== "object" || v === null || Array.isArray(v)) {
    return false;
  }
  const s = v as Record<string, unknown>;
  return typeof s.accessToken === "string" && s.accessToken.length > 0 &&
    typeof s.refreshToken === "string" && s.refreshToken.length > 0 &&
    typeof s.accessExpiresAt === "string" && s.accessExpiresAt.length > 0 &&
    typeof s.refreshExpiresAt === "string" && s.refreshExpiresAt.length > 0 &&
    typeof s.deviceId === "string" && s.deviceId.length > 0;
}

/** 内存实现(测试与不落盘场景默认;进程结束即失效)。 */
export class MemoryTokenStore implements TokenStore {
  private session: SessionDto | null = null;

  load(): SessionDto | null {
    return this.session;
  }

  save(session: SessionDto): void {
    this.session = session;
  }

  clear(): void {
    this.session = null;
  }
}
