// F62 兑换码域客户端(契约 cdk.md Frozen v1)。玩家侧 POST /v1/cdk/redeem 全 Bearer;
// 码生成/报表归接入方 live-ops;反滥用由全局限流覆盖。
import type { ApiClient } from "../core/apiClient.ts";
import { isCapabilityDisabled } from "../core/courierError.ts";

const prefix = "/v1/cdk";

/** 兑换结果(同账号+码重复 → repeated=true,不二次发放)。 */
export interface RedeemResultDto {
  readonly code: string;
  readonly grant: string;
  readonly repeated: boolean;
}

export class CdkService {
  private readonly api: ApiClient;

  constructor(api: ApiClient) {
    this.api = api;
  }

  /** 兑换一个码(501 → null 隐藏兑换 UI;未知码 → CDK_NOT_FOUND;限次 → CDK_EXHAUSTED)。 */
  async redeemAsync(code: string): Promise<RedeemResultDto | null> {
    try {
      return await this.api.request<RedeemResultDto>("POST", prefix + "/redeem",
        { code }, true);
    } catch (e) {
      if (isCapabilityDisabled(e)) {
        return null;
      }
      throw e;
    }
  }
}
