// L2 Core:增量 UTF-8 解码(纯逻辑,平台无关)。
// 用途:无 TextDecoder 的平台运行时(微信小游戏/小程序、LayaAir JSB 等)按字节
// 分块喂入,跨块多字节序列不撕裂——微信 wx.request enableChunked 的 onChunkReceived
// 以 ArrayBuffer 交付,分块边界可落在任意字节。有 TextDecoder 的环境
// (Node / Cocos web)用原生实现,见 core/transport.ts、core/sseStream.ts 的注释。
// 语义:形状级校验(起始字节/续字节/长度),超长编码与代理区/越界码点以 U+FFFD
// 替代;网关 JSON 恒为合法 UTF-8,本件只需在非法输入下安全降级而非报错。

/** 增量 UTF-8 解码器:decode(chunk) 按块喂入,end() 冲刷尾部残留。 */
export class IncrementalUtf8Decoder {
  private pending: number[] = [];

  /** 喂入一块字节,返回已能确定的字符串;不完整的序列留待下一块。 */
  decode(chunk: Uint8Array): string {
    let out = "";
    for (const byte of chunk) {
      out += this.pushByte(byte);
    }
    return out;
  }

  /** 冲刷:残留的不完整序列按截断处理(U+FFFD),流尾残余不应被静默丢弃。 */
  end(): string {
    if (this.pending.length === 0) {
      return "";
    }
    this.pending = [];
    return "�";
  }

  private pushByte(byte: number): string {
    if (this.pending.length === 0) {
      if (byte < 0x80) {
        return String.fromCharCode(byte); // ASCII 直出
      }
      if ((byte & 0xE0) === 0xC0) {
        if (byte < 0xC2) {
          return "�"; // 0xC0/0xC1 = 超长编码
        }
        this.pending.push(byte); // 2 字节序列
        return "";
      }
      if ((byte & 0xF0) === 0xE0) {
        this.pending.push(byte); // 3 字节序列
        return "";
      }
      if ((byte & 0xF8) === 0xF0) {
        if (byte > 0xF4) {
          return "�"; // > U+10FFFF
        }
        this.pending.push(byte); // 4 字节序列
        return "";
      }
      return "�"; // 孤立续字节或非法起始
    }
    if ((byte & 0xC0) !== 0x80) {
      this.pending = []; // 续字节缺失:序列作废,当前字节重新起头
      return "�" + this.pushByte(byte);
    }
    this.pending.push(byte);
    const expected = this.pending[0] >= 0xF0 ? 4 : this.pending[0] >= 0xE0 ? 3 : 2;
    if (this.pending.length < expected) {
      return "";
    }
    const codePoint = this.codePoint();
    this.pending = [];
    return codePoint === null ? "�" : codePointToString(codePoint);
  }

  private codePoint(): number | null {
    if (this.pending.length === 2) {
      const codePoint = ((this.pending[0] & 0x1F) << 6) | (this.pending[1] & 0x3F);
      return codePoint >= 0x80 ? codePoint : null; // 排除超长编码
    }
    if (this.pending.length === 3) {
      const codePoint = ((this.pending[0] & 0x0F) << 12) | ((this.pending[1] & 0x3F) << 6)
        | (this.pending[2] & 0x3F);
      if (codePoint < 0x800) {
        return null; // 排除超长编码
      }
      return codePoint >= 0xD800 && codePoint <= 0xDFFF ? null : codePoint; // UTF-8 不编码代理区
    }
    const codePoint = ((this.pending[0] & 0x07) << 18) | ((this.pending[1] & 0x3F) << 12)
      | ((this.pending[2] & 0x3F) << 6) | (this.pending[3] & 0x3F);
    return codePoint < 0x10000 || codePoint > 0x10FFFF ? null : codePoint;
  }
}

function codePointToString(codePoint: number): string {
  if (codePoint <= 0xFFFF) {
    return String.fromCharCode(codePoint);
  }
  const offset = codePoint - 0x10000;
  return String.fromCharCode(0xD800 + (offset >> 10), 0xDC00 + (offset & 0x3FF));
}
