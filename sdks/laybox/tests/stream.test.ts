// M2 流式传输测试(零依赖,node:test):编排(帧上抛/心跳静默/断线退避重连/
// Authorization 每连现取/501/401 终态不重试/Retry-After 优先/健康连重置退避/
// 连接失败重试/净停)+ 退避纯函数 + FetchSseSource 真服务器 e2e(chunked 分框)。
// 运行:node --test tests/stream.test.ts
import assert from "node:assert/strict";
import { test } from "node:test";
import { createServer, type IncomingMessage, type Server, type ServerResponse } from "node:http";
import { SseCloseReasons, FetchSseSource, sseBackoffDelayMs,
  type SseStreamConnection, type SseStreamRequest, type SseSource } from "../src/core/sseStream.ts";
import { TransportError } from "../src/core/transport.ts";
import { SseStreamClient } from "../src/service/sseStreamClient.ts";

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

/** 等 promise 落定(不限 resolve/reject):防未净停的 run 循环遗留后台自旋。 */
async function awaitSettled(p: Promise<unknown>, ms = 5000): Promise<void> {
  let done = false;
  p.then(() => { done = true; }, () => { done = true; });
  await waitUntil(() => done, ms);
}

/** 脚本化连接:按序回放行(string)或抛异常(TransportError);行尽 = 流尾。 */
class FakeConnection implements SseStreamConnection {
  readonly statusCode: number;
  readonly headers: Record<string, string>;
  closed = false;
  private readonly script: (string | TransportError)[];

  constructor(statusCode: number, script: (string | TransportError)[] = [],
    headers: Record<string, string> = {}) {
    this.statusCode = statusCode;
    this.headers = headers;
    this.script = [...script];
  }

  async readLine(): Promise<string | null> {
    const item = this.script.shift();
    if (item === undefined) {
      return null; // 流尾 = 断流
    }
    if (item instanceof TransportError) {
      throw item;
    }
    return item;
  }

  close(): void {
    this.closed = true;
  }
}

/** 脚本化接缝:按序回放连接(FakeConnection / TransportError);记录每次请求头。 */
class FakeSource implements SseSource {
  readonly requests: SseStreamRequest[] = [];
  readonly connections: (SseStreamConnection | TransportError)[] = [];
  connectCount = 0;

  async connect(request: SseStreamRequest): Promise<SseStreamConnection> {
    this.connectCount++;
    this.requests.push(request);
    const next = this.connections.shift();
    if (next === undefined) {
      throw new TransportError("script exhausted");
    }
    if (next instanceof TransportError) {
      throw next;
    }
    return next;
  }
}

function request(headers?: Record<string, string>): SseStreamRequest {
  return {
    url: "http://gw.example/v1/messages/stream",
    headers: headers ?? { "X-Courier-Game-Id": "game_demo" },
    tokenProvider: null,
  };
}

// ---- 退避纯函数(契约 messages.md 语义规则 3) ----

test("sseBackoffDelayMs doubles, caps at max, Retry-After wins", () => {
  assert.equal(sseBackoffDelayMs(1, 0, 1000, 60000), 1000);
  assert.equal(sseBackoffDelayMs(2, 0, 1000, 60000), 2000);
  assert.equal(sseBackoffDelayMs(3, 0, 1000, 60000), 4000);
  assert.equal(sseBackoffDelayMs(4, 0, 1000, 60000), 8000);
  assert.equal(sseBackoffDelayMs(7, 0, 1000, 60000), 60000); // 封顶
  assert.equal(sseBackoffDelayMs(99, 0, 1000, 60000), 60000);
  assert.equal(sseBackoffDelayMs(1, 7000, 1000, 60000), 7000); // Retry-After 优先
  assert.equal(sseBackoffDelayMs(1, 0, 500, 60000), 500);
  assert.equal(sseBackoffDelayMs(1, 0, 90000, 60000), 60000); // base 超 max 钳制
  assert.equal(sseBackoffDelayMs(0, 0, 0, 0), 1); // 全零配置不挂死
});

// ---- 编排(契约 messages.md 客户端要求) ----

