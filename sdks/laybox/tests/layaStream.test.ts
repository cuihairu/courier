// Layabox 流式适配测试(零依赖,node:test):createLayaSseSource 走
// XHR 流读——onprogress 增量(消费偏移切分)、onload 净关流重连、头/状态首拍回填、
// 多字节跨块、心跳静默、建连失败重试、Retry-After 生效、abort 净停。
// 运行:node --test tests/layaStream.test.ts
import assert from "node:assert/strict";
import { test } from "node:test";
import type { SseStreamRequest } from "../src/core/sseStream.ts";
import { TransportError } from "../src/core/transport.ts";
import { SseStreamClient } from "../src/service/sseStreamClient.ts";
import { createLayaSseSource } from "../src/platform/laya/sseSource.ts";

/** 让出宏任务(防纯微任务自旋饿死 waitUntil 的定时器)。 */
const yieldSleep = async () => new Promise<void>((r) => setTimeout(r, 0));

async function waitUntil(cond: () => boolean, ms = 5000): Promise<void> {
  const deadline = Date.now() + ms;
  while (!cond()) {
    if (Date.now() > deadline) {
      throw new Error("condition not met in time");
    }
    await new Promise<void>((r) => setTimeout(r, 10));
  }
}

async function awaitSettled(p: Promise<unknown>, ms = 5000): Promise<void> {
  let done = false;
  p.then(() => { done = true; }, () => { done = true; });
  await waitUntil(() => done, ms);
}

/** fake XHR:测试手动 head/progress/finish/fail 驱动流式阶段。 */
class FakeLayaXhr {
  method = "";
  url = "";
  headers: Record<string, string> = {};
  timeout = 0;
  status = 0;
  readyState = 0;
  responseText = "";
  rawHeaders = "";
  aborted = 0;
  sent = false;
  onprogress: (() => void) | null = null;
  onload: (() => void) | null = null;
  onerror: (() => void) | null = null;
  ontimeout: (() => void) | null = null;
  onabort: (() => void) | null = null;

  open(method: string, url: string): void {
    this.method = method;
    this.url = url;
  }
  setRequestHeader(name: string, value: string): void {
    this.headers[name] = value;
  }
  send(): void {
    this.sent = true;
  }
  getAllResponseHeaders(): string {
    return this.rawHeaders;
  }
  abort(): void {
    this.aborted++;
    this.onabort?.();
  }

  /** 头到(LOADING 态;状态码/头可读)。 */
  head(statusCode: number, header: Record<string, string>): void {
    this.status = statusCode;
    this.rawHeaders = Object.entries(header).map(([k, v]) => `${k}: ${v}`).join("\r\n");
    this.readyState = 3;
  }
  /** 体增量:responseText 累积后回调(浏览器内建流式解码,跨块多字节不撕裂)。 */
  progress(text: string): void {
    this.responseText += text;
    this.readyState = 3;
    this.onprogress?.();
  }
  /** 净关流(DONE):源按流尾归一,编排层重连。 */
  finish(): void {
    this.readyState = 4;
    this.onload?.();
  }
  /** 网络失败(建连/流中断两用)。 */
  fail(): void {
    this.onerror?.();
  }
  /** 超时(长连不设超时;超时归一断流)。 */
  timedOut(): void {
    this.ontimeout?.();
  }
}

class FakeXhrFactory {
  xhrs: FakeLayaXhr[] = [];
  constructor() {
    const self = this;
    this.create = () => {
      const xhr = new FakeLayaXhr();
      self.xhrs.push(xhr);
      return xhr;
    };
  }
  create: () => FakeLayaXhr;
}

function makeRequest(token: () => string): SseStreamRequest {
  return {
    url: "https://layabox.example.com/v1/messages/stream",
    headers: { "X-Courier-Game-Id": "game_demo" },
    tokenProvider: token,
  };
}

