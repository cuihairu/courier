// Courier Cocos SDK 门面(unity CourierClient 同构):装配 ApiClient + 身份/会话,
// 域服务随批次挂入 services(与 unity CourierServices 门面对齐)。
// 依赖方向 service→core;core 零平台引用(平台差异收敛在 Transport/TokenStore)。
import { ApiClient, type ApiClientOptions } from "./apiClient.ts";
import { IdentityService } from "./identity.ts";
import { SessionService } from "./session.ts";
import { MemoryTokenStore, type TokenStore } from "./tokenStore.ts";
import type { CourierConfig } from "./types.ts";

export interface CourierClientOptions {
  readonly config: CourierConfig;
  readonly transport: ApiClientOptions["transport"];
  readonly tokenStore?: TokenStore;
  readonly retry?: ApiClientOptions["retry"];
  readonly sleep?: ApiClientOptions["sleep"];
}

export class CourierClient {
  readonly config: CourierConfig;
  readonly api: ApiClient;
  readonly identity: IdentityService;
  readonly session: SessionService;

  constructor(options: CourierClientOptions) {
    if (!options.config.gameId.trim() || !options.config.env.trim()) {
      throw new Error("courier: gameId/env 必填(scope 头,契约 scope.md)");
    }
    this.config = options.config;
    const store: TokenStore = options.tokenStore ?? new MemoryTokenStore();
    // 两段装配:session 先建(api 的 tokenProvider/refreshHook 回调它),
    // api 建成后回填——环只能延迟接线。
    this.session = new SessionService(store);
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
}
