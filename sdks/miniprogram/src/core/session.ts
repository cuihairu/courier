// L2 Core 会话域(契约 auth.md F5–F8):冷启动恢复、refresh 轮换(单飞)、
// 登出吊销、设备列表。安全事件(REFRESH_REUSED/TOKEN_REVOKED)→ 本地清场
// (整个会话已被服务端吊销,契约 auth.md「重放检测」)。
import type { ApiClient } from "./apiClient.ts";
import type { LifecycleMachine } from "./lifecycle.ts";
import type { TokenStore } from "./tokenStore.ts";
import type { DeviceListDto, SessionDto, SessionInfoDto } from "./types.ts";

export class SessionService {
  // api 由 CourierClient 两段装配回填(session 先建、api 后建——api 的
  // accessTokenProvider/refreshHook 又回调本服务,天然成环,只能延迟接线)。
  private api: ApiClient | null = null;
  private readonly store: TokenStore;
  private readonly lifecycle: LifecycleMachine;
  private refreshInFlight: Promise<boolean> | null = null;

  constructor(store: TokenStore, lifecycle: LifecycleMachine) {
    this.store = store;
    this.lifecycle = lifecycle;
  }

  /** CourierClient 装配回填(内部接线点,接入方不经此构造)。 */
  attachApi(api: ApiClient): void {
    this.api = api;
  }

  private requireApi(): ApiClient {
    if (!this.api) {
      throw new Error("courier: SessionService 未接线(须经理由 CourierClient 构造)");
    }
    return this.api;
  }

  /** 当前会话(冷启动时尝试从 store 恢复一次;null = 未认证)。 */
  current(): SessionDto | null {
    return this.store.load();
  }

  /** 当前 access token(轮换后自动为新值;null = 未认证)。 */
  accessToken(): string | null {
    return this.store.load()?.accessToken ?? null;
  }

  /** 落库新会话(登录/轮换成功后由 IdentityService 经 onSession 调用)。 */
  adopt(session: SessionDto): void {
    this.store.save(session);
    this.lifecycle.reportAccountId(session.accountId); // 切号检测(events.md account_switched)
  }

  /** 本地清场(登出成功/安全事件后)。 */
  clear(): void {
    this.store.clear();
  }

  /** refresh 轮换(单飞:并发调用共享一次请求)。返回是否拿到新凭证。 */
  refresh(): Promise<boolean> {
    if (this.refreshInFlight) {
      return this.refreshInFlight;
    }
    this.refreshInFlight = this.tryRefresh().finally(() => {
      this.refreshInFlight = null;
    });
    return this.refreshInFlight;
  }

  private async tryRefresh(): Promise<boolean> {
    const current = this.store.load();
    if (!current) {
      return false;
    }
    this.lifecycle.raiseTokenExpired(); // 契约事件:自动 refresh 开始(状态不变)
    try {
      const dto = await this.requireApi().request<SessionDto>("POST", "/v1/identity/refresh",
        { refreshToken: current.refreshToken }, false);
      this.store.save(dto);
      return true;
    } catch (e) {
      const wire = e instanceof Error && "wire" in e ? String((e as { wire: unknown }).wire) : "";
      if (wire === "AUTH_REFRESH_REUSED" || wire === "AUTH_TOKEN_REVOKED") {
        // 安全事件:本地清场 + 生命周期登出(重登;契约 auth.md「重放检测」)。
        this.store.clear();
        this.lifecycle.tryFire("SignedOut");
        return false;
      }
      throw e;
    }
  }

  /** 登出:吊销当前会话(F6,Bearer)+ 本地清场;服务端失败仍清本地(尽力而为)。 */
  async logout(): Promise<void> {
    try {
      await this.requireApi().request<null>("POST", "/v1/identity/logout", undefined, true);
    } catch {
      // 吊销失败不阻塞登出(本地清场为准;下次请求自然 401)。
    } finally {
      this.store.clear();
      this.lifecycle.tryFire("SignedOut");
    }
  }

  /** 当前会话信息(F7)。 */
  async info(): Promise<SessionInfoDto> {
    return await this.requireApi().request<SessionInfoDto>("GET", "/v1/identity/session", undefined, true);
  }

  /** 已绑定设备列表(F8)。 */
  async listDevices(): Promise<DeviceListDto> {
    return await this.requireApi().request<DeviceListDto>("GET", "/v1/identity/devices", undefined, true);
  }

  /** 解绑设备并吊销其全部会话(F9;deviceId 需 URL 编码)。 */
  async unbindDevice(deviceId: string): Promise<null> {
    return await this.requireApi().request<null>("DELETE",
      "/v1/identity/devices/" + encodeURIComponent(deviceId), undefined, true);
  }
}
