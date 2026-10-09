// Courier Cocos SDK 门面(unity CourierClient 同构):装配生命周期状态机 +
// ApiClient + 身份/会话,域服务随批次挂入 services(与 unity CourierServices 门面对齐)。
// 依赖方向 service→core;core 零平台引用(平台差异收敛在 Transport/TokenStore)。
import { ApiClient, type ApiClientOptions } from "./apiClient.ts";
import { IdentityService } from "./identity.ts";
import { LifecycleMachine, LifecycleTrigger, LifecycleState } from "./lifecycle.ts";
import { SessionService } from "./session.ts";
import { MemoryTokenStore, type TokenStore } from "./tokenStore.ts";
import type { CredentialsRequest, CourierConfig, GuestRequest, SessionDto } from "./types.ts";

export interface CourierClientOptions {
  readonly config: CourierConfig;
  readonly transport: ApiClientOptions["transport"];
  readonly tokenStore?: TokenStore;
  readonly retry?: ApiClientOptions["retry"];
  readonly sleep?: ApiClientOptions["sleep"];
}

export class CourierClient {
  readonly config: CourierConfig;
  readonly lifecycle: LifecycleMachine;
  readonly api: ApiClient;
  readonly identity: IdentityService;
  readonly session: SessionService;

  constructor(options: CourierClientOptions) {
    if (!options.config.gameId.trim() || !options.config.env.trim()) {
      throw new Error("courier: gameId/env 必填(scope 头,契约 scope.md)");
    }
    this.config = options.config;
    this.lifecycle = new LifecycleMachine();
    const store: TokenStore = options.tokenStore ?? new MemoryTokenStore();
    // 两段装配:session 先建(api 的 tokenProvider/refreshHook 回调它),
    // api 建成后回填——环只能延迟接线。
    this.session = new SessionService(store, this.lifecycle);
    this.api = new ApiClient({
      config: options.config,
      transport: options.transport,
      retry: options.retry,
      sleep: options.sleep,
      accessTokenProvider: () => this.session.accessToken(),
      refreshHook: () => this.session.refresh(),
    });
    this.session.attachApi(this.api);
    this.identity = new IdentityService(this.api, (s) => this.session.adopt(s));
  }

  /** 初始化(Uninitialized→Initializing→Ready):校验配置后推进状态机。 */
  init(): this {
    if (!this.config.gameId.trim() || !this.config.env.trim()) {
      throw new Error("courier: gameId/env 必填(scope 头,契约 scope.md)");
    }
    this.lifecycle.fire(LifecycleTrigger.InitStarted);
    this.lifecycle.fire(LifecycleTrigger.InitCompleted);
    return this;
  }

  /** 游客登录完整流(Ready/SignedOut → Authenticating → PlayerReady)。 */
  async guestLoginAsync(request: GuestRequest): Promise<SessionDto> {
    return await this.authFlow(() => this.identity.guest(request));
  }

  /** 邮箱+密码登录完整流。 */
  async loginAsync(request: CredentialsRequest): Promise<SessionDto> {
    return await this.authFlow(() => this.identity.login(request));
  }

  /** 邮箱+密码注册并登录完整流。 */
  async registerAsync(request: CredentialsRequest): Promise<SessionDto> {
    return await this.authFlow(() => this.identity.register(request));
  }

  // 认证状态流(unity LoginAsync 同构):守卫 Ready/SignedOut 才可发起;
  // AuthFailed 只在仍处 Authenticating 时回退(成功后失败不存在)。
  private async authFlow(send: () => Promise<SessionDto>): Promise<SessionDto> {
    const state = this.lifecycle.current;
    if (state !== LifecycleState.Ready && state !== LifecycleState.SignedOut) {
      throw new Error("courier: login requires state Ready/SignedOut, current: " + state);
    }
    this.lifecycle.fire(LifecycleTrigger.AuthStarted);
    try {
      const session = await send();
      this.lifecycle.fire(LifecycleTrigger.AuthSucceeded);
      this.lifecycle.fire(LifecycleTrigger.EnterPlayerReady);
      return session;
    } catch (e) {
      if (this.lifecycle.current === LifecycleState.Authenticating) {
        this.lifecycle.fire(LifecycleTrigger.AuthFailed);
      }
      throw e;
    }
  }
}
