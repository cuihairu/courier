// M2 后段实名域客户端(契约 realname.md Frozen v1;consent 组 v1.1,F31)。
// 红线落地:姓名/证件号只经 submitAsync 传输一次,SDK 不缓存、不落盘、不进日志;
// consent 的出生日期同款红线(只经 declareAge 传输一次,UI 展示仅年份);
// playtime-report 与 consent/s2s/* 是 S2S 端点(F30/v1.1),SDK 不封装——接入方服务端直调。
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

/** 同意状态(consent v1.1 六态:UNDECLARED/ADULT/PENDING_PARENT/GRANTED/DENIED/REVOKED;
 *  时间戳字段只在对应态下发,判定由服务端下发,客户端不得自行推断)。 */
export interface ConsentStatusDto {
  readonly state: string;
  readonly declaredAt?: string;
  readonly grantedAt?: string;
  readonly revokedAt?: string;
  readonly requestExpiresAt?: string;
}

/** VPC 家长验证请求回执(PENDING_PARENT 期间幂等:未过期返回同一 requestId)。 */
export interface ConsentRequestDto {
  readonly requestId: string;
  readonly requestExpiresAt: string;
}

// consent 域专用降级:501 能力关闭按契约转「未启用」(null),不走异常路径。
async function consentOrDisabled<T>(api: ApiClient, method: string, path: string, body?: unknown):
  Promise<T | null> {
  try {
    return await api.request<T>(method, path, body, true);
  } catch (e) {
    if (isCapabilityDisabled(e)) {
      return null;
    }
    throw e; // 其余错误(参数/限流/网络)照常抛给调用方分支
  }
}

/** RealName.Consent 年龄门/家长同意(契约 v1.1,F31;挂载 /v1/realname/consent/*)。
 *  默认关闭:501 → null,接入方隐藏年龄门 UI,不进报错路径(同实名域 v1)。
 *  OnConsentChanged 回调预留:消费 SSE realname.consent.updated(同 config.updated
 *  帧形状),事件分发在接入方,SDK 不内置订阅;REVOKED 到达后的诊断停发联动为
 *  diagnostics.md 预留位。 */
export class ConsentService {
  private readonly api: ApiClient;

  constructor(api: ApiClient) {
    this.api = api;
  }

  /** 同意状态查询。能力未开启 → null。 */
  async statusAsync(): Promise<ConsentStatusDto | null> {
    return await consentOrDisabled<ConsentStatusDto>(this.api, "GET", prefix + "/consent/status");
  }

  /** 年龄申报(响应即判定;防回拨:重复申报仅趋严生效)。
   *  红线:birthDate 只在本调用传输一次,SDK 不缓存、不落盘、不进诊断采集。 */
  async declareAgeAsync(birthDate: string): Promise<ConsentStatusDto | null> {
    return await consentOrDisabled<ConsentStatusDto>(this.api, "POST",
      prefix + "/consent/declare-age", { birthDate });
  }

  /** 发起 VPC 家长同意请求(仅 PENDING_PARENT 态可发起;请求期内幂等)。 */
  async requestParentConsentAsync(): Promise<ConsentRequestDto | null> {
    return await consentOrDisabled<ConsentRequestDto>(this.api, "POST",
      prefix + "/consent/parent-consent/request", {});
  }
}

export class RealNameService {
  private readonly api: ApiClient;

  /** 嵌套 consent 面(契约 v1.1 投影 RealName.Consent)。 */
  readonly consent: ConsentService;

  constructor(api: ApiClient) {
    this.api = api;
    this.consent = new ConsentService(api);
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
