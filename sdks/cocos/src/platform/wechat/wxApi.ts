// L3 平台适配(微信小游戏):wx 全局的最小结构类型(零外部依赖,按需面定义)。
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

/** wx.request 分块(chunked)选项:options 带 enableChunked,响应体经
 *  requestTask.onChunkReceived 交付,success 回调不再携带 data。 */
export interface WxChunkedRequestOptions {
  url: string;
  method: string;
  header: Record<string, string>;
  enableChunked: boolean;
  success(res: { statusCode: number; header: Record<string, string> }): void;
  fail(err: { errMsg: string }): void;
}

/** 分块任务面:多一个 onChunkReceived(按块交付 ArrayBuffer)。 */
export interface WxChunkedRequestTask extends WxRequestTask {
  onChunkReceived(callback: (res: { data: ArrayBuffer }) => void): void;
}

export interface WxRequester {
  /** 普通请求与分块请求共用同一方法;options 带 enableChunked 时按分块处理。 */
  request(options: WxRequestOptions | WxChunkedRequestOptions): WxRequestTask | WxChunkedRequestTask;
}

/** 流式适配需要的 wx 面(createWechatSseSource 入参;真实 wx 天然满足)。 */
export interface WxChunkedRequester {
  request(options: WxChunkedRequestOptions): WxChunkedRequestTask;
}

/** 前后台/网络信号面(小游戏:wx.onShow/onHide/onNetworkStatusChange)。 */
export interface WxAppHooks {
  onShow(callback: () => void): void;
  onHide(callback: () => void): void;
  onNetworkStatusChange(callback: (res: { isConnected: boolean }) => void): void;
}

/** 本适配器用到的 wx 面合集(传真实 wx 或测试 fake 均可)。 */
export type WechatApi = WxStorage & WxRequester & WxAppHooks;
