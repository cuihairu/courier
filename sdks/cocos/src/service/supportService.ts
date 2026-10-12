// M2 客服域客户端(契约 support.md:玩家侧工单/消息/FAQ)。
// 坐席回复的实时感知走 messages 通道(见 SseParser);通道关闭 → 拉取详情兜底。
import type { ApiClient } from "../core/apiClient.ts";
import type { Page } from "../contract/envelope.ts";
import type {
  AttachmentUploadRequest,
  AttachmentUploadResult,
  CreateTicketRequest,
  FaqDto,
  TicketDetailDto,
  TicketDto,
  TicketMessageDto,
} from "./serviceDto.ts";

const prefix = "/v1/support/";

export class SupportService {
  private readonly api: ApiClient;

  constructor(api: ApiClient) {
    this.api = api;
  }

  /** 提单(category 可空 = OTHER);限流超限 typed RATE_LIMITED。 */
  async createTicketAsync(request: CreateTicketRequest): Promise<TicketDto> {
    return await this.api.request<TicketDto>("POST", prefix + "tickets", request, true);
  }

  /** 我的工单列表(updatedAt 倒序)。 */
  async listTicketsAsync(limit: number, cursor?: string): Promise<Page<TicketDto>> {
    let path = prefix + "tickets?limit=" + limit;
    if (cursor) {
      path += "&cursor=" + encodeURIComponent(cursor);
    }
    return await this.api.request<Page<TicketDto>>("GET", path, undefined, true);
  }

  /** 工单详情(ticket + messages 升序);非本人 → typed SUPPORT_TICKET_NOT_FOUND。 */
  async getTicketAsync(ticketId: string): Promise<TicketDetailDto> {
    return await this.api.request<TicketDetailDto>("GET",
      prefix + "tickets/" + encodeURIComponent(ticketId), undefined, true);
  }

  /** 追加玩家消息;工单 CLOSED → typed SUPPORT_TICKET_CLOSED(409,不重试)。 */
  async appendMessageAsync(ticketId: string, body: string): Promise<TicketMessageDto> {
    return await this.api.request<TicketMessageDto>("POST",
      prefix + "tickets/" + encodeURIComponent(ticketId) + "/messages", { body }, true);
  }

  /** 申请附件直传(v1.1):返回短时效预签名 URL,客户端 PUT 直传存储;
   *  超限 409 SUPPORT_ATTACHMENT_LIMIT、超限体积 413 SUPPORT_ATTACHMENT_TOO_LARGE、
   *  存储未配置 501 COMMON_CAPABILITY_DISABLED。 */
  async requestAttachmentUploadAsync(ticketId: string, request: AttachmentUploadRequest):
    Promise<AttachmentUploadResult> {
    return await this.api.request<AttachmentUploadResult>("POST",
      prefix + "tickets/" + encodeURIComponent(ticketId) + "/attachments", request, true);
  }

  /** FAQ 检索(关键词命中为空是常态,不是错误)。 */
  async faqAsync(keyword: string | undefined, limit: number): Promise<Page<FaqDto>> {
    let path = prefix + "faq?limit=" + limit;
    if (keyword) {
      path += "&keyword=" + encodeURIComponent(keyword);
    }
    return await this.api.request<Page<FaqDto>>("GET", path, undefined, true);
  }
}
