// L3 平台适配(微信小程序):TokenStore 的 wx storage 实现。
// 会话落盘微信本地存储(key 前缀隔离);损坏数据按「未认证」清场,不抛。
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
        return JSON.parse(raw) as SessionDto;
      } catch {
        wx.removeStorageSync(key); // 损坏数据:本地清场(下次请求自然 401)
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
