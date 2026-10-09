// M2 后段实名域客户端(契约 realname.md Frozen v1)。
// 红线落地:姓名/证件号只经 submitAsync 传输一次,SDK 不缓存、不落盘、不进日志;
// playtime-report 是 S2S 端点(F30),SDK 不封装——接入方服务端直调。
import type { ApiClient } from "../core/apiClient.ts";
import { isCapabilityDisabled } from "../core/courierError.ts";

const prefix = "/v1/realname";

/** 实名状态(UNVERIFIED/PENDING_REVIEW/VERIFIED/REJECTED;isMinor 仅 VERIFIED 后下发)。 */
export interface RealNameStatusDto {
  readonly state: string;
  readonly isMinor?: boolean;
  readonly verifiedAt?: string;
}

/** 可玩时段查询(F28):playable=false 时 nextWindowAt 给下一窗口。 */
export interface RealNameCurfewDto {
  readonly playable: boolean;
  readonly nextWindowAt?: string;
}

/** 充值额度校验(F29):限额字段 0 = 未下发。 */
export interface RealNameChargeDto {
  readonly allowed: boolean;
  readonly singleLimitCents?: number;
  readonly monthlyLimitCents?: number;
  readonly monthlyUsedCents?: number;
}

export class RealNameService {
  private readonly api: ApiClient;

  constructor(api: ApiClient) {
    this.api = api;
  }

  /** 提交核验(F27)。REJECTED/PENDING_REVIEW 也是有效提交结果,不是异常。
   *  能力未开启(501)→ null,调用方隐藏实名 UI,不进报错路径。 */
  async submitAsync(name: string, idNumber: string): Promise<RealNameStatusDto | null> {
    return await this.sendOrDisabled<RealNameStatusDto>("POST", prefix + "/verify",
      { name, idNumber });
  }

  /** 状态查询(F27)。能力未开启 → null。 */
  async statusAsync(): Promise<RealNameStatusDto | null> {
    return await this.sendOrDisabled<RealNameStatusDto>("GET", prefix + "/status");
  }

  /** 可玩时段查询(F28)。能力未开启 → null(接入方自行决定是否限制)。 */
  async curfewAsync(): Promise<RealNameCurfewDto | null> {
    return await this.sendOrDisabled<RealNameCurfewDto>("GET", prefix + "/curfew");
  }

  /** 充值额度校验(F29):支付下单前的前置校验;能力未开启 → null(不拦支付,由接入方决定)。 */
  async chargeCheckAsync(amountCents: number): Promise<RealNameChargeDto | null> {
    return await this.sendOrDisabled<RealNameChargeDto>("POST", prefix + "/charge-check",
      { amountCents });
  }

  // 实名域专用降级:501 能力关闭按契约转「未启用」(null),不走异常路径。
  private async sendOrDisabled<T>(method: string, path: string, body?: unknown):
    Promise<T | null> {
    try {
      return await this.api.request<T>(method, path, body, true);
    } catch (e) {
      if (isCapabilityDisabled(e)) {
        return null;
      }
      throw e; // 其余错误(参数/限流/网络)照常抛给调用方分支
    }
  }
}
