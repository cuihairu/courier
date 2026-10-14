// Core 传输层补口(零依赖,node:test):FetchTransport(默认传输实现——仓内此前
// 只有脚本化 FakeTransport,真 fetch 适配从无单测覆盖)+ FetchSseConnection
// 行读边界(CRLF / 流尾残余半行 / 流读失败归一 / 无 body 拒)+ SessionService
// 边界(未接线守卫 / clear / 无会话 refresh / 非安全事件上抛 / 设备面)。
import assert from "node:assert/strict";
import { test } from "node:test";
import { CourierClient } from "../src/core/courierClient.ts";
import { LifecycleMachine } from "../src/core/lifecycle.ts";
import { SessionService } from "../src/core/session.ts";
import { FetchSseSource } from "../src/core/sseStream.ts";
import { FetchTransport, TransportError } from "../src/core/transport.ts";
import type { Transport, TransportRequest, TransportResponse } from "../src/core/transport.ts";
import { MemoryTokenStore } from "../src/core/tokenStore.ts";
import type { SessionDto } from "../src/core/types.ts";

const config = { endpoint: "https://api.example.com", gameId: "game_demo", env: "prod" };
const sessionJson = JSON.stringify({
  account: { id: "acc_1", type: "GUEST", status: "ACTIVE", createdAt: "2026-10-09T12:00:00.000Z" },
  accessToken: "access-1",
  accessExpiresAt: "2026-10-09T12:15:00.000Z",
  refreshToken: "refresh-1",
  refreshExpiresAt: "2026-10-10T12:00:00.000Z",
  deviceId: "device-1",
});
const success = (data: unknown) => JSON.stringify({ data });
const successRaw = (raw: string) => '{"data":' + raw + "}";

class FakeTransport implements Transport {
  requests: TransportRequest[] = [];
  private queue: TransportResponse[] = [];

  enqueue(statusCode: number, body: string): void {
    this.queue.push({ statusCode, headers: {}, body });
  }

  async send(request: TransportRequest): Promise<TransportResponse> {
    this.requests.push(request);
    const next = this.queue.shift();
    if (!next) throw new Error("脚本耗尽:" + request.method + " " + request.url);
    return next;
  }
}

/** 记录一次 fetch 调用入参(断言 method/headers/body/signal 透传)。 */
type FetchCall = { url: string; init: RequestInit };

function fakeFetch(
  handler: (call: FetchCall) => Response | Promise<Response>,
): { impl: typeof fetch; calls: FetchCall[] } {
  const calls: FetchCall[] = [];
  const impl = (async (input: unknown, init: RequestInit = {}) => {
    calls.push({ url: String(input), init });
    return await handler({ url: String(input), init });
  }) as unknown as typeof fetch;
  return { impl, calls };
}

// ---- FetchTransport(默认传输实现) ----

test("FetchTransport:透传 method/headers/jsonBody;响应头键小写归一", async () => {
  const { impl, calls } = fakeFetch(() =>
    new Response("{\"data\":null}", { status: 200, headers: { "X-Trace-Id": "trace-1" } }));
  const transport = new FetchTransport(impl);

  const resp = await transport.send({
    method: "POST",
    url: "https://api.example.com/v1/identity/refresh",
    headers: { "Content-Type": "application/json" },
    jsonBody: "{\"refreshToken\":\"r-1\"}",
    timeoutMs: 5000,
  });

  assert.equal(resp.statusCode, 200);
  assert.equal(resp.body, "{\"data\":null}");
  assert.equal(resp.headers["x-trace-id"], "trace-1"); // 键小写归一(契约 transport)
  assert.equal(calls.length, 1);
  assert.equal(calls[0].url, "https://api.example.com/v1/identity/refresh");
  assert.equal(calls[0].init.method, "POST");
  assert.deepEqual(calls[0].init.headers, { "Content-Type": "application/json" });
  assert.equal(calls[0].init.body, "{\"refreshToken\":\"r-1\"}");
  assert.ok(calls[0].init.signal instanceof AbortSignal);
});

test("FetchTransport:timeoutMs 非正回落 10s 默认(不抛,signal 仍挂载)", async () => {
  const { impl, calls } = fakeFetch(() => new Response("", { status: 200 }));
  const transport = new FetchTransport(impl);

  const resp = await transport.send({
    method: "GET", url: "https://api.example.com/healthz", headers: {}, timeoutMs: 0,
  });
  assert.equal(resp.statusCode, 200);
  assert.ok(calls[0].init.signal instanceof AbortSignal);
});

