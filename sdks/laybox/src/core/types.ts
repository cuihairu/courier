// L2 Core 共享类型(契约 auth.md / primitives.md;wire camelCase,时间 RFC3339 ms UTC)。
// DTO 以 ../../docs/contract/ 为唯一事实源;nullable = 契约可选字段缺省。

/** 连接配置(endpoint 不带尾斜杠也可,ApiClient 归一)。 */
export interface CourierConfig {
  readonly endpoint: string;
  readonly gameId: string;
  readonly env: string;
  readonly language?: string; // 玩家 locale(RFC 5646,F76):公告列表自动附带 language 参数;缺省不附带
}

/** 账号(契约 auth.md 数据模型;GUEST 时 email 缺省)。 */
export interface AccountDto {
  readonly id: string;
  readonly type: string; // EMAIL / GUEST
  readonly email?: string;
  readonly status: string; // ACTIVE / DISABLED / PENDING_DELETE / DELETED(v1.1)
  readonly createdAt: string;
}

/** 会话(登录/轮换响应 data)。accountId 不上线(网关不下发),切号检测取 account.id。 */
export interface SessionDto {
  readonly account?: AccountDto;
  readonly accessToken: string;
  readonly accessExpiresAt: string;
  readonly refreshToken: string;
  readonly refreshExpiresAt: string;
  readonly deviceId: string;
}

/** GET /v1/identity/session(当前会话信息)。 */
export interface SessionInfoDto {
  readonly account?: AccountDto;
  readonly sessionId: string;
  readonly deviceId: string;
}

/** 已绑定设备(GET /v1/identity/devices 分页项;id 为服务端行 ID,platform 可选)。 */
export interface DeviceDto {
  readonly id: string;
  readonly deviceId: string;
  readonly platform?: string;
  readonly createdAt: string;
}

export interface DeviceListDto {
  readonly items: readonly DeviceDto[];
  readonly nextCursor?: string;
}

/** POST /v1/identity/account/delete 响应(v1.1):PENDING_DELETE 带一次性 cancelToken;
 *  DELETED(立即删)带 deletedAt。 */
export interface DeleteAccountResult {
  readonly status: string; // PENDING_DELETE / DELETED
  readonly cancelToken?: string;
  readonly pendingDelete?: boolean;
  readonly deletedAt?: string;
}

/** 注册/登录请求(契约 auth.md F1/F2)。 */
export interface CredentialsRequest {
  readonly email: string;
  readonly password: string;
}

/** 游客登录请求(按设备幂等)。 */
export interface GuestRequest {
  readonly deviceId: string;
  readonly platform?: string;
}

/** 绑定邮箱请求(游客转正;Bearer)。 */
export interface BindRequest {
  readonly email: string;
  readonly password: string;
}

/** 第三方凭证换会话请求(契约 oauth.md v1.1:SDK 不内置第三方 SDK,凭证由接入方
 *  自行集成取得;凭证只发一次,不本地缓存;tokenType 缺省 ID_TOKEN)。 */
export interface OAuthExchangeRequest {
  readonly provider: string;
  readonly token: string;
  readonly tokenType?: "ID_TOKEN" | "ACCESS_TOKEN" | "CODE";
  readonly deviceId?: string;
  readonly platform?: string;
}

/** 已绑定第三方身份(契约 oauth.md GET bindings:不返回 subject,防客户端侧泄露)。 */
export interface OAuthBindingDto {
  readonly provider: string;
  readonly createdAt: string;
}

export interface OAuthBindingListDto {
  readonly items: readonly OAuthBindingDto[];
  readonly nextCursor?: string;
}
