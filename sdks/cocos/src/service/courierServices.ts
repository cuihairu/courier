// 服务层门面(unity CourierServices 同构):从 CourierClient 取 ApiClient 装配域服务。
// 域服务随批次挂入;依赖方向 service→core,core 不感知本层。
import type { CourierClient } from "../core/courierClient.ts";
import { AnnouncementService } from "./announcementService.ts";
import { SupportService } from "./supportService.ts";

export class CourierServices {
  readonly announcements: AnnouncementService;
  readonly support: SupportService;

  constructor(client: CourierClient) {
    this.announcements = new AnnouncementService(client.api);
    this.support = new SupportService(client.api);
  }
}
