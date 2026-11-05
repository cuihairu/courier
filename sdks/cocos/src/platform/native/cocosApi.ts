// L3 平台适配(Cocos Creator 原生/Web):引擎 sys.localStorage 的最小结构类型
// (零外部依赖,按需面定义)。接入方把真实 sys.localStorage 对象传入工厂;
// 测试注入同构 fake(结构类型,鸭子匹配)。

/** 同步存储面(Cocos Creator sys.localStorage 同形;值按字符串存取)。 */
export interface CocosStorage {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
  removeItem(key: string): void;
}
