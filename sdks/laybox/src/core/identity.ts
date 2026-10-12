// L2 Core 身份域(契约 auth.md F1–F4 + v1.1 注销)。register/login/guest 匿名,bind 需 Bearer;
// 成功响应一律是新会话(bind 除外:返回账号)——经 onSession 交还持有方落库。
// restore(冷启动恢复)、refresh 轮换、登出吊销归 SessionService。
import type { ApiClient } from "./apiClient.ts";
import type {
  AccountDto,
  BindRequest,
  CredentialsRequest,
  DeleteAccountResult,
  GuestRequest,
  SessionDto,
} from "./types.ts";

const prefix = "/v1/identity/";

export class IdentityService {
  private readonly api: ApiClient;
  private readonly onSession: (session: SessionDto) => void;

  constructor(api: ApiClient, onSession: (session: SessionDto) => void) {
    this.api = api;
    this.onSession = onSession;
  }

  /** 邮箱+密码注册并登录(F1)。 */
  async register(request: CredentialsRequest): Promise<SessionDto> {
    return await this.send<SessionDto>("POST", "register", request, false);
  }

  /** 邮箱+密码登录(F2)。 */
  async login(request: CredentialsRequest): Promise<SessionDto> {
    return await this.send<SessionDto>("POST", "login", request, false);
  }

  /** 游客登录(F3,按设备幂等:同设备回到同一游客账号)。 */
  async guest(request: GuestRequest): Promise<SessionDto> {
    return await this.send<SessionDto>("POST", "guest", request, false);
  }

  /** 游客账号绑定邮箱(转正,F4;Bearer;返回转正后账号)。 */
  async bind(request: BindRequest): Promise<AccountDto> {
    return await this.send<AccountDto>("POST", "bind", request, true);
  }

  /** refresh 轮换(F5,匿名;响应为轮换后的新会话)。 */
  async refresh(refreshToken: string): Promise<SessionDto> {
    return await this.send<SessionDto>("POST", "refresh", { refreshToken }, false);
  }

  /** 注销账号(v1.1):受理即吊销全部会话。EMAIL 走宽限期(响应 PENDING_DELETE + 一次性
   *  cancelToken),GUEST 立即删(响应 DELETED + deletedAt);宽限期内登录被拒。 */
  async deleteAccount(): Promise<DeleteAccountResult> {
    return await this.api.request<DeleteAccountResult>(
      "POST", prefix + "account/delete", undefined, true);
  }

  /** 宽限期内撤销注销(v1.1,匿名):cancelToken 一次性;已吊销会话不恢复,需重登。 */
  async cancelDelete(cancelToken: string): Promise<void> {
    await this.api.request<unknown>(
      "POST", prefix + "account/delete/cancel", { cancelToken }, false);
  }

  private async send<T>(method: string, action: string, body: unknown,
    withAuth: boolean): Promise<T> {
    const data = await this.api.request<T>(method, prefix + action, body, withAuth);
    if (data instanceof Object && "accessToken" in (data as object)) {
      this.onSession(data as unknown as SessionDto);
    }
    return data;
  }
}