test("FetchTransport:网络失败归一 TransportError(Error 与非 Error 两形)", async () => {
  const throwing = new FetchTransport(fakeFetch(() => {
    throw new TypeError("network down");
  }).impl);
  const err = await throwing.send({
    method: "GET", url: "https://api.example.com/x", headers: {}, timeoutMs: 1000,
  }).catch((e: unknown) => e);
  assert.ok(err instanceof TransportError);
  assert.equal(err.name, "TransportError");
  assert.equal(err.message, "network down");

  const throwingRaw = new FetchTransport(fakeFetch(() => {
    throw "offline"; // 非 Error:String(e) 归一
  }).impl);
  const raw = await throwingRaw.send({
    method: "GET", url: "https://api.example.com/x", headers: {}, timeoutMs: 1000,
  }).catch((e: unknown) => e);
  assert.ok(raw instanceof TransportError);
  assert.equal(raw.message, "offline");
});

test("FetchTransport:省略 fetchImpl 时绑定 globalThis.fetch", async () => {
  const original = globalThis.fetch;
  const { impl, calls } = fakeFetch(() => new Response("bound", { status: 200 }));
  (globalThis as { fetch: typeof fetch }).fetch = impl;
  try {
    const transport = new FetchTransport(); // 不传实现 → globalThis.fetch
    const resp = await transport.send({
      method: "GET", url: "https://api.example.com/ping", headers: {}, timeoutMs: 1000,
    });
    assert.equal(resp.body, "bound");
    assert.equal(calls.length, 1);
  } finally {
    (globalThis as { fetch: typeof fetch }).fetch = original;
  }
});

// ---- FetchSseSource / FetchSseConnection 行读边界 ----

/** 由分片数组构造 chunked 响应(逐片投递,不整块给出)。 */
function streamResponse(chunks: (Uint8Array | Error)[]): Response {
  let i = 0;
  const stream = new ReadableStream<Uint8Array>({
    pull(controller) {
      if (i >= chunks.length) {
        controller.close();
        return;
      }
      const next = chunks[i++];
      if (next instanceof Error) controller.error(next);
      else controller.enqueue(next);
    },
  });
  return new Response(stream, { status: 200, headers: { "Content-Type": "text/event-stream" } });
}

const enc = (s: string) => new TextEncoder().encode(s);

test("FetchSseSource:connect 失败归一 TransportError(Error 与非 Error 两形)", async () => {
  const source = new FetchSseSource(fakeFetch(() => {
    throw new Error("refused");
  }).impl);
  const err = await source.connect({ url: "https://api.example.com/v1/messages/stream" })
    .catch((e: unknown) => e);
  assert.ok(err instanceof TransportError);
  assert.equal(err.message, "sse connect failed: refused");

  const sourceRaw = new FetchSseSource(fakeFetch(() => {
    throw "dns"; // 非 Error:String(e) 归一
  }).impl);
  const raw = await sourceRaw.connect({ url: "https://api.example.com/v1/messages/stream" })
    .catch((e: unknown) => e);
  assert.ok(raw instanceof TransportError);
  assert.equal(raw.message, "sse connect failed: dns");
});

test("FetchSseSource:非流式 body(204)拒连接;头键小写归一;Bearer 透传", async () => {
  const { impl } = fakeFetch(() => new Response(null, { status: 204 }));
  const err = await new FetchSseSource(impl)
    .connect({ url: "https://api.example.com/v1/messages/stream" })
    .catch((e: unknown) => e);
  assert.ok(err instanceof TransportError);
  assert.equal(err.message, "sse stream requires a streamable body");

  const ok = fakeFetch(() => new Response(streamResponse([enc("a\n")]), {
    status: 200, headers: { "X-Courier-Topic": "events" },
  }));
  const conn = await new FetchSseSource(ok.impl).connect({
    url: "https://api.example.com/v1/messages/stream",
    headers: { Authorization: "Bearer access-1" },
  });
  assert.equal(conn.statusCode, 200);
  assert.equal(conn.headers["x-courier-topic"], "events");
  assert.equal((ok.calls[0].init.headers as Record<string, string>).Accept, "text/event-stream");
  assert.equal((ok.calls[0].init.headers as Record<string, string>).Authorization, "Bearer access-1");
  conn.close();
});

test("FetchSseConnection:CRLF 容忍;流尾残余半行吐出;再读 null 断流;close 幂等", async () => {
  const conn = await new FetchSseSource(fakeFetch(() =>
    streamResponse([enc("a\r\nb"), enc("\nc")])).impl)
    .connect({ url: "https://api.example.com/v1/messages/stream" });

  assert.equal(await conn.readLine(), "a"); // CRLF:剥尾 \r
  assert.equal(await conn.readLine(), "b");
  assert.equal(await conn.readLine(), "c");
  assert.equal(await conn.readLine(), null); // 流尾 = 断流

  const tail = await new FetchSseSource(fakeFetch(() =>
    streamResponse([enc("x\n残行")])).impl)
    .connect({ url: "https://api.example.com/v1/messages/stream" });
  assert.equal(await tail.readLine(), "x");
  assert.equal(await tail.readLine(), "残行"); // 流尾残余半行按行吐出(SSE 规范容忍)
  assert.equal(await tail.readLine(), null);
  tail.close();
  tail.close(); // 已 done:幂等 no-op
});

