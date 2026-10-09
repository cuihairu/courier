// L2 Core 共享类型(契约 auth.md / primitives.md;wire camelCase,时间 RFC3339 ms UTC)。
// DTO 以 ../../docs/contract/ 为唯一事实源;nullable = 契约可选字段缺省。

/** 连接配置(endpoint 不带尾斜杠也可,ApiClient 归一)。 */
export interface CourierConfig {
  readonly endpoint: string;
  readonly gameId: string;
  readonly env: string;
}

/** 账号(契约 auth.md 数据模型;GUEST 时 email 缺省)。 */
export interface AccountDto {
  readonly id: string;
  readonly type: string; // EMAIL / GUEST
  readonly email?: string;
  readonly status: string; // ACTIVE / DISABLED
  readonly createdAt: string;
}

/** 会话(登录/轮换响应 data)。 */
export interface SessionDto {
  readonly accountId: string;
  readonly account?: AccountDto;
  readonly accessToken: string;
  readonly accessExpiresAt: string;
  readonly refreshToken: string;
  readonly refreshExpiresAt: string;
  readonly deviceId: string;
}

/** GET /v1/identity/session(当前会话信息)。 */
export interface SessionInfoDto {
  readonly sessionId: string;
  readonly deviceId: string;
}

/** 已绑定设备(GET /v1/identity/devices 分页项)。 */
export interface DeviceDto {
  readonly deviceId: string;
  readonly createdAt: string;
}

export interface DeviceListDto {
  readonly items: readonly DeviceDto[];
  readonly nextCursor?: string;
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
