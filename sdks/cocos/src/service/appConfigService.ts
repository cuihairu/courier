// M3 远程配置客户端(契约 config.md Frozen v1)。
// 客户端要求:启动/进前台拉取;config.updated 事件到达后经 needsRefetch 判重拉
// (相同 version 即忽略);重拉失败沿用缓存(配置是加速器,不是依赖)。
import type { ApiClient } from "../core/apiClient.ts";
import { isCapabilityDisabled } from "../core/courierError.ts";
import type { AppConfigDto } from "./appDto.ts";

const path = "/v1/app/config";

export class AppConfigService {
  private readonly api: ApiClient;
  private cached: AppConfigDto | null = null;

  constructor(api: ApiClient) {
    this.api = api;
  }

  /** 最近一次成功拉取(缓存);null = 从未拉到。 */
  get cachedConfig(): AppConfigDto | null {
    return this.cached;
  }

  /** 拉取命中当前条件的键值全量(platform/appVersion/region 可选;缺省维度不参与过滤)。
   *  501 能力未接 → 空结果(v0 + 空 items,接入方使用内建默认值,不进报错路径)。 */
  async fetchAsync(platform?: string, appVersion?: string, region?: string): Promise<AppConfigDto> {
    const params = new URLSearchParams();
    if (platform) params.set("platform", platform);
    if (appVersion) params.set("appVersion", appVersion);
    if (region) params.set("region", region);
    const q = params.toString();
    try {
      const snapshot = await this.api.request<AppConfigDto>("GET",
        path + (q ? "?" + q : ""), undefined, true);
      this.cached = snapshot;
      return snapshot;
    } catch (e) {
      if (isCapabilityDisabled(e)) {
        return { configVersion: 0, items: {} };
      }
      throw e; // 其余错误(认证/限流/网络)照常抛
    }
  }

  /** config.updated 事件到达后的重拉判据:事件 version 与缓存不同才需要重拉
   *  (相同即忽略,不重渲染);从未拉到过 → 一律重拉。 */
  needsRefetch(eventConfigVersion: number): boolean {
    return this.cached === null || this.cached.configVersion !== eventConfigVersion;
  }

  /** 取值;键缺失返回 undefined(值原样,类型由接入方收窄)。 */
  get<T = unknown>(key: string): T | undefined {
    return this.cached?.items[key] as T | undefined;
  }
}
