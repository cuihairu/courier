// 微信小游戏流式适配测试(零依赖,node:test):createWechatSseSource 走
// wx.request enableChunked——分块行缓冲、跨块多字节、心跳静默、头键小写归一、
// 建连失败/流中断重连(现取 token)、AbortSignal 净停。运行:node --test tests/wechatStream.test.ts
import assert from "node:assert/strict";
import { test } from "node:test";
import type { SseStreamRequest } from "../src/core/sseStream.ts";
import { SseStreamClient } from "../src/service/sseStreamClient.ts";
import { createWechatSseSource } from "../src/platform/wechat/sseSource.ts";
import { TransportError } from "../src/core/transport.ts";
import type { WxChunkedRequestOptions } from "../src/platform/wechat/wxApi.ts";

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

interface ScriptItem {
  statusCode?: number;
  header?: Record<string, string>;
  errMsg?: string;
}

interface FakeTask {
  cb?: (res: { data: ArrayBuffer }) => void;
  aborted: boolean;
  /** 头已交付后可挂断(流中断);建连 settle 前为空。 */
  hangup: () => void;
}

/** fake wx:按序回放分块响应;测试手动 pushTo/hangup 驱动块与断流。 */
class FakeChunkedWx {
  requests: WxChunkedRequestOptions[] = [];
  tasks: FakeTask[] = [];
  aborted = 0;
  private script: ScriptItem[];

  constructor(script: ScriptItem[]) {
    this.script = [...script];
  }

  request(options: WxChunkedRequestOptions) {
    this.requests.push(options);
    const next = this.script.shift() ?? { errMsg: "request:fail script exhausted" };
    const task: FakeTask = {
      aborted: false,
      hangup: () => {},
    };
    task.hangup = () => {
      options.fail({ errMsg: task.aborted ? "request:fail aborted" : "request:fail disconnected" });
    };
    this.tasks.push(task);
    queueMicrotask(() => {
      if (task.aborted || next.errMsg !== undefined) {
        options.fail({ errMsg: task.aborted ? "request:fail aborted" : next.errMsg! });
        return;
      }
      options.success({ statusCode: next.statusCode ?? 200, header: next.header });
    });
    return {
      abort: () => {
        this.aborted++;
        const first = !task.aborted;
        task.aborted = true;
        if (first) task.hangup(); // 真机语义:abort 以 fail 收场(归一断流/建连失败)
      },
      onChunkReceived: (cb: (res: { data: ArrayBuffer }) => void) => {
        task.cb = cb;
      },
    };
  }

  /** 向指定连接推一段文本(按 UTF-8 字节交付,模拟分块边界)。 */
  pushTo(index: number, text: string): void {
    const bytes = new TextEncoder().encode(text);
    this.tasks[index]?.cb?.({ data: bytes.buffer as ArrayBuffer });
  }

  /** 指定连接断流(wx fail)。 */
  hangup(index: number): void {
    this.tasks[index]?.hangup();
  }
}

function makeRequest(token: () => string | null, headers: Record<string, string> = {}): SseStreamRequest {
  return {
    url: "https://servicewechat.com/v1/messages/stream",
    headers: { "X-Courier-Game-Id": "game_demo", ...headers },
    tokenProvider: token,
  };
}

