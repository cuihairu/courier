// L3 平台适配(微信小程序):wx 全局的最小结构类型(零外部依赖,按需面定义)。
// 接入方把真实 wx 对象传入工厂;测试注入同构 fake(结构类型,鸭子匹配)。

/** 同步存储面(wx.getStorageSync 等;值按字符串存取)。 */
export interface WxStorage {
  getStorageSync(key: string): string;
  setStorageSync(key: string, value: string): void;
  removeStorageSync(key: string): void;
}

export interface WxRequestTask {
  abort(): void;
}

/** wx.request 选项(只取 SDK 用到的面;data 为请求体字符串)。 */
export interface WxRequestOptions {
  url: string;
  method: string;
  header: Record<string, string>;
  data?: string;
  timeout?: number;
  success(res: { statusCode: number; header: Record<string, string>; data?: unknown }): void;
  fail(err: { errMsg: string }): void;
}

export interface WxRequester {
  request(options: WxRequestOptions): WxRequestTask;
}

/** 前后台/网络信号面(小程序:wx.onAppShow/onAppHide/onNetworkStatusChange)。 */
export interface WxAppHooks {
  onAppShow(callback: () => void): void;
  onAppHide(callback: () => void): void;
  onNetworkStatusChange(callback: (res: { isConnected: boolean }) => void): void;
}

/** 本适配器用到的 wx 面合集(传真实 wx 或测试 fake 均可)。 */
export type WechatApi = WxStorage & WxRequester & WxAppHooks;
