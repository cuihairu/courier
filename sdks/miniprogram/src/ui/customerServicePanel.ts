// UI 包客服页面板骨架(unity CustomerServicePanel 同构)。
// 会话流:提单 → 追加消息 → 坐席回复感知。
// 实时感知:游戏侧把 messages 通道(SseParser 解析出 support.ticket_replied)
// 接到 notifyTicketReplied → 面板重拉详情;通道不可用时宿主可不接,
// 面板仍有轮询/手动刷新兜底(契约拉取兜底语义)。
import type { TicketDetailDto } from "../service/serviceDto.ts";
import type { CourierServices } from "../service/courierServices.ts";
import { getSupportLabel, tryGetString } from "./brandingCatalog.ts";
import { wireOf } from "./announcementPanel.ts";

export const CustomerServicePanelDefaultTitle = "客服";

export class CustomerServicePanel {
  /** 渲染出口:详情(工单 + 消息升序)交游戏侧控件。 */
  onDetailRendered: ((detail: TicketDetailDto) => void) | null = null;
  /** 错误出口:typed wire code 交游戏侧提示(RATE_LIMITED/CLOSED 等分支)。 */
  onError: ((wireCode: string) => void) | null = null;

  brandTitle = CustomerServicePanelDefaultTitle;

  private currentTicketId: string | null = null;

  private readonly services?: CourierServices;

  constructor(services?: CourierServices) {
    this.services = services;
  }

  /** 当前面板关注的工单(实时重拉判断用)。 */
  getCurrentTicketId(): string | null {
    return this.currentTicketId;
  }

  /** 品牌兜底链标题:远端 productName → 宿主 brandTitle → 内置默认。 */
  resolveTitle(): string {
    const remote = tryGetString("productName");
    return remote != null && remote !== "" ? remote : this.brandTitle;
  }

  /** 客服入口文案:branding → Courier 默认标。 */
  get supportEntryLabel(): string {
    return getSupportLabel();
  }

  /** 提单并打开。category 可空(服务端缺省 OTHER);限流 → RATE_LIMITED 出错误口。 */
  async openTicketAsync(title: string, body: string, category?: string): Promise<void> {
    if (this.services == null) {
      return;
    }
    try {
      const ticket = await this.services.support.createTicketAsync({
        title, body, category,
      });
      await this.loadTicketAsync(ticket.id);
    } catch (e) {
      this.onError?.(wireOf(e));
    }
  }

  /** 拉取工单详情并渲染(手动刷新/轮询兜底走这里);非本人 → 404 出错误口。 */
  async loadTicketAsync(ticketId: string): Promise<void> {
    if (this.services == null) {
      return;
    }
    this.currentTicketId = ticketId;
    try {
      const detail = await this.services.support.getTicketAsync(ticketId);
      this.onDetailRendered?.(detail);
    } catch (e) {
      this.onError?.(wireOf(e));
    }
  }

  /** 玩家追加消息,成功后重拉详情。CLOSED 工单 → SUPPORT_TICKET_CLOSED 出错误口。 */
  async sendMessageAsync(body: string): Promise<void> {
    if (this.services == null || this.currentTicketId == null) {
      return;
    }
    try {
      await this.services.support.appendMessageAsync(this.currentTicketId, body);
      await this.loadTicketAsync(this.currentTicketId);
    } catch (e) {
      this.onError?.(wireOf(e));
    }
  }

  /** FAQ 检索(关键词为空 = 热门问题);空结果是常态,返回「无匹配」文案。 */
  async searchFaqAsync(keyword?: string): Promise<string> {
    if (this.services == null) {
      return "";
    }
    try {
      const page = await this.services.support.faqAsync(keyword, 10);
      if (page.items.length === 0) {
        return "(无匹配问题)";
      }
      let text = "";
      for (const faq of page.items) {
        text += "Q: " + faq.question + "\nA: " + faq.answer + "\n";
      }
      return text;
    } catch (e) {
      this.onError?.(wireOf(e));
      return "";
    }
  }

  /** messages 通道回调入口:游戏侧解析到 support.ticket_replied 时调用,
   *  面板自动重拉当前工单(拉取兜底语义的实时面)。 */
  async notifyTicketReplied(ticketId: string): Promise<void> {
    if (ticketId !== this.currentTicketId || this.services == null) {
      return;
    }
    await this.loadTicketAsync(ticketId);
  }
}