test("run surfaces frames, heartbeat silent, reconnect refreshes token", async () => {
  const source = new FakeSource();
  let token = 0;
  const conn1 = new FakeConnection(200,
    ["id: 1", "event: announcement.published", "data: {\"id\":\"ann_1\"}", "", ": ping", ""]);
  const conn2 = new FakeConnection(200,
    ["id: 2", "event: config.updated", "data: {\"configVersion\":7}", ""]);
  source.connections.push(conn1, conn2);
  const client = new SseStreamClient({ source, sleep: yieldSleep });
  const events: { type: string; id?: string; data: string }[] = [];
  const reasons: string[] = [];
  const req = request();
  req.tokenProvider = () => "tok-" + ++token;
  const controller1 = new AbortController();

  const run = client.run(req, (e) => events.push(e), (r) => reasons.push(r), controller1.signal);
  await waitUntil(() => events.length >= 1);
  assert.equal(events[0].type, "announcement.published");
  assert.equal(events[0].id, "1");
  assert.equal(events[0].data, "{\"id\":\"ann_1\"}");
  await waitUntil(() => events.length >= 2);
  assert.equal(events[1].type, "config.updated");
  await waitUntil(() => source.requests.length >= 2);

  assert.ok(conn1.closed); // 断流后连接必须释放
  assert.equal(source.requests[0].headers["Authorization"], "Bearer tok-1"); // 首连现取
  assert.equal(source.requests[1].headers["Authorization"], "Bearer tok-2"); // 重连再取(轮换)
  assert.equal(source.requests[1].headers["X-Courier-Game-Id"], "game_demo"); // scope 头透传
  assert.deepEqual(reasons, []); // 断线是重连语义,非终态

  // 净停:取消后循环退出,连接释放,不再发终态
  const controller = new AbortController();
  const run2 = client.run(request(), (e) => events.push(e), (r) => reasons.push(r), controller.signal);
  await waitUntil(() => source.connectCount >= 3);
  controller.abort();
  await awaitSettled(run2);
  controller1.abort();
  await awaitSettled(run); // 首个 run 亦须净停,不留后台自旋
  assert.deepEqual(reasons, []);
});

test("run 501 capability_disabled terminal, no retry", async () => {
  const source = new FakeSource();
  source.connections.push(new FakeConnection(501));
  const client = new SseStreamClient({ source, sleep: yieldSleep });
  const reasons: string[] = [];
  await client.run(request(), () => {}, (r) => reasons.push(r));

  assert.deepEqual(reasons, [SseCloseReasons.CapabilityDisabled]); // 转纯拉取信号
  assert.equal(source.connectCount, 1); // 不重试
});

test("run 401 unauthenticated terminal, no retry", async () => {
  const source = new FakeSource();
  source.connections.push(new FakeConnection(401));
  const client = new SseStreamClient({ source, sleep: yieldSleep });
  const reasons: string[] = [];
  await client.run(request(), () => {}, (r) => reasons.push(r));

  assert.deepEqual(reasons, [SseCloseReasons.Unauthenticated]);
  assert.equal(source.connectCount, 1);
});

test("run non-200 honors Retry-After, applies once then backoff resumes", async () => {
  const source = new FakeSource();
  source.connections.push(new FakeConnection(429, [], { "Retry-After": "7" }));
  const sleeps: number[] = []; // 捕获实参:注入 sleep 立即返回,client.lastDelayMs 只留最新值
  const client = new SseStreamClient({
    source,
    sleep: async (ms) => {
      sleeps.push(ms);
      await yieldSleep();
    },
  });
  const controller = new AbortController();
  let run: Promise<void> | undefined;
  try {
    run = client.run(request(), () => {}, () => {}, controller.signal);
    await waitUntil(() => sleeps.length >= 2);
    assert.equal(sleeps[0], 7000); // Retry-After 秒 → 毫秒,优先于退避序列
    assert.equal(sleeps[1], 2000); // 仅对本次响应生效,后续回到翻倍序列
  } finally {
    controller.abort();
    if (run) await awaitSettled(run);
  }
});

