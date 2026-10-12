// M2 公告域客户端(契约 announcement.md:玩家侧只读投影)。
// 列表/详情经 ApiClient(scope 头、Bearer、信封、重试全部归 L2)。
import type { ApiClient } from "../core/apiClient.ts";
import type { Page } from "../contract/envelope.ts";
import type { AnnouncementDto, AnnouncementQuery } from "./serviceDto.ts";

const prefix = "/v1/announcements";

export class AnnouncementService {
  private readonly api: ApiClient;
  private readonly defaultLanguage: string;

  constructor(api: ApiClient, defaultLanguage?: string) {
    this.api = api;
    this.defaultLanguage = defaultLanguage ?? "";
  }

  /** 可见公告列表(分页;limit 默认 20 最大 100,游标回传 nextCursor)。
   *  query 为 v1.1 定向参数:缺省维度不过滤,详情被过滤出局 → typed ANNOUNCEMENT_NOT_FOUND。 */
  async listAsync(limit: number, cursor?: string,
    query?: AnnouncementQuery): Promise<Page<AnnouncementDto>> {
    let path = prefix + "?limit=" + limit;
    if (cursor) {
      path += "&cursor=" + encodeURIComponent(cursor);
    }
    if (query) {
      for (const key of ["platform", "appVersion", "region"] as const) {
        const value = query[key];
        if (value) {
          path += "&" + key + "=" + encodeURIComponent(value);
        }
      }
    }
    // F76 多语内容:配置 locale 自动附带 language(仅列表);显式 query.language 覆写,
    // 配置缺省不附带(与 v1 行为一致)。行内 language 元数据不参与匹配。
    const language = query?.language ?? this.defaultLanguage;
    if (language) {
      path += "&language=" + encodeURIComponent(language);
    }
    return await this.api.request<Page<AnnouncementDto>>("GET", path, undefined, true);
  }

  /** 公告详情;不可见/不存在 → typed ANNOUNCEMENT_NOT_FOUND(404,不重试)。 */
  async getAsync(announcementId: string): Promise<AnnouncementDto> {
    return await this.api.request<AnnouncementDto>("GET",
      prefix + "/" + encodeURIComponent(announcementId), undefined, true);
  }
}
