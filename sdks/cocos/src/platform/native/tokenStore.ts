// L3 平台适配(Cocos Creator 原生/Web):TokenStore 的 localStorage 实现。
// 会话落盘引擎 localStorage(key 前缀隔离);损坏数据按「未认证」清场,不抛。
import { isSessionDto } from "../../core/tokenStore.ts";
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
        if (!isSessionDto(parsed)) {
          // 合法 JSON 但非会话对象(字符串/数字/数组/`{}`/缺字段/空 token):按未认证清场。
          try {
            storage.removeItem(key);
          } catch {
            // 删除失败(磁盘满/权限/存储异常):仍按未认证降级,不抛。
          }
          return null;
        }
        return parsed;
      } catch {
        try {
          storage.removeItem(key); // 损坏数据:本地清场(下次请求自然 401)
        } catch {
          // 删除失败:仍按未认证降级,不抛。
        }
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
