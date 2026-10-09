// L3 平台适配(Layabox):平台面的最小结构类型(零外部依赖,按需面定义)。
// 接入方把真实平台对象传入工厂;测试注入同构 fake(结构类型,鸭子匹配)。
// LayaAir 各目标(Web/小游戏壳)普遍可用 XHR 与 LocalStorage 形态,故按此立面;
// 引擎版本差异收敛在接入方绑定侧,不进 SDK。

/** 同步存储面(Laya.LocalStorage / window.localStorage 同形;值按字符串存取)。 */
export interface LayaStorage {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
  removeItem(key: string): void;
}

/** XMLHttpRequest 最小面(只取 SDK 用到的属性与方法)。 */
export interface XhrLike {
  open(method: string, url: string): void;
  setRequestHeader(name: string, value: string): void;
  send(body?: string | null): void;
  timeout: number;
  status: number;
  responseText: string;
  getAllResponseHeaders(): string;
  onload: (() => void) | null;
  onerror: (() => void) | null;
  ontimeout: (() => void) | null;
}

/** XHR 构造面(注入真实 XMLHttpRequest 构造器或测试 fake)。 */
export type XhrFactory = () => XhrLike;

/** 前后台/网络信号面(LayaAir 运行时 = 文档可见性 + navigator onLine;
 *  接入方从引擎事件桥接到此面,测试注入 fake)。 */
export interface LayaboxAppHooks {
  onVisibilityChange(callback: (visible: boolean) => void): void;
  onNetworkStatusChange(callback: (isConnected: boolean) => void): void;
}