test("run connect failure retries, then stopped path", async () => {
  const source = new FakeSource();
  source.connections.push(new TransportError("net down")); // 建连网络故障
  source.connections.push(new FakeConnection(200,
    ["id: 9", "event: support.ticket_replied", "data: {}", ""]));
  const client = new SseStreamClient({ source, sleep: yieldSleep });
  const events: { type: string }[] = [];
  let connectAtFirstEvent = -1;
  const controller = new AbortController();

  const run = client.run(request(), (e) => {
    if (connectAtFirstEvent < 0) {
      connectAtFirstEvent = source.connectCount; // 自旋会继续计数,事件点才确定
    }
    events.push(e);
  }, () => {}, controller.signal);
  await waitUntil(() => events.length >= 1); // 故障后重连成功
  assert.equal(events[0].type, "support.ticket_replied");
  assert.equal(connectAtFirstEvent, 2); // 首事件恰在第 2 次建连
  controller.abort();
  await awaitSettled(run);
});

test("run cancelled before start, no connect", async () => {
  const source = new FakeSource();
  const client = new SseStreamClient({ source, sleep: yieldSleep });
  const controller = new AbortController();
  controller.abort();
  await client.run(request(), () => {}, () => {}, controller.signal);
  assert.equal(source.connectCount, 0);
});

test("run:非法 Retry-After 落退避;request/conn 头缺省归一空头集", async () => {
  const source = new FakeSource();
  const bare: SseStreamConnection = {
    statusCode: 429, headers: undefined as never, // 真机防御:头回调前恒缺省
    readLine: async () => null, close() {},
  };
  source.connections.push(
    new FakeConnection(429, [], { "Retry-After": "abc" }), // 非法值 → 0 → 退避序列
    bare, // headers 缺省 → lowerKeys ?? {}
  );
  const sleeps: number[] = [];
  const client = new SseStreamClient({
    source, backoffBaseMs: 500,
    sleep: async (ms) => { sleeps.push(ms); await yieldSleep(); },
  });
  const controller = new AbortController();
  let run: Promise<void> | undefined;
  try {
    run = client.run({ url: "http://gw.example/v1/messages/stream", tokenProvider: () => "t" },
      () => {}, () => {}, controller.signal); // request 无 headers 字段
    await waitUntil(() => sleeps.length >= 2);
    assert.equal(sleeps[0], 500); // 非法 Retry-After 归 0 → 退避序列
    assert.equal(sleeps[1], 1000); // 第二连退避翻倍
    assert.deepEqual(source.requests[0].headers, { Authorization: "Bearer t" }); // 空头集仅 Bearer
  } finally {
    controller.abort();
    if (run) await awaitSettled(run);
  }
});

test("run:readLine 抛非 TransportError 原样上抛(不吞错重连)", async () => {
  const source = new FakeSource();
  const boom: SseStreamConnection = {
    statusCode: 200, headers: {},
    readLine: async () => { throw new Error("decoder boom"); },
    close() {},
  };
  source.connections.push(boom);
  const client = new SseStreamClient({ source, sleep: yieldSleep });
  await assert.rejects(
    () => client.run(request(), () => {}, () => {}),
    /decoder boom/,
  );
});

test("run:建连中取消(connect 抛错且 signal 已取消)净停不发终态", async () => {
  const controller = new AbortController();
  const cancellingSource: SseSource = {
    connect: async () => {
      controller.abort(); // 建连竞态:抛错前信号已取消
      throw new TransportError("torn during connect");
    },
  };
  const client = new SseStreamClient({ source: cancellingSource, sleep: yieldSleep });
  const reasons: string[] = [];
  await client.run(request(), () => {}, (r) => reasons.push(r), controller.signal);
  assert.deepEqual(reasons, []); // 净停
});

test("run:未注入 sleep 走 default 退避,abort 即拒(退避中取消净停);缺省构造绑全局 fetch", () => {
  assert.ok(new FetchSseSource()); // fetchImpl ?? globalThis.fetch:缺省支可达

  const source = new FakeSource();
  source.connections.push(new TransportError("down"));
  const client = new SseStreamClient({ source, backoffBaseMs: 50 });
  const controller = new AbortController();
  const run = client.run(request(), () => {}, () => {}, controller.signal);
  setTimeout(() => controller.abort(), 10); // 退避(50ms)中取消
  return awaitSettled(run).then(() => {
    assert.equal(source.connectCount, 1); // 净停:不再重连
  });
});