test("laya sse streams frames, heartbeat silent, multibyte across progress", async () => {
  const factory = new FakeXhrFactory();
  let token = 0;
  const controller = new AbortController();
  let run: Promise<void> | undefined;
  try {
    const client = new SseStreamClient({
      source: createLayaSseSource(factory.create),
      sleep: yieldSleep,
    });
    const events: { type: string; id?: string; data: string }[] = [];
    run = client.run(makeRequest(() => "tok-" + ++token), (e) => events.push(e), () => {}, controller.signal);
    await waitUntil(() => factory.xhrs.length >= 1);
    factory.xhrs[0].head(200, { "Content-Type": "text/event-stream" });
    // 增量边界落在中文与 emoji 中间(🎮 = 4 字节)
    factory.xhrs[0].progress("id: 1\nevent: announcement.published\ndata: {\"title\":\"公告\"}");
    factory.xhrs[0].progress("🎮\n\n: ping\n\nid: 2\nevent: config.updated\ndata: {\"configVersion\":7}\n\n");
    await waitUntil(() => events.length >= 2);
    assert.equal(events[0].type, "announcement.published");
    assert.equal(events[0].id, "1");
    assert.equal(events[0].data, "{\"title\":\"公告\"}🎮");
    assert.equal(events[1].type, "config.updated");
    assert.equal(factory.xhrs.length, 1);
    assert.equal(factory.xhrs[0].method, "GET");
    assert.equal(factory.xhrs[0].timeout, 0); // 长连不设超时
    assert.equal(factory.xhrs[0].headers["Accept"], "text/event-stream");
    assert.equal(factory.xhrs[0].headers["Authorization"], "Bearer tok-1");
    assert.equal(factory.xhrs[0].headers["X-Courier-Game-Id"], "game_demo");
  } finally {
    controller.abort();
    if (run) await awaitSettled(run);
  }
});

test("laya sse clean stream end reconnects with fresh token", async () => {
  const factory = new FakeXhrFactory();
  let token = 0;
  const controller = new AbortController();
  let run: Promise<void> | undefined;
  try {
    const client = new SseStreamClient({
      source: createLayaSseSource(factory.create),
      sleep: yieldSleep,
    });
    const events: { type: string }[] = [];
    run = client.run(makeRequest(() => "tok-" + ++token), (e) => events.push(e), () => {}, controller.signal);
    await waitUntil(() => factory.xhrs.length >= 1);
    factory.xhrs[0].head(200, {});
    factory.xhrs[0].progress("id: 9\nevent: support.ticket_replied\ndata: {}\n\n");
    await waitUntil(() => events.length >= 1);
    factory.xhrs[0].finish(); // 服务端净关流(onload)→ 重连
    await waitUntil(() => factory.xhrs.length >= 2);
    factory.xhrs[1].head(200, {});
    factory.xhrs[1].progress("id: 10\nevent: config.updated\ndata: {\"a\":1}\n\n");
    await waitUntil(() => events.length >= 2);
    assert.equal(events[0].type, "support.ticket_replied");
    assert.equal(events[1].type, "config.updated");
    assert.equal(factory.xhrs[0].headers["Authorization"], "Bearer tok-1");
    assert.equal(factory.xhrs[1].headers["Authorization"], "Bearer tok-2"); // 重连现取
  } finally {
    controller.abort();
    if (run) await awaitSettled(run);
  }
});

test("laya sse connect failure retries then delivers", async () => {
  const factory = new FakeXhrFactory();
  const controller = new AbortController();
  let run: Promise<void> | undefined;
  try {
    const client = new SseStreamClient({
      source: createLayaSseSource(factory.create),
      sleep: yieldSleep,
    });
    const events: { type: string }[] = [];
    run = client.run(makeRequest(() => "tok-x"), (e) => events.push(e), () => {}, controller.signal);
    await waitUntil(() => factory.xhrs.length >= 1);
    factory.xhrs[0].fail(); // 建连网络失败 → 重连
    await waitUntil(() => factory.xhrs.length >= 2);
    factory.xhrs[1].head(200, {});
    factory.xhrs[1].progress("id: 1\nevent: announcement.published\ndata: {}\n\n");
    await waitUntil(() => events.length >= 1);
    assert.equal(events[0].type, "announcement.published");
    assert.equal(factory.xhrs.length, 2); // 失败一次后重试
  } finally {
    controller.abort();
    if (run) await awaitSettled(run);
  }
});

test("laya sse abort calls xhr abort and stops cleanly", async () => {
  const factory = new FakeXhrFactory();
  const controller = new AbortController();
  const client = new SseStreamClient({
    source: createLayaSseSource(factory.create),
    sleep: yieldSleep,
  });
  const events: { type: string }[] = [];
  const reasons: string[] = [];
  const run = client.run(makeRequest(() => "tok-x"), (e) => events.push(e),
    (r) => reasons.push(r), controller.signal);
  await waitUntil(() => factory.xhrs.length >= 1);
  controller.abort();
  await awaitSettled(run);
  assert.equal(factory.xhrs[0].aborted, 1); // xhr.abort 被调用
  assert.deepEqual(events, []);
  assert.deepEqual(reasons, []); // 净停不发终态
});

