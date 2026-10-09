// M2 公告域客户端(契约 announcement.md:玩家侧只读投影)。
// 列表/详情经 ApiClient(scope 头、Bearer、信封、重试全部归 L2)。
import type { ApiClient } from "../core/apiClient.ts";
import type { Page } from "../contract/envelope.ts";
import type { AnnouncementDto } from "./serviceDto.ts";

const prefix = "/v1/announcements";

export class AnnouncementService {
  private readonly api: ApiClient;

  constructor(api: ApiClient) {
    this.api = api;
  }

  /** 可见公告列表(分页;limit 默认 20 最大 100,游标回传 nextCursor)。 */
  async listAsync(limit: number, cursor?: string): Promise<Page<AnnouncementDto>> {
    let path = prefix + "?limit=" + limit;
    if (cursor) {
      path += "&cursor=" + encodeURIComponent(cursor);
    }
    return await this.api.request<Page<AnnouncementDto>>("GET", path, undefined, true);
  }

  /** 公告详情;不可见/不存在 → typed ANNOUNCEMENT_NOT_FOUND(404,不重试)。 */
  async getAsync(announcementId: string): Promise<AnnouncementDto> {
    return await this.api.request<AnnouncementDto>("GET",
      prefix + "/" + encodeURIComponent(announcementId), undefined, true);
  }
}