test("run:事件回调内取消,行未读尽 → 循环顶检查净停(连接释放,不重连)", async () => {
  const source = new FakeSource();
  const conn = new FakeConnection(200, ["event: a", "data: 1", "", "event: b", "data: 2"]);
  source.connections.push(conn);
  const client = new SseStreamClient({ source, sleep: yieldSleep });
  const controller = new AbortController();
  let events = 0;
  const run = client.run(request(), () => {
    events++;
    controller.abort(); // 行还有剩:取消在下一轮循环顶被检查
  }, () => {}, controller.signal);
  await awaitSettled(run);
  assert.equal(events, 1);
  assert.ok(conn.closed);
  assert.equal(source.connectCount, 1); // 净停:不重连
});

test("run:读行抛 TransportError 中途断流 → 重连;abort 与断流竞态 → 净停", async () => {
  const source = new FakeSource();
  source.connections.push(
    new FakeConnection(200, ["event: a", "data: 1", "", new TransportError("torn mid-stream")]),
    new FakeConnection(200, ["event: b", "data: 2", ""]),
  );
  const client = new SseStreamClient({ source, sleep: yieldSleep });
  const events: { type: string }[] = [];
  const controller = new AbortController();
  const run = client.run(request(), (e) => events.push(e), () => {}, controller.signal);
  await waitUntil(() => events.length >= 2);
  assert.equal(events[1].type, "b"); // 断流后重连续传
  controller.abort();
  await awaitSettled(run);

  // 竞态:readLine 抛 TransportError 时信号已取消 → 净停(Stopped)不发终态
  const raceSource = new FakeSource();
  const raceConn: SseStreamConnection = {
    statusCode: 200, headers: {},
    readLine: async () => {
      controller.abort();
      throw new TransportError("torn");
    },
    close() {},
  };
  raceSource.connections.push(raceConn);
  const reasons: string[] = [];
  const client2 = new SseStreamClient({ source: raceSource, sleep: yieldSleep });
  const run2 = client2.run(request(), () => {}, (r) => reasons.push(r), controller.signal);
  await awaitSettled(run2);
  assert.deepEqual(reasons, []);
});

test("run:建连返回前取消 + 429 → default 退避入口即拒(净停不发终态)", async () => {
  const controller = new AbortController();
  const source: SseSource = {
    connect: async () => {
      controller.abort(); // 建连期间取消,连接仍带回 429
      return { statusCode: 429, headers: {}, readLine: async () => null, close() {} };
    },
  };
  const client = new SseStreamClient({ source, backoffBaseMs: 60000 }); // 未注入 sleep:default
  const reasons: string[] = [];
  await client.run(request(), () => {}, (r) => reasons.push(r), controller.signal);
  assert.deepEqual(reasons, []); // 退避入口检 signal.aborted 即拒
});

// ---- FetchSseSource 真服务器 e2e(chunked 分框 + 断流重连 + 501) ----

interface SseServerHandle {
  server: Server;
  port: number;
  requests: IncomingMessage[];
}

async function startSseServer(handler: (reqNo: number, req: IncomingMessage, res: ServerResponse) => void):
  Promise<SseServerHandle> {
  const requests: IncomingMessage[] = [];
  const server = createServer((req, res) => {
    requests.push(req);
    handler(requests.length, req, res);
  });
  await new Promise<void>((r) => server.listen(0, "127.0.0.1", r));
  const address = server.address();
  if (address === null || typeof address === "string") {
    throw new Error("no server address");
  }
  return { server, port: address.port, requests };
}

async function stopServer(handle: SseServerHandle): Promise<void> {
  handle.server.closeAllConnections();
  await new Promise<void>((r) => handle.server.close(r));
}