test("laya sse honors Retry-After on 429", async () => {
  const factory = new FakeXhrFactory();
  const sleeps: number[] = [];
  const controller = new AbortController();
  const client = new SseStreamClient({
    source: createLayaSseSource(factory.create),
    sleep: async (ms) => { sleeps.push(ms); await yieldSleep(); },
  });
  let run: Promise<void> | undefined;
  try {
    run = client.run(makeRequest(() => "tok-x"), () => {}, () => {}, controller.signal);
    await waitUntil(() => factory.xhrs.length >= 1);
    factory.xhrs[0].head(429, { "Retry-After": "7" });
    factory.xhrs[0].progress("{\"error\":\"rate limited\"}"); // 错误体:onprogress 首拍回填
    await waitUntil(() => sleeps.length >= 1);
    assert.equal(sleeps[0], 7000); // 大写头键仍生效(适配器小写归一)
  } finally {
    controller.abort();
    if (run) await awaitSettled(run);
  }
});

test("laya sse empty stream ends clean and reconnects", async () => {
  const factory = new FakeXhrFactory();
  let token = 0;
  const controller = new AbortController();
  let run: Promise<void> | undefined;
  try {
    const client = new SseStreamClient({
      source: createLayaSseSource(factory.create),
      sleep: yieldSleep,
    });
    const events: { type: string }[] = [];
    run = client.run(makeRequest(() => "tok-" + ++token), (e) => events.push(e), () => {}, controller.signal);
    await waitUntil(() => factory.xhrs.length >= 1);
    factory.xhrs[0].head(200, {});
    factory.xhrs[0].finish(); // 无体直接 DONE:onload 首拍回填 + 流尾 → 重连
    await waitUntil(() => factory.xhrs.length >= 2);
    assert.deepEqual(events, []);
    assert.equal(factory.xhrs[0].headers["Authorization"], "Bearer tok-1");
    assert.equal(factory.xhrs[1].headers["Authorization"], "Bearer tok-2");
  } finally {
    controller.abort();
    if (run) await awaitSettled(run);
  }
});

test("laya sse source 守卫:已取消拒连;工厂同步抛错归一(Error 与非 Error);send 同步抛错归一", async () => {
  const factory = new FakeXhrFactory();
  const controller = new AbortController();
  controller.abort();
  await assert.rejects(
    () => createLayaSseSource(factory.create).connect(makeRequest(() => "t"), controller.signal),
    (e: unknown) => e instanceof TransportError && e.message === "aborted");

  const boom = { create: () => { throw new Error("xhr down"); } };
  await assert.rejects(
    () => createLayaSseSource(boom.create as never).connect(makeRequest(() => "t")),
    (e: unknown) => e instanceof TransportError && e.message === "xhr down");

  const rawBoom = { create: () => { throw "xhr broken"; } };
  await assert.rejects(
    () => createLayaSseSource(rawBoom.create as never).connect(makeRequest(() => "t")),
    (e: unknown) => e instanceof TransportError && e.message === "xhr broken");

  const sendBoom = {
    open() {}, setRequestHeader() {}, send() { throw new Error("send boom"); }, abort() {},
    getAllResponseHeaders() { return ""; },
    timeout: 0, status: 0, readyState: 0, responseText: "",
    onprogress: null, onload: null, onerror: null, ontimeout: null, onabort: null,
  };
  await assert.rejects(
    () => createLayaSseSource((() => sendBoom) as never).connect(makeRequest(() => "t")),
    (e: unknown) => e instanceof TransportError && e.message === "send boom");
});

test("laya sse connection:CRLF 行剥除;progress 携 DONE 交流尾;abort/close 抛错被吞", async () => {
  const factory = new FakeXhrFactory();
  const controller = new AbortController();
  const connPromise = createLayaSseSource(factory.create).connect(makeRequest(() => "t"), controller.signal);
  const xhr = factory.xhrs[0]; // factory 同步建 xhr;FakeLayaXhr 全手动驱动

  xhr.head(200, {});
  xhr.progress("event: a\r\n\r\n"); // 首拍:回填连接并 resolve
  const conn = await connPromise;
  assert.equal(await conn.readLine(), "event: a"); // 行尾 \r 剥除
  assert.equal(await conn.readLine(), ""); // 帧分隔空行

  // onprogress 携 DONE(部分浏览器体尽时末拍 readyState=4):交流尾
  xhr.responseText += "data: {}\n\n";
  xhr.readyState = 4;
  xhr.onprogress?.();
  assert.equal(await conn.readLine(), "data: {}");
  assert.equal(await conn.readLine(), "");
  assert.equal(await conn.readLine(), null); // DONE 交流尾(净尽,无 failure)

  xhr.abort = () => { throw new Error("abort boom"); };
  controller.abort(); // onAbort 吞掉 xhr.abort 抛错
  conn.close(); // close 内 abort 抛错同样被吞
  assert.equal(await conn.readLine(), null); // 仍是净尽
});

