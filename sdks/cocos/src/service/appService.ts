// M3 应用状态客户端(契约 app.md Frozen v1)。三端点匿名可(维护中仍需可查);
// 501 能力未接 → null(跳过版本/维护检查直接登录,能力安静地不存在)。
// 503 APP_MAINTENANCE / 426 APP_VERSION_UNSUPPORTED 照常抛 typed 错误(信封已登记),
// 调用方据此走维护页/更新引导;SDK 不自动跳转商店。
import type { ApiClient } from "../core/apiClient.ts";
import { isCapabilityDisabled } from "../core/courierError.ts";
import type { AppEnvironmentDto, AppMaintenanceDto, AppVersionDto } from "./appDto.ts";

const prefix = "/v1/app";

export class AppService {
  private readonly api: ApiClient;

  constructor(api: ApiClient) {
    this.api = api;
  }

  /** 版本检查:appVersion 上报参与 forceUpdate 判定(缺省 = 不判定);platform 可选。 */
  async checkUpdateAsync(appVersion?: string, platform?: string): Promise<AppVersionDto | null> {
    const params = new URLSearchParams();
    if (appVersion) params.set("appVersion", appVersion);
    if (platform) params.set("platform", platform);
    const q = params.toString();
    return await this.sendOrDisabled<AppVersionDto>("GET",
      prefix + "/version" + (q ? "?" + q : ""));
  }

  /** 维护状态查询:维护页数据源;estimatedRecoveryAt 供轮询节奏。 */
  async checkMaintenanceAsync(): Promise<AppMaintenanceDto | null> {
    return await this.sendOrDisabled<AppMaintenanceDto>("GET", prefix + "/maintenance");
  }

  /** 环境回显:初始化后校验 scope 与网关一致(排查接错环境)。 */
  async getEnvironmentAsync(): Promise<AppEnvironmentDto | null> {
    return await this.sendOrDisabled<AppEnvironmentDto>("GET", prefix + "/environment");
  }

  // App 域降级:501 能力关闭 → null(未启用态,跳过检查直接登录)。
  private async sendOrDisabled<T>(method: string, path: string): Promise<T | null> {
    try {
      // 本域匿名(withAuth=false):维护中未登录也要可查。
      return await this.api.request<T>(method, path, undefined, false);
    } catch (e) {
      if (isCapabilityDisabled(e)) {
        return null;
      }
      throw e;
    }
  }
}
