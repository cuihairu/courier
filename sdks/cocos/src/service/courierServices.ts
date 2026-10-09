// 服务层门面(unity CourierServices 同构):从 CourierClient 取 ApiClient 装配域服务。
// 依赖方向 service→core,core 不感知本层。
import type { CourierClient } from "../core/courierClient.ts";
import { AnnouncementService } from "./announcementService.ts";
import { SupportService } from "./supportService.ts";
import { AppService } from "./appService.ts";
import { AppConfigService } from "./appConfigService.ts";
import { BrandingService } from "./brandingService.ts";
import { PlayerService } from "./playerService.ts";
import { AssistantService } from "./assistantService.ts";
import { PaymentService } from "./paymentService.ts";
import { RealNameService } from "./realnameService.ts";

export class CourierServices {
  readonly announcements: AnnouncementService;
  readonly support: SupportService;
  readonly app: AppService;
  readonly config: AppConfigService;
  readonly branding: BrandingService;
  readonly player: PlayerService;
  readonly assistant: AssistantService;
  readonly payments: PaymentService;
  readonly realname: RealNameService;

  constructor(client: CourierClient) {
    this.announcements = new AnnouncementService(client.api);
    this.support = new SupportService(client.api);
    this.app = new AppService(client.api);
    this.config = new AppConfigService(client.api);
    this.branding = new BrandingService(client.api);
    this.player = new PlayerService(client.api);
    this.assistant = new AssistantService(client.api);
    this.payments = new PaymentService(client.api);
    this.realname = new RealNameService(client.api);
  }
}
