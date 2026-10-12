// M2 服务域 DTO(契约 announcement.md / support.md Frozen v1.1)。
// wire camelCase 与字段语义与网关响应逐一对齐;可选 = 契约缺省字段。
// 分页复用契约信封层 Page<T>(src/contract/envelope.ts)。

/** 公告(契约数据模型;严重度大写枚举字符串)。 */
export interface AnnouncementDto {
  readonly id: string;
  readonly title: string;
  readonly body: string;
  readonly severity: string;
  readonly startAt?: string; // 缺省 = 发布即可见(契约 announcement.md)
  readonly endAt?: string;   // 缺省 = 不过期
  readonly publishedAt: string;
  readonly filters?: Readonly<Record<string, readonly string[]>>; // 定向过滤(v1.1;缺省 = 全量可见)
}

/** 列表定向参数(v1.1):缺省维度不过滤;值须与发布 filters 维度值精确匹配。 */
export interface AnnouncementQuery {
  readonly platform?: string;
  readonly appVersion?: string;
  readonly region?: string;
  readonly language?: string;
}

/** 工单(状态 OPEN/REPLIED/CLOSED)。 */
export interface TicketDto {
  readonly id: string;
  readonly title: string;
  readonly status: string;
  readonly category?: string;
  readonly createdAt: string;
  readonly updatedAt: string;
  readonly context?: Readonly<Record<string, string | number | boolean>>; // 服务端零解释透传(v1.1)
}

/** 工单消息(senderType: PLAYER/AGENT/SYSTEM,createdAt 升序)。 */
export interface TicketMessageDto {
  readonly senderType: string;
  readonly body: string;
  readonly createdAt: string;
}

/** 工单附件(契约 support.md v1.1 详情 attachments[] 项;不返回服务端存储路径)。 */
export interface TicketAttachmentDto {
  readonly id: string;
  readonly fileName: string;
  readonly contentType: string;
  readonly sizeBytes: number;
  readonly kind?: "IMAGE" | "LOG" | "OTHER";
  readonly createdAt: string;
  readonly downloadUrl?: string; // 预签名短时效;对象未直传完成前由存储返回 404
}

/** 工单详情(ticket + messages + 附件,附件 createdAt 升序)。 */
export interface TicketDetailDto {
  readonly ticket: TicketDto;
  readonly messages: readonly TicketMessageDto[];
  readonly attachments?: readonly TicketAttachmentDto[]; // v1.1 加法扩展
}

/** FAQ 知识条目。 */
export interface FaqDto {
  readonly id: string;
  readonly question: string;
  readonly answer: string;
  readonly keywords?: readonly string[];
}

/** 提单请求(POST /support/tickets;category 缺省 OTHER)。 */
export interface CreateTicketRequest {
  readonly title: string;
  readonly body: string;
  readonly category?: string;
  readonly context?: Readonly<Record<string, string | number | boolean>>; // v1.1:键 1–32 位 [a-zA-Z0-9_.],≤10 键,值 ≤256 字符,整体 ≤1KiB
}

/** 申请附件直传请求(v1.1;fileName 1–255 字符,contentType 须在 allowlist 内,
 *  sizeBytes ≤ 10 MiB)。 */
export interface AttachmentUploadRequest {
  readonly fileName: string;
  readonly contentType: string;
  readonly sizeBytes: number;
  readonly kind?: "IMAGE" | "LOG" | "OTHER";
}

/** 申请附件直传响应(v1.1):客户端拿 uploadUrl 直传存储,不经网关。 */
export interface AttachmentUploadResult {
  readonly attachmentId: string;
  readonly uploadUrl: string;
  readonly method: string; // PUT
  readonly headers: Readonly<Record<string, string>>;
  readonly expiresAt: string;
}
