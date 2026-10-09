// M2 推送通道帧解析(契约 messages.md:SSE text/event-stream)。
// 纯逻辑、平台无关:流式读入的行喂进来,完整帧析出事件。
// 心跳注释(: ping)忽略;未知 event type 原样吐出——调用方按契约容忍未知。

/** SSE 单事件(type 两段小写,data 单行 JSON,序号)。 */
export interface SseEvent {
  type: string;
  data: string;
  id?: string;
}

/** SSE 帧解析器(增量):按行喂入,遇空行结算一帧。
 *  data 多行时按 SSE 规范以 \n 连接(契约 data 为单行 JSON,此为健壮性)。 */
export class SseParser {
  private data: string[] = [];
  private type: string | null = null;
  private id: string | null = null;
  private inFrame = false;

  /** 喂入一行(不含换行符)。返回结算出的事件;否则 null。 */
  feedLine(line: string | null | undefined): SseEvent | null {
    const l = line ?? "";
    if (l.length === 0) {
      // 空行 = 帧结束。
      if (!this.inFrame) {
        return null; // 心跳/连接初始化(: connected)后无内容的空行
      }
      const evt: SseEvent = {
        type: this.type ?? "",
        data: this.data.join("\n"),
      };
      if (this.id !== null) {
        evt.id = this.id;
      }
      this.type = null;
      this.id = null;
      this.data = [];
      this.inFrame = false;
      // 纯注释帧(心跳)不出事件:注释行不置 inFrame。
      return evt.data.length === 0 && evt.type.length === 0 ? null : evt;
    }
    if (l.startsWith(":")) {
      return null; // 注释帧(: ping / : connected)——保活语义,无业务
    }
    this.inFrame = true;
    const colon = l.indexOf(":");
    const field = colon < 0 ? l : l.slice(0, colon);
    let value = colon < 0 ? "" : l.slice(colon + 1);
    if (value.startsWith(" ")) {
      value = value.slice(1); // SSE 规范:冒号后单个前导空格剥除
    }
    switch (field) {
      case "event":
        this.type = value;
        break;
      case "data":
        this.data.push(value);
        break;
      case "id":
        this.id = value;
        break;
      default:
        break; // 未知字段容忍(契约 versioning.md)
    }
    return null;
  }

  /** 重置(断线重连后复用)。 */
  reset(): void {
    this.type = null;
    this.id = null;
    this.data = [];
    this.inFrame = false;
  }
}
