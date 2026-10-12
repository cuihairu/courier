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

/** 工单详情(ticket + messages)。 */
export interface TicketDetailDto {
  readonly ticket: TicketDto;
  readonly messages: readonly TicketMessageDto[];
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
