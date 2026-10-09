// UI 包公告栏面板骨架(unity AnnouncementPanel 同构;todo「公告栏」)。
// 边界:包内只做编排与纯文本产出,渲染经 onListRendered 回调交游戏侧,
// 不绑死特定 UI 框架(Cocos Creator UIRichText/Label/自绘均可挂)。
// 品牌定制:标题走兜底链 远端 productName → 宿主 brandTitle → 内置默认。
import type { AnnouncementDto } from "../service/serviceDto.ts";
import type { CourierServices } from "../service/courierServices.ts";
import { getProductTitle, tryGetString } from "./brandingCatalog.ts";

/** 面板默认标(远端 productName 可覆盖)。 */
export const AnnouncementPanelDefaultTitle = "公告";

export class AnnouncementPanel {
  /** 渲染出口:游戏侧设置后把文本投到目标控件。 */
  onListRendered: ((text: string) => void) | null = null;
  /** 打点出口:点开详情时回调公告 id(埋点归游戏)。 */
  onAnnouncementOpened: ((id: string) => void) | null = null;
  /** 错误出口:typed wire code 交游戏侧提示。 */
  onError: ((wireCode: string) => void) | null = null;

  /** 首页拉取条数(契约默认 20,最大 100)。 */
  pageSize = 20;

  brandTitle: string = AnnouncementPanelDefaultTitle;

  private readonly services?: CourierServices;

  constructor(services?: CourierServices, brandTitle?: string) {
    this.services = services;
    if (brandTitle !== undefined) {
      this.brandTitle = brandTitle;
    }
  }

  /** 拉取可见公告并渲染(下拉刷新也走这里);未装配:面板静默不抛。 */
  async refreshAsync(): Promise<void> {
    if (this.services == null) {
      return; // UI 骨架允许先挂场景后接服务
    }
    try {
      const page = await this.services.announcements.listAsync(this.pageSize);
      this.onListRendered?.(this.format(page.items));
    } catch (e) {
      this.onError?.(wireOf(e));
    }
  }

  /** 查看详情:渲染单条全文并回调 id。 */
  async openAsync(announcementId: string): Promise<void> {
    if (this.services == null) {
      return;
    }
    try {
      const dto = await this.services.announcements.getAsync(announcementId);
      this.onAnnouncementOpened?.(dto.id);
      this.onListRendered?.(this.format([dto], false));
    } catch (e) {
      this.onError?.(wireOf(e));
    }
  }

  /** 品牌兜底链:远端 productName → 宿主 brandTitle → 内置默认。 */
  resolveTitle(): string {
    const remote = tryGetString("productName");
    return remote != null && remote !== "" ? remote : this.brandTitle;
  }

  /** 列表渲染带标题头;详情渲染(详情=全文视图)不带,避免标题重复。 */
  private format(items: readonly AnnouncementDto[], withTitle = true): string {
    if (items.length === 0) {
      return withTitle ? this.resolveTitle() + "\n(暂无公告)" : "(暂无公告)";
    }
    let text = withTitle ? this.resolveTitle() + "\n" : "";
    for (const item of items) {
      text += "[" + item.severity + "] " + item.title + "\n" + item.body;
    }
    return text;
  }
}

/** typed 错误的 wire code(非 Courier 错误落 UNKNOWN)。 */
export function wireOf(e: unknown): string {
  return e instanceof Error && "wire" in e ? String((e as { wire: unknown }).wire) : "UNKNOWN";
}