test("FetchSseConnection:流读失败归一 TransportError 且被记忆(重读同错)", async () => {
  const conn = await new FetchSseSource(fakeFetch(() =>
    streamResponse([enc("a\n"), new Error("socket hang up")])).impl)
    .connect({ url: "https://api.example.com/v1/messages/stream" });

  assert.equal(await conn.readLine(), "a"); // 先吐已解出的行
  const err = await conn.readLine().catch((e: unknown) => e);
  assert.ok(err instanceof TransportError);
  assert.equal(err.message, "sse stream broken: socket hang up");
  const again = await conn.readLine().catch((e: unknown) => e);
  assert.equal(again, err); // 失败被记忆,后续读同抛

  const rawConn = await new FetchSseSource(fakeFetch(() => {
    let i = 0;
    const stream = new ReadableStream<Uint8Array>({
      pull(controller) {
        if (i++ === 0) controller.error("boom"); // 非 Error
        else controller.close();
      },
    });
    return new Response(stream, { status: 200 });
  }).impl).connect({ url: "https://api.example.com/v1/messages/stream" });
  const rawErr = await rawConn.readLine().catch((e: unknown) => e);
  assert.ok(rawErr instanceof TransportError);
  assert.equal(rawErr.message, "sse stream broken: boom"); // 非 Error:String(e) 归一
});

// ---- SessionService 边界 ----

test("session:未接线的 SessionService 拒绝发包(守卫抛错)", async () => {
  // CourierClient 构造即回填 api;裸构造 SessionService 才是未接线面。
  const bare = new SessionService(new MemoryTokenStore(), new LifecycleMachine());
  const err = await bare.info().catch((e: unknown) => e);
  assert.ok(err instanceof Error);
  assert.match((err as Error).message, /未接线/);
});

test("session:clear 本地清场;无会话时 refresh 直接 false(不发包)", async () => {
  const transport = new FakeTransport();
  const store = new MemoryTokenStore();
  const client = new CourierClient({ config, transport, tokenStore: store, sleep: async () => {} });
  transport.enqueue(200, successRaw(sessionJson));
  await client.identity.guest({ deviceId: "device-1" });
  assert.ok(store.load() !== null);

  client.session.clear();
  assert.equal(store.load(), null);
  assert.equal(await client.session.refresh(), false); // 无会话:不发包直接 false
  assert.equal(transport.requests.length, 1); // 只有登录那一次
});

test("session:refresh 非安全事件错误上抛(不误清场)", async () => {
  const transport = new FakeTransport();
  const store = new MemoryTokenStore();
  const client = new CourierClient({ config, transport, tokenStore: store, sleep: async () => {} });
  transport.enqueue(200, successRaw(sessionJson));
  await client.identity.guest({ deviceId: "device-1" });

  for (let i = 0; i < 3; i++) { // 退避三次后放弃
    transport.enqueue(503, JSON.stringify({
      error: { code: "COMMON_UNAVAILABLE", message: "busy", retryable: true },
    }));
  }
  await assert.rejects(() => client.session.refresh());
  assert.ok(store.load() !== null); // 非重放/吊销事件:会话保留,不清场
});

test("session:listDevices 与 unbindDevice 走 Bearer;deviceId URL 编码", async () => {
  const transport = new FakeTransport();
  const client = new CourierClient({ config, transport, sleep: async () => {} });
  transport.enqueue(200, successRaw(sessionJson));
  await client.identity.guest({ deviceId: "device-1" });

  transport.enqueue(200, success({ items: [
    { id: "row_1", deviceId: "device-1", platform: "IOS", createdAt: "t" },
  ], nextCursor: "" }));
  const list = await client.session.listDevices();
  assert.equal(list.items[0].id, "row_1");
  assert.equal(transport.requests[1].url, "https://api.example.com/v1/identity/devices");
  assert.equal(transport.requests[1].headers["Authorization"], "Bearer access-1");

  transport.enqueue(200, JSON.stringify({ data: null }));
  await client.session.unbindDevice("设备/1 2");
  assert.equal(transport.requests[2].method, "DELETE");
  assert.equal(transport.requests[2].url,
    "https://api.example.com/v1/identity/devices/" + encodeURIComponent("设备/1 2"));
  assert.equal(transport.requests[2].headers["Authorization"], "Bearer access-1");
});

test("session:adopt 落库并报账号(切号事件面)", () => {
  const store = new MemoryTokenStore();
  const client = new CourierClient({
    config, transport: new FakeTransport(), tokenStore: store, sleep: async () => {},
  });
  const dto = JSON.parse(sessionJson) as SessionDto;
  client.session.adopt(dto);
  assert.equal(store.load()?.account?.id, "acc_1");
  client.session.adopt({ ...dto, account: undefined }); // 无账号:报 null
  assert.equal(store.load()?.account, undefined);
});