test("laya sse 事件回调内取消:循环顶检查净停收尾(不重连不发终态)", async () => {
  const factory = new FakeXhrFactory();
  const controller = new AbortController();
  const client = new SseStreamClient({
    source: createLayaSseSource(factory.create),
    sleep: yieldSleep,
  });
  const events: { type: string }[] = [];
  const reasons: string[] = [];
  const run = client.run(makeRequest(() => "tok-x"), (e) => {
    events.push(e);
    controller.abort(); // 事件回调内同步取消:readLine 返回后循环顶检查
  }, (r) => reasons.push(r), controller.signal);
  await waitUntil(() => factory.xhrs.length >= 1);
  factory.xhrs[0].head(200, {});
  factory.xhrs[0].progress("id: 1\nevent: config.updated\ndata: {}\n\n");
  await awaitSettled(run);
  assert.equal(events.length, 1);
  assert.equal(factory.xhrs.length, 1); // 净停:不再重连
  assert.ok(factory.xhrs[0].aborted >= 1); // signal 监听与 close 各 abort 一次
  assert.deepEqual(reasons, []); // 净停不发终态回调
});

test("laya sse source:无 headers 建连(headers ?? {} 空头集,无业务头)", async () => {
  const factory = new FakeXhrFactory();
  const request: SseStreamRequest = {
    url: "https://layabox.example.com/v1/messages/stream",
    tokenProvider: () => "tok-1",
  };
  const connPromise = createLayaSseSource(factory.create).connect(request);
  const xhr = factory.xhrs[0];
  xhr.head(200, {});
  xhr.progress("id: 1\nevent: announcement.published\ndata: {}\n\n");
  const conn = await connPromise;
  assert.equal(conn.statusCode, 200);
  assert.equal(xhr.headers["Accept"], "text/event-stream"); // 固定头仍在
  assert.equal(xhr.headers["Authorization"], undefined); // source 只透传:Bearer 由编排层拼
  assert.equal(xhr.headers["X-Courier-Game-Id"], undefined); // headers 缺省:for-of 零迭代
  assert.equal(await conn.readLine(), "id: 1");
});

test("laya sse timeout 归一断流:建连超时走重连,流中断超时断流重连", async () => {
  const factory = new FakeXhrFactory();
  let token = 0;
  const controller = new AbortController();
  let run: Promise<void> | undefined;
  try {
    const client = new SseStreamClient({
      source: createLayaSseSource(factory.create),
      sleep: yieldSleep,
    });
    const events: { type: string }[] = [];
    run = client.run(makeRequest(() => "tok-" + ++token), (e) => events.push(e), () => {}, controller.signal);
    await waitUntil(() => factory.xhrs.length >= 1);
    factory.xhrs[0].timedOut(); // 建连超时 → 归一断流重试
    await waitUntil(() => factory.xhrs.length >= 2);
    factory.xhrs[1].head(200, {});
    factory.xhrs[1].progress("id: 1\nevent: announcement.published\ndata: {}\n\n");
    await waitUntil(() => events.length >= 1);
    assert.equal(events[0].type, "announcement.published");
    assert.equal(factory.xhrs[0].headers["Authorization"], "Bearer tok-1");
    assert.equal(factory.xhrs[1].headers["Authorization"], "Bearer tok-2");

    // 流中断超时:已建连连 → breakStream → 重连
    factory.xhrs[1].timedOut();
    await waitUntil(() => factory.xhrs.length >= 3);
    factory.xhrs[2].head(200, {});
    factory.xhrs[2].progress("id: 2\nevent: config.updated\ndata: {}\n\n");
    await waitUntil(() => events.length >= 2);
    assert.equal(events[1].type, "config.updated");
    assert.equal(factory.xhrs[2].headers["Authorization"], "Bearer tok-3");
  } finally {
    controller.abort();
    if (run) await awaitSettled(run);
  }
});
