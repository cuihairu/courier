// M4 小助手客户端(契约 assistant.md Frozen v1)。POST /v1/assistant/query(全 Bearer)。
// 未命中不是错误:返回 matched=false 的结果对象,UI 据此引导转人工;
// 501 能力未接 → null(隐藏小助手入口,能力安静地不存在);无本地缓存(即席查询)。
import type { ApiClient } from "../core/apiClient.ts";
import { isCapabilityDisabled } from "../core/courierError.ts";

const prefix = "/v1/assistant";

/** 知识条目快照(命中时原样返回;契约:不改写)。 */
export interface AssistantAnswerDto {
  readonly id: string;
  readonly question: string;
  readonly answer: string;
  readonly keywords?: readonly string[];
}

/** 查询结果:matched=false 时 answer 缺省且 suggestTransfer=true(未命中不是错误)。 */
export interface AssistantQueryDto {
  readonly matched: boolean;
  readonly answer?: AssistantAnswerDto;
  readonly suggestTransfer: boolean;
}

export class AssistantService {
  private readonly api: ApiClient;

  constructor(api: ApiClient) {
    this.api = api;
  }

  /** 提问(1-500 字符;空/超长 = 使用方错误,服务端 400)。
   *  未命中 → matched=false + suggestTransfer=true;UI 据此调 Support 域提单转人工。 */
  async queryAsync(text: string): Promise<AssistantQueryDto | null> {
    try {
      return await this.api.request<AssistantQueryDto>("POST", prefix + "/query", { text }, true);
    } catch (e) {
      if (isCapabilityDisabled(e)) {
        return null; // 契约:能力未接 → 隐藏小助手入口
      }
      throw e; // 参数/限流/网络照常抛给调用方
    }
  }
}
