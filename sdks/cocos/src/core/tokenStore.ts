// 会话令牌存储抽象(unity ITokenStore 同构):接入方按平台隔离实现
// (Cocos 原生 / 微信小游戏 storage;禁止明文 PlayerPrefs 形态的全局缓存)。
import type { SessionDto } from "./types.ts";

export interface TokenStore {
  load(): SessionDto | null;
  save(session: SessionDto): void;
  clear(): void;
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
