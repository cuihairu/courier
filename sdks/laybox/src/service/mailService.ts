// F62 邮件域客户端(契约 mail.md Frozen v1)。玩家侧三端点全 Bearer;
// 邮件只是附件载体(发放走接入方钩子);mail.updated 推送为提示,列表为准。
import type { ApiClient } from "../core/apiClient.ts";
import type { Page } from "../contract/envelope.ts";
import { isCapabilityDisabled } from "../core/courierError.ts";

const prefix = "/v1/mail";

/** 附件(不透明物品描述;无第二价值单位)。 */
export interface MailAttachmentDto {
  readonly id: string;
  readonly type: string;
  readonly amount: number;
  readonly currency?: string;
}

/** 邮件(列表/详情;createdAt 倒序)。 */
export interface MailDto {
  readonly id: string;
  readonly title: string;
  readonly body: string;
  readonly attachments: readonly MailAttachmentDto[];
  readonly createdAt: string;
  readonly expiresAt?: string;
}

export class MailService {
  private readonly api: ApiClient;

  constructor(api: ApiClient) {
    this.api = api;
  }

  /** 我的邮件列表(个人+全量,最新在前;501 → null)。 */
  async listAsync(limit = 20, cursor?: string): Promise<Page<MailDto> | null> {
    let path = prefix + "?limit=" + limit;
    if (cursor) {
      path += "&cursor=" + encodeURIComponent(cursor);
    }
    return await this.sendOrDisabled<Page<MailDto>>("GET", path);
  }

  /** 邮件详情;不存在/非本人 → typed MAIL_NOT_FOUND。 */
  async getAsync(mailId: string): Promise<MailDto | null> {
    return await this.sendOrDisabled<MailDto>("GET",
      prefix + "/" + encodeURIComponent(mailId));
  }

  /** 领取附件(幂等:重复领取同结果,不二次发放)。 */
  async claimAsync(mailId: string): Promise<MailDto | null> {
    return await this.sendOrDisabled<MailDto>("POST",
      prefix + "/" + encodeURIComponent(mailId) + "/claim", {});
  }

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
