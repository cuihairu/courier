// M3 品牌客户端(契约 branding.md Frozen v1)。SDK 透传原始 JSON——零解释、
// 零渲染、零缓存策略(缓存归宿主/接入方);兜底规则与消费全在接入方 UI。
// 501 能力未接 → null(UI 全默认)。匿名可:登录页就要显示品牌。
import type { ApiClient } from "../core/apiClient.ts";
import { isCapabilityDisabled } from "../core/courierError.ts";
import type { BrandingDto } from "./appDto.ts";

const path = "/v1/app/branding";

export class BrandingService {
  private readonly api: ApiClient;
  private cached: BrandingDto | null = null;

  constructor(api: ApiClient) {
    this.api = api;
  }

  /** 最近一次成功拉取;null = 从未拉到。 */
  get cachedBranding(): BrandingDto | null {
    return this.cached;
  }

  /** 拉取品牌物料(version + 业务字段透传)。501 → null(未启用态,UI 全默认)。 */
  async fetchAsync(): Promise<BrandingDto | null> {
    try {
      // 匿名(withAuth=false):登录页消费。
      const dto = await this.api.request<BrandingDto>("GET", path, undefined, false);
      this.cached = dto;
      return dto;
    } catch (e) {
      if (isCapabilityDisabled(e)) {
        return null;
      }
      throw e;
    }
  }

  /** branding.updated 事件到达后的重拉判据:事件 version 与缓存不同才需要重拉
   *  (相同即忽略,不重渲染);从未拉到过 → 一律重拉。 */
  needsRefetch(eventVersion: number): boolean {
    return this.cached === null || this.cached.version !== eventVersion;
  }
}
