// UI 包小助手面板(M4,契约 assistant.md「客户端要求」;unity AssistantPanel 同构)。
// 问答流:提问 → 命中渲染答案原样快照;未命中(非错误)→ 转人工引导:
// 一键经 support 域提单(标题带 [助手] 前缀,截 120),后续坐席回复走客服面板链路。
// 能力未接(501→null)→ onError 出口,UI 隐藏入口。
import type { AssistantQueryDto } from "../service/assistantService.ts";
import type { CourierServices } from "../service/courierServices.ts";
import { tryGetString } from "./brandingCatalog.ts";
import { wireOf } from "./announcementPanel.ts";

export const AssistantPanelDefaultTitle = "助手";

export class AssistantPanel {
  /** 渲染出口:命中时答案条目;未命中时 suggestTransfer=true。 */
  onAnswerRendered: ((result: AssistantQueryDto) => void) | null = null;
  /** 转人工完成:新工单 ID(游戏侧可跳转客服面板)。 */
  onTicketCreated: ((ticketId: string) => void) | null = null;
  /** 错误出口:typed wire code + 能力未接(COMMON_CAPABILITY_DISABLED)。 */
  onError: ((wireCode: string) => void) | null = null;

  brandTitle = AssistantPanelDefaultTitle;

  private readonly services?: CourierServices;

  constructor(services?: CourierServices) {
    this.services = services;
  }

  /** 品牌兜底链标题:远端 productName → 宿主 brandTitle → 内置默认。 */
  resolveTitle(): string {
    const remote = tryGetString("productName");
    return remote != null && remote !== "" ? remote : this.brandTitle;
  }

  /** 提问。未命中不进错误路径:渲染 suggestTransfer=true 的结果(契约「未命中不是错误」)。 */
  async askAsync(text: string): Promise<void> {
    if (this.services == null) {
      return;
    }
    try {
      const result = await this.services.assistant.queryAsync(text);
      if (result == null) {
        this.onError?.("COMMON_CAPABILITY_DISABLED"); // 契约:隐藏入口
        return;
      }
      this.onAnswerRendered?.(result);
    } catch (e) {
      this.onError?.(wireOf(e));
    }
  }

  /** 一键转人工(未命中后调用):经 support 域提单,把玩家问题带进工单。
   *  提单成功后坐席侧照常回复,实时感知复用客服面板链路。 */
  async transferToHumanAsync(question: string): Promise<void> {
    if (this.services == null) {
      return;
    }
    try {
      const ticket = await this.services.support.createTicketAsync({
        title: truncate("[助手] " + question, 120),
        body: question,
      });
      this.onTicketCreated?.(ticket.id);
    } catch (e) {
      this.onError?.(wireOf(e));
    }
  }
}

/// 工单标题上限 120 字符(契约 support.md title 长度)。
function truncate(s: string, max: number): string {
  return s.length <= max ? s : s.slice(0, max);
}
