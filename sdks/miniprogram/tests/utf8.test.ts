// L2 Core 单测(零依赖,node:test):增量 UTF-8 解码——跨块多字节不撕裂、
// 非法输入安全降级(U+FFFD)、流尾截断冲刷。
// 运行:node --test tests/utf8.test.ts
import assert from "node:assert/strict";
import { test } from "node:test";
import { IncrementalUtf8Decoder } from "../src/core/utf8.ts";

/** 按给定切分点分块解码,拼回结果。 */
function decodeSplit(bytes: Uint8Array, splits: number[]): string {
  const decoder = new IncrementalUtf8Decoder();
  let out = "";
  let start = 0;
  for (const at of splits) {
    out += decoder.decode(bytes.subarray(start, at));
    start = at;
  }
  out += decoder.decode(bytes.subarray(start));
  out += decoder.end();
  return out;
}

test("utf8 ascii and multibyte roundtrip", () => {
  const samples = [
    "hello",
    "公告已发布",
    "emoji 🎮🎉 tail",
    "mixed 中英 🎮 混排",
    "\u0000 control \u007F del",
  ];
  const encoder = new TextEncoder();
  for (const sample of samples) {
    const bytes = encoder.encode(sample);
    assert.equal(decodeSplit(bytes, []), sample);
    assert.equal(decodeSplit(bytes, [1, 2, 3, bytes.length - 1]), sample);
  }
});

test("utf8 every split point preserves multibyte", () => {
  const sample = "A中🎮B";
  const bytes = new TextEncoder().encode(sample);
  for (let at = 1; at < bytes.length; at++) {
    assert.equal(decodeSplit(bytes, [at]), sample, `split at ${at}`);
  }
});

test("utf8 byte-by-byte equals whole", () => {
  const sample = "推送中💀done";
  const bytes = new TextEncoder().encode(sample);
  const splits = Array.from({ length: bytes.length - 1 }, (_, i) => i + 1);
  assert.equal(decodeSplit(bytes, splits), sample);
});

test("utf8 orphan continuation and broken sequence degrade to U+FFFD", () => {
  // 0x80 孤立续字节
  assert.equal(decodeSplit(new Uint8Array([0x80]), []), "�");
  // "中"(E4 B8 AD)序列被 ASCII 打断:FFF D + "A"
  assert.equal(decodeSplit(new Uint8Array([0xe4, 0x41]), []), "�A");
});

test("utf8 overlong, surrogate, out-of-range reject", () => {
  // 超长编码 0xC0 0x80(U+0000 的两字节形):起始非法 + 孤立续字节
  assert.equal(decodeSplit(new Uint8Array([0xc0, 0x80]), []), "��");
  // 代理区 U+D800 的三字节形 ED A0 80:形状合法但码点拒收
  assert.equal(decodeSplit(new Uint8Array([0xed, 0xa0, 0x80]), []), "�");
  // 越界 0xF5 0x80 0x80 0x80:起始非法 + 三个孤立续字节
  assert.equal(decodeSplit(new Uint8Array([0xf5, 0x80, 0x80, 0x80]), []), "����");
});

test("utf8 truncated tail flushed as U+FFFD", () => {
  const decoder = new IncrementalUtf8Decoder();
  assert.equal(decoder.decode(new Uint8Array([0xe4, 0xb8])), ""); // "中" 前两字节
  assert.equal(decoder.end(), "�"); // 流尾截断
  const clean = new IncrementalUtf8Decoder();
  clean.decode(new TextEncoder().encode("ok"));
  assert.equal(clean.end(), ""); // 完整输入无残留
});

test("utf8 emoji split across three chunks", () => {
  const decoder = new IncrementalUtf8Decoder();
  const bytes = new TextEncoder().encode("🎮"); // F0 9F 8E AE
  let out = "";
  out += decoder.decode(bytes.subarray(0, 1));
  out += decoder.decode(bytes.subarray(1, 3));
  out += decoder.decode(bytes.subarray(3));
  out += decoder.end();
  assert.equal(out, "🎮");
});
