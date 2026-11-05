// L3 平台适配(Cocos Creator 原生/Web):TokenStore 的 localStorage 实现。
// 会话落盘引擎 localStorage(key 前缀隔离);损坏数据按「未认证」清场,不抛。
import type { TokenStore } from "../../core/tokenStore.ts";
import type { SessionDto } from "../../core/types.ts";
import type { CocosStorage } from "./cocosApi.ts";

const defaultKey = "courier.session";

export function createCocosTokenStore(storage: CocosStorage, key: string = defaultKey): TokenStore {
  return {
    load(): SessionDto | null {
      let raw: string | null;
      try {
        raw = storage.getItem(key);
      } catch {
        return null;
      }
      if (!raw) {
        return null;
      }
      try {
        const parsed: unknown = JSON.parse(raw);
        if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed) ||
            typeof (parsed as { accessToken?: unknown }).accessToken !== "string") {
          // 合法 JSON 但非会话对象(字符串/数字/数组/`{}`/缺 accessToken):按未认证清场。
          storage.removeItem(key);
          return null;
        }
        return parsed as SessionDto;
      } catch {
        storage.removeItem(key); // 损坏数据:本地清场(下次请求自然 401)
        return null;
      }
    },
    save(session: SessionDto): void {
      storage.setItem(key, JSON.stringify(session));
    },
    clear(): void {
      storage.removeItem(key);
    },
  };
}
