// L3 平台适配(微信小游戏):TokenStore 的 wx storage 实现。
// 会话落盘微信本地存储(key 前缀隔离);损坏数据按「未认证」清场,不抛。
import { isSessionDto } from "../../core/tokenStore.ts";
import type { TokenStore } from "../../core/tokenStore.ts";
import type { SessionDto } from "../../core/types.ts";
import type { WxStorage } from "./wxApi.ts";

const defaultKey = "courier.session";

export function createWechatTokenStore(wx: WxStorage, key: string = defaultKey): TokenStore {
  return {
    load(): SessionDto | null {
      let raw: string;
      try {
        raw = wx.getStorageSync(key);
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
            wx.removeStorageSync(key);
          } catch {
            // 删除失败(磁盘满/权限/存储异常):仍按未认证降级,不抛。
          }
          return null;
        }
        return parsed;
      } catch {
        try {
          wx.removeStorageSync(key); // 损坏数据:本地清场(下次请求自然 401)
        } catch {
          // 删除失败:仍按未认证降级,不抛。
        }
        return null;
      }
    },
    save(session: SessionDto): void {
      wx.setStorageSync(key, JSON.stringify(session));
    },
    clear(): void {
      wx.removeStorageSync(key);
    },
  };
}
