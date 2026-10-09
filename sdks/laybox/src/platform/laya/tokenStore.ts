// L3 平台适配(Layabox):TokenStore 的 LocalStorage 实现。
// 会话落盘本地存储(key 前缀隔离);损坏数据按「未认证」清场,不抛。
import type { TokenStore } from "../../core/tokenStore.ts";
import type { SessionDto } from "../../core/types.ts";
import type { LayaStorage } from "./layaApi.ts";

const defaultKey = "courier.session";

export function createLayaboxTokenStore(storage: LayaStorage, key: string = defaultKey): TokenStore {
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
        return JSON.parse(raw) as SessionDto;
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