test("wechat sse streams frames, heartbeat silent, multibyte across chunks", async () => {
  const wx = new FakeChunkedWx([{ statusCode: 200, header: { "Content-Type": "text/event-stream" } }]);
  let token = 0;
  const controller = new AbortController();
  let run: Promise<void> | undefined;
  try {
    const client = new SseStreamClient({
      source: createWechatSseSource(wx),
      sleep: yieldSleep,
    });
    const events: { type: string; id?: string; data: string }[] = [];
    run = client.run(makeRequest(() => "tok-" + ++token), (e) => events.push(e), () => {}, controller.signal);
    await waitUntil(() => wx.requests.length >= 1);
    // 分块边界落在中文与 emoji 中间(🎮 = F0 9F 8E AE)
    wx.pushTo(0, "id: 1\nevent: announcement.published\ndata: {\"title\":\"公告\"}");
    wx.pushTo(0, "🎮");
    wx.pushTo(0, "\n\n: ping\n\nid: 2\nevent: config.updated\ndata: {\"configVersion\":7}\n\n");
    await waitUntil(() => events.length >= 2);
    assert.equal(events[0].type, "announcement.published");
    assert.equal(events[0].id, "1");
    assert.equal(events[0].data, "{\"title\":\"公告\"}🎮");
    assert.equal(events[1].type, "config.updated");
    assert.equal(wx.requests.length, 1);
    assert.equal(wx.requests[0].method, "GET");
    assert.equal(wx.requests[0].enableChunked, true);
    assert.equal(wx.requests[0].header["Accept"], "text/event-stream");
    assert.equal(wx.requests[0].header["Authorization"], "Bearer tok-1");
    assert.equal(wx.requests[0].header["X-Courier-Game-Id"], "game_demo");
  } finally {
    controller.abort();
    if (run) await awaitSettled(run);
  }
});

test("wechat sse reconnects after stream break with fresh token", async () => {
  const wx = new FakeChunkedWx([{ statusCode: 200 }, { statusCode: 200 }]);
  let token = 0;
  const controller = new AbortController();
  let run: Promise<void> | undefined;
  try {
    const client = new SseStreamClient({
      source: createWechatSseSource(wx),
      sleep: yieldSleep,
    });
    const events: { type: string }[] = [];
    run = client.run(makeRequest(() => "tok-" + ++token), (e) => events.push(e), () => {}, controller.signal);
    await waitUntil(() => wx.tasks.length >= 1);
    wx.pushTo(0, "id: 9\nevent: support.ticket_replied\ndata: {}\n\n");
    await waitUntil(() => events.length >= 1);
    wx.hangup(0); // 服务端关流(wx fail)→ 断流重连
    await waitUntil(() => wx.requests.length >= 2);
    wx.pushTo(1, "id: 10\nevent: config.updated\ndata: {\"a\":1}\n\n");
    await waitUntil(() => events.length >= 2);
    assert.equal(events[0].type, "support.ticket_replied");
    assert.equal(events[1].type, "config.updated");
    assert.equal(wx.requests[0].header["Authorization"], "Bearer tok-1");
    assert.equal(wx.requests[1].header["Authorization"], "Bearer tok-2"); // 重连现取
  } finally {
    controller.abort();
    if (run) await awaitSettled(run);
  }
});

test("wechat sse connect failure retries then delivers", async () => {
  const wx = new FakeChunkedWx([{ errMsg: "request:fail timeout" }, { statusCode: 200 }]);
  const controller = new AbortController();
  let run: Promise<void> | undefined;
  try {
    const client = new SseStreamClient({
      source: createWechatSseSource(wx),
      sleep: yieldSleep,
    });
    const events: { type: string }[] = [];
    run = client.run(makeRequest(() => "tok-x"), (e) => events.push(e), () => {}, controller.signal);
    await waitUntil(() => wx.requests.length >= 2); // 失败一次后重试
    wx.pushTo(1, "id: 1\nevent: announcement.published\ndata: {}\n\n");
    await waitUntil(() => events.length >= 1);
    assert.equal(events[0].type, "announcement.published");
  } finally {
    controller.abort();
    if (run) await awaitSettled(run);
  }
});

test("wechat sse abort before data stops cleanly", async () => {
  const wx = new FakeChunkedWx([{ statusCode: 200 }]);
  const controller = new AbortController();
  const client = new SseStreamClient({
    source: createWechatSseSource(wx),
    sleep: yieldSleep,
  });
  const events: { type: string }[] = [];
  let reasons: string[] = [];
  const run = client.run(makeRequest(() => "tok-x"), (e) => events.push(e),
    (r) => reasons.push(r), controller.signal);
  await waitUntil(() => wx.requests.length >= 1);
  controller.abort();
  await awaitSettled(run);
  assert.equal(wx.aborted, 1); // task.abort 被调用
  assert.deepEqual(events, []);
  assert.deepEqual(reasons, []); // 净停不发终态
});