test("FetchSseSource real chunked stream: frames surfaced, stream-end reconnects with fresh token", async () => {
  const handle = await startSseServer((_n, _req, res) => {
    res.writeHead(200, { "Content-Type": "text/event-stream" });
    res.write("id: 1\nevent: announcement.published\ndata: {\"id\":\"ann_1\"}\n\n");
    res.write(": ping\n\n"); // 心跳注释:不出事件
    res.write("id: 2\nevent: config.updated\ndata: {\"configVersion\":7}\n\n");
    res.end(); // 流尾 → 客户端退避重连
  });
  const controller = new AbortController();
  let run: Promise<void> | undefined;
  try {
    let token = 0;
    const client = new SseStreamClient({ source: new FetchSseSource(), sleep: yieldSleep });
    const events: { type: string; id?: string; data: string }[] = [];
    const req: SseStreamRequest = {
      url: `http://127.0.0.1:${handle.port}/v1/messages/stream`,
      headers: { "X-Courier-Game-Id": "game_demo" },
      tokenProvider: () => "tok-" + ++token,
    };
    run = client.run(req, (e) => events.push(e), () => {}, controller.signal);
    await waitUntil(() => events.length >= 2);
    assert.equal(events[0].type, "announcement.published");
    assert.equal(events[1].type, "config.updated");
    await waitUntil(() => handle.requests.length >= 2); // 断流后重连
    assert.equal(handle.requests[0].headers.authorization, "Bearer tok-1");
    assert.equal(handle.requests[1].headers.authorization, "Bearer tok-2"); // 重连现取
    assert.equal(handle.requests[0].headers.accept, "text/event-stream");
  } finally {
    controller.abort(); // 净停先于一切失败路径,防 run 循环遗留后台自旋
    await stopServer(handle);
    if (run) await awaitSettled(run);
  }
});

test("FetchSseSource real 501: capability_disabled terminal", async () => {
  const handle = await startSseServer((_n, _req, res) => {
    res.writeHead(501, { "Content-Type": "application/json" });
    res.end("{\"error\":{\"code\":\"COMMON_CAPABILITY_DISABLED\"}}");
  });
  try {
    const client = new SseStreamClient({ source: new FetchSseSource(), sleep: yieldSleep });
    const reasons: string[] = [];
    await client.run({
      url: `http://127.0.0.1:${handle.port}/v1/messages/stream`,
      headers: {},
      tokenProvider: null,
    }, () => {}, (r) => reasons.push(r));
    assert.deepEqual(reasons, [SseCloseReasons.CapabilityDisabled]);
    assert.equal(handle.requests.length, 1);
  } finally {
    await stopServer(handle);
  }
});

// ---- 补口:构造守卫 / 退避取消净停 / 非 TransportError 上抛 / defaultSleep ----

test("SseStreamClient:source/request 必填守卫", async () => {
  assert.throws(() => new SseStreamClient({} as never), /source required/);
  const source = new FakeSource();
  const client = new SseStreamClient({ source });
  await assert.rejects(() => client.run(null as never, () => {}), /request required/);
});

test("SseStreamClient:退避 sleep 被拒 → 净停返回(无终态回调)", async () => {
  const source = new FakeSource();
  source.connections.push(new TransportError("down"), new TransportError("down"));
  let closed: string | null = null;
  let slept = 0;
  const client = new SseStreamClient({
    source,
    backoffBaseMs: 1000,
    sleep: async () => {
      slept++;
      throw new TransportError("cancelled");
    },
  });
  await client.run(request(), () => {}, (reason) => { closed = reason; });
  assert.equal(slept, 1);
  assert.equal(closed, null); // 退避中取消非终态,不上报收尾原因
  assert.ok(client.lastDelayMs > 0);
});

test("SseStreamClient:连接抛非 TransportError 原样上抛", async () => {
  const source = {
    connect: async () => {
      throw new Error("boom");
    },
  };
  const client = new SseStreamClient({ source: source as never });
  await assert.rejects(() => client.run(request(), () => {}), /boom/);
});

test("SseStreamClient:defaultSleep 定时放行重连(无 sleep 选项)", async () => {
  const source = new FakeSource();
  for (let i = 0; i < 5; i++) {
    source.connections.push(new TransportError("down"));
  }
  const client = new SseStreamClient({ source, backoffBaseMs: 1 });
  const controller = new AbortController();
  setTimeout(() => controller.abort(), 30); // 数次毫秒级重连后取消
  await client.run(request(), () => {}, null, controller.signal);
  assert.ok(source.connectCount >= 2, "sleep 放行后应重连,实连 " + source.connectCount);
});
