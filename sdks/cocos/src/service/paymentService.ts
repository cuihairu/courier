// M5 支付客户端(契约 payment.md Frozen v1.1)。玩家侧四端点全 Bearer;
// POST /v1/payments/callback 为 S2S 渠道回调(HMAC 签名即认证),不经客户端 SDK 封装。
// 下单只收 skuId(服务端定价红线:客户端不下发/不计算金额)。
// v1.1 发货感知:推送为提示(payment.paid/delivered 载荷只含 orderId)+ 轮询为准。
import type { ApiClient } from "../core/apiClient.ts";
import type { Page } from "../contract/envelope.ts";
import { isCapabilityDisabled } from "../core/courierError.ts";

const prefix = "/v1/payments";

/** 可购 SKU(服务端价格表投影;金额原样展示,客户端不做金额计算)。 */
export interface PaymentSkuDto {
  readonly id: string;
  readonly productId: string;
  readonly form: string;
  readonly amountCents: number;
  readonly currency: string;
}

/** 订单快照。payToken 仅下单响应出现一次(消费归渠道 SDK,SDK 不解释);
 *  详情/列表不下发。发货感知 = 轮询 status 到 DELIVERED。 */
export interface PaymentOrderDto {
  readonly id: string;
  readonly status: string; // CREATED / PAID / DELIVERED / CLOSED
  readonly skuId: string;
  readonly productId: string;
  readonly form: string;
  readonly amountCents: number;
  readonly currency: string;
  readonly payToken?: string;
  readonly createdAt: string;
  readonly updatedAt: string;
  readonly paidAt?: string;
  readonly deliveredAt?: string;
}

export class PaymentService {
  private readonly api: ApiClient;

  constructor(api: ApiClient) {
    this.api = api;
  }

  /** 可购 SKU 列表(价格表投影;金额原样展示)。 */
  async getSkusAsync(): Promise<Page<PaymentSkuDto> | null> {
    return await this.sendOrDisabled<Page<PaymentSkuDto>>("GET", prefix + "/skus");
  }

  /** 下单:服务端按 skuId 定价 → CREATED + payToken(仅本次响应出现)。 */
  async createOrderAsync(skuId: string): Promise<PaymentOrderDto | null> {
    return await this.sendOrDisabled<PaymentOrderDto>("POST", prefix + "/orders", { skuId });
  }

  /** 我的订单(updatedAt 倒序;不含 payToken)。 */
  async listOrdersAsync(): Promise<Page<PaymentOrderDto> | null> {
    return await this.sendOrDisabled<Page<PaymentOrderDto>>("GET", prefix + "/orders");
  }

  /** 订单详情(发货感知 = 轮询 status;不属当前玩家 → 404 不泄露存在性)。 */
  async getOrderAsync(orderId: string): Promise<PaymentOrderDto | null> {
    return await this.sendOrDisabled<PaymentOrderDto>("GET",
      prefix + "/orders/" + encodeURIComponent(orderId));
  }

  // 降级语义:501 能力关闭 → null(隐藏商城/充值 UI,不进报错路径);其余错误照常抛。
  private async sendOrDisabled<T>(method: string, path: string, body?: unknown):
    Promise<T | null> {
    try {
      return await this.api.request<T>(method, path, body, true);
    } catch (e) {
      if (isCapabilityDisabled(e)) {
        return null;
      }
      throw e;
    }
  }
}