test("wechat sse normalizes header keys (Retry-After honored)", async () => {
  const wx = new FakeChunkedWx([{ statusCode: 429, header: { "Retry-After": "7" } }]);
  const sleeps: number[] = [];
  const controller = new AbortController();
  const client = new SseStreamClient({
    source: createWechatSseSource(wx),
    sleep: async (ms) => { sleeps.push(ms); await yieldSleep(); },
  });
  let run: Promise<void> | undefined;
  try {
    run = client.run(makeRequest(() => "tok-x"), () => {}, () => {}, controller.signal);
    await waitUntil(() => sleeps.length >= 1);
    assert.equal(sleeps[0], 7000); // 大写头键仍生效(适配器小写归一)
  } finally {
    controller.abort();
    if (run) await awaitSettled(run);
  }
});

// ---- 补口:连接守卫与连接级行为边界 ----

test("wechat sse source guards:已取消拒连;wx.request 同步抛错归一(Error 与非 Error)", async () => {
  const source = createWechatSseSource(new FakeChunkedWx([]));
  const controller = new AbortController();
  controller.abort();
  await assert.rejects(() => source.connect(makeRequest(() => "t"), controller.signal),
    (e: unknown) => e instanceof TransportError && e.message === "aborted");

  const wxErr = { request(): never { throw new Error("wx down"); } };
  await assert.rejects(
    () => createWechatSseSource(wxErr as never).connect(makeRequest(() => "t")),
    (e: unknown) => e instanceof TransportError && e.message === "wx down");

  const wxStr = { request(): never { throw "wx broken"; } };
  await assert.rejects(
    () => createWechatSseSource(wxStr as never).connect(makeRequest(() => "t")),
    (e: unknown) => e instanceof TransportError && e.message === "wx broken");
});

test("wechat sse source:settled 后重复 success 忽略;task.abort 抛错被吞(fail 照常收场)", async () => {
  const wx = new FakeChunkedWx([{ statusCode: 200, header: { "X-A": "1" } }]);
  const origRequest = wx.request.bind(wx);
  // 覆写 request:abort 抛错须落在 sseSource 实际持有的返回 task 对象上(内部 FakeTask 的 abort 无人调用)
  wx.request = (options) => {
    const task = origRequest(options);
    task.abort = () => { throw new Error("abort boom"); };
    return task;
  };
  const source = createWechatSseSource(wx);
  const controller = new AbortController();
  const conn = await source.connect(makeRequest(() => "t"), controller.signal);

  const opts = wx.requests[0];
  opts.success({ statusCode: 500, header: { "X-B": "2" }, data: "" } as never);
  assert.equal(conn.statusCode, 200); // 头到后再 success:settled 分流忽略
  assert.equal((conn.headers as Record<string, string>)["x-a"], "1");
  assert.equal((conn.headers as Record<string, string>)["x-b"], undefined);

  controller.abort(); // onAbort 吞掉返回 task.abort 的抛错,不置 aborted 标记
  wx.hangup(0); // wx 仍以 fail 收场(aborted 标记未置 → disconnected)
  await assert.rejects(() => conn.readLine(), /disconnected/);
});

test("wechat sse connection:CRLF 行剥除;空块与关流后 push 忽略;close 净关流", async () => {
  const wx = new FakeChunkedWx([{ statusCode: 200 }]);
  const conn = await createWechatSseSource(wx).connect(makeRequest(() => "t"));

  wx.pushTo(0, "event: a\r\n\r\n");
  assert.equal(await conn.readLine(), "event: a"); // 行尾 \r 剥除
  assert.equal(await conn.readLine(), ""); // 帧分隔空行

  wx.pushTo(0, ""); // 空块:守卫直接忽略
  wx.hangup(0); // 断流
  wx.pushTo(0, "late"); // 关流后 push 忽略
  await assert.rejects(() => conn.readLine(), /disconnected/);

  const wx2 = new FakeChunkedWx([{ statusCode: 200 }]);
  const conn2 = await createWechatSseSource(wx2).connect(makeRequest(() => "t"));
  conn2.close(); // 净关流(无 failure)
  assert.equal(await conn2.readLine(), null);
  wx2.hangup(0); // close 后再来 fail:breakStream 已关守卫忽略,仍是净关流
  assert.equal(await conn2.readLine(), null);
});
