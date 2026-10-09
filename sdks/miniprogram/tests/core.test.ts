// L2 Core 测试(零依赖,node:test):wire 形状、scope/Bearer 头、信封解析、
// 重试退避、TOKEN_EXPIRED 自动 refresh 重放、安全事件清场、本地未认证预检。
import assert from "node:assert/strict";
import { test } from "node:test";
import { ApiClient } from "../src/core/apiClient.ts";
import { CourierApiError, isCapabilityDisabled } from "../src/core/courierError.ts";
import { CourierClient } from "../src/core/courierClient.ts";
import { MemoryTokenStore } from "../src/core/tokenStore.ts";
import type { Transport, TransportRequest, TransportResponse } from "../src/core/transport.ts";
import type { SessionDto } from "../src/core/types.ts";

const config = { endpoint: "https://api.example.com", gameId: "game_demo", env: "prod" };
const sessionJson = JSON.stringify({
  accountId: "acc_1",
  account: { id: "acc_1", type: "GUEST", status: "ACTIVE", createdAt: "2026-10-09T12:00:00.000Z" },
  accessToken: "access-1",
  accessExpiresAt: "2026-10-09T12:15:00.000Z",
  refreshToken: "refresh-1",
  refreshExpiresAt: "2026-10-10T12:00:00.000Z",
  deviceId: "device-1",
});
const success = (data: unknown) => JSON.stringify({ data });
const successRaw = (raw: string) => '{"data":' + raw + "}"; // 裸 DTO 字符串直接包信封

/** 脚本化假传输:记录请求,按队列回放响应(同 unity FakeTransport)。 */
class FakeTransport implements Transport {
  requests: TransportRequest[] = [];
  private queue: Array<(req: TransportRequest) => TransportResponse> = [];

  enqueue(statusCode: number, body: string, headers?: Record<string, string>): void {
    this.queue.push(() => ({ statusCode, headers: headers ?? {}, body }));
  }

  enqueueHandler(handler: (req: TransportRequest) => TransportResponse): void {
    this.queue.push(handler);
  }

  async send(request: TransportRequest): Promise<TransportResponse> {
    this.requests.push(request);
    const next = this.queue.shift();
    if (!next) throw new Error("脚本耗尽:" + request.method + " " + request.url);
    return next(request);
  }
}

/** 记录型睡眠(不真等;断言退避间隔)。 */
function fakeSleep() {
  const sleeps: number[] = [];
  return {
    sleeps,
    fn: async (ms: number) => {
      sleeps.push(ms);
    },
  };
}

function newClient(transport: FakeTransport) {
  const { sleeps, fn } = fakeSleep();
  const client = new CourierClient({
    config,
    transport,
    sleep: fn,
  });
  return { client, sleeps };
}

const isErr = (e: unknown): CourierApiError => {
  assert.ok(e instanceof CourierApiError, "应抛 CourierApiError,实得 " + String(e));
  return e;
};

test("guest:wire 形状(scope 头/JSON 体/无 Bearer)+ 会话落库", async () => {
  const transport = new FakeTransport();
  const { client } = newClient(transport);
  transport.enqueue(200, successRaw(sessionJson));

  const session = await client.identity.guest({ deviceId: "device-1", platform: "android" });

  const req = transport.requests[0];
  assert.equal(req.method, "POST");
  assert.equal(req.url, "https://api.example.com/v1/identity/guest");
  assert.equal(req.headers["X-Courier-Game-Id"], "game_demo");
  assert.equal(req.headers["X-Courier-Env"], "prod");
  assert.ok(!("Authorization" in req.headers));
  assert.equal(req.jsonBody, '{"deviceId":"device-1","platform":"android"}');
  assert.equal(session.accessToken, "access-1");
  assert.equal(client.session.current()?.refreshToken, "refresh-1"); // 已落库
});

test("Bearer:认证请求自动附带,匿名请求不带", async () => {
  const transport = new FakeTransport();
  const { client } = newClient(transport);
  transport.enqueue(200, successRaw(sessionJson));
  await client.identity.guest({ deviceId: "device-1" });

  transport.enqueue(200, success({ sessionId: "ses_1", deviceId: "device-1" }));
  transport.enqueue(200, successRaw(sessionJson));
  const info = await client.session.info();
  const refreshed = await client.identity.guest({ deviceId: "device-2" });

  assert.equal(transport.requests[1].headers["Authorization"], "Bearer access-1");
  assert.equal(info.sessionId, "ses_1");
  assert.ok(!("Authorization" in transport.requests[2].headers)); // guest 匿名
  assert.equal(refreshed.accessToken, "access-1"); // 响应为准(同设备回同账号)
  assert.deepEqual(JSON.parse(transport.requests[2].jsonBody ?? "{}"), { deviceId: "device-2" });
});

test("未认证预检:withAuth 无会话本地抛,不发包", async () => {
  const transport = new FakeTransport();
  const { client } = newClient(transport);

  const e = isErr(await client.session.info().catch((x) => x));
  assert.equal(e.wire, "COMMON_UNAUTHENTICATED");
  assert.equal(e.code, "CommonUnauthenticated");
  assert.equal(transport.requests.length, 0);
});

test("失败信封:typed 错误(wire/枚举/http/retryable/traceId)", async () => {
  const transport = new FakeTransport();
  const { client } = newClient(transport);
  transport.enqueue(404, JSON.stringify({
    error: { code: "ANNOUNCEMENT_NOT_FOUND", message: "gone", retryable: false },
    traceId: "4bf92f3577b34da6a3ce929d0e0e4736",
  }));

  const e = isErr(await client.api.request("GET", "/v1/announcements/x", undefined, false).catch((x) => x));
  assert.equal(e.wire, "ANNOUNCEMENT_NOT_FOUND");
  assert.equal(e.code, "AnnouncementNotFound");
  assert.equal(e.http, 404);
  assert.equal(e.retryable, false);
  assert.equal(e.traceId, "4bf92f3577b34da6a3ce929d0e0e4736");
  assert.match(e.message, /gone/);
});

test("空 body/非 JSON:按状态兜底 typed(503 → COMMON_UNAVAILABLE)", async () => {
  const transport = new FakeTransport();
  const { client } = newClient(transport);
  transport.enqueue(503, "");
  transport.enqueue(502, "<html>bad gateway</html>");

  const e1 = isErr(await client.api.request("GET", "/x", undefined, false).catch((x) => x));
  assert.equal(e1.wire, "COMMON_UNAVAILABLE");
  assert.equal(e1.retryable, true);
  const e2 = isErr(await client.api.request("GET", "/x", undefined, false).catch((x) => x));
  assert.equal(e2.wire, "COMMON_UNAVAILABLE");
});

test("重试:retryable(RATE_LIMITED)按 Retry-After 退避后成功;非 retryable 一次即抛", async () => {
  const transport = new FakeTransport();
  const { client, sleeps } = newClient(transport);
  transport.enqueue(429, JSON.stringify({
    error: { code: "RATE_LIMITED", message: "slow down", retryable: true },
    traceId: "t".repeat(32),
  }), { "retry-after": "2" });
  transport.enqueue(200, success({ ok: true }));
  transport.enqueue(404, JSON.stringify({
    error: { code: "COMMON_NOT_FOUND", message: "nope", retryable: false },
  }));

  const data = await client.api.request<{ ok: boolean }>("GET", "/x", undefined, false);
  assert.deepEqual(data, { ok: true });
  assert.equal(transport.requests.length, 2);
  assert.deepEqual(sleeps, [2000]); // Retry-After(秒)→ 毫秒

  const e = isErr(await client.api.request("GET", "/x", undefined, false).catch((x) => x));
  assert.equal(e.wire, "COMMON_NOT_FOUND");
  assert.equal(transport.requests.length, 3); // 不重试
});

test("重试耗尽:maxAttempts 次后抛最后一次错误", async () => {
  const transport = new FakeTransport();
  const { client, sleeps } = newClient(transport);
  for (let i = 0; i < 3; i++) {
    transport.enqueue(503, JSON.stringify({
      error: { code: "COMMON_UNAVAILABLE", message: "down", retryable: true },
    }));
  }

  const e = isErr(await client.api.request("GET", "/x", undefined, false).catch((x) => x));
  assert.equal(e.wire, "COMMON_UNAVAILABLE");
  assert.equal(transport.requests.length, 3);
  assert.equal(sleeps.length, 2); // 3 次尝试间 2 次退避
});

test("网络失败:映射 COMMON_UNAVAILABLE 可重试,恢复后成功", async () => {
  const transport = new FakeTransport();
  const { client } = newClient(transport);
  transport.enqueueHandler(() => {
    throw new Error("ECONNRESET");
  });
  transport.enqueue(200, success({ ok: 1 }));

  const data = await client.api.request<{ ok: number }>("GET", "/x", undefined, false);
  assert.deepEqual(data, { ok: 1 });
  assert.equal(transport.requests.length, 2);
});

test("TOKEN_EXPIRED:自动 refresh 一次后重放(新 Bearer),hook 只走一次", async () => {
  const transport = new FakeTransport();
  const store = new MemoryTokenStore();
  const { sleeps } = fakeSleep();
  const client = new CourierClient({ config, transport, tokenStore: store, sleep: async () => {} });
  transport.enqueue(200, successRaw(sessionJson));
  await client.identity.guest({ deviceId: "device-1" });

  let refreshCalls = 0;
  transport.enqueue(401, JSON.stringify({
    error: { code: "AUTH_TOKEN_EXPIRED", message: "expired", retryable: false },
  }));
  transport.enqueueHandler((req) => {
    refreshCalls++;
    assert.equal(req.url, "https://api.example.com/v1/identity/refresh");
    assert.deepEqual(JSON.parse(req.jsonBody ?? "{}"), { refreshToken: "refresh-1" });
    return { statusCode: 200, headers: {}, body: successRaw(sessionJson.replace("access-1", "access-2").replace("refresh-1", "refresh-2")) };
  });
  transport.enqueueHandler((req) => {
    assert.equal(req.headers["Authorization"], "Bearer access-2"); // 重放带新 token
    return { statusCode: 200, headers: {}, body: success({ ok: true }) };
  });

  const data = await client.api.request<{ ok: boolean }>("GET", "/v1/announcements");
  assert.deepEqual(data, { ok: true });
  assert.equal(refreshCalls, 1);
  assert.equal(store.load()?.accessToken, "access-2"); // 轮换已落库
  assert.equal(sleeps.length, 0); // refresh 重放不占退避次数
});

test("refresh 失败:重放放弃,原 401 抛出", async () => {
  const transport = new FakeTransport();
  const client = new CourierClient({ config, transport, sleep: async () => {} });
  transport.enqueue(200, successRaw(sessionJson));
  await client.identity.guest({ deviceId: "device-1" });

  transport.enqueue(401, JSON.stringify({
    error: { code: "AUTH_TOKEN_EXPIRED", message: "expired", retryable: false },
  }));
  transport.enqueue(401, JSON.stringify({
    error: { code: "AUTH_REFRESH_REUSED", message: "replay", retryable: false },
  }));
  transport.enqueue(200, success({ ok: true })); // 不会被消费

  const e = isErr(await client.api.request("GET", "/x", undefined, false).catch((x) => x));
  assert.equal(e.wire, "AUTH_TOKEN_EXPIRED");
  assert.equal(transport.requests.length, 3);
});

test("安全事件:refresh REUSED/REVOKED → 本地清场(重登)", async () => {
  const transport = new FakeTransport();
  const store = new MemoryTokenStore();
  const client = new CourierClient({ config, transport, tokenStore: store, sleep: async () => {} });
  transport.enqueue(200, successRaw(sessionJson));
  await client.identity.guest({ deviceId: "device-1" });

  transport.enqueue(401, JSON.stringify({
    error: { code: "AUTH_REFRESH_REUSED", message: "replay", retryable: false },
  }));
  const ok = await client.session.refresh();

  assert.equal(ok, false);
  assert.equal(store.load(), null); // 清场,需重登
});

test("refresh 单飞:并发共享一次轮换", async () => {
  const transport = new FakeTransport();
  const store = new MemoryTokenStore();
  const client = new CourierClient({ config, transport, tokenStore: store, sleep: async () => {} });
  transport.enqueue(200, successRaw(sessionJson));
  await client.identity.guest({ deviceId: "device-1" });

  transport.enqueue(200, successRaw(sessionJson.replace("access-1", "access-9")));
  const [a, b] = await Promise.all([client.session.refresh(), client.session.refresh()]);
  assert.equal(a && b, true);
  assert.equal(transport.requests.length, 2); // guest + 一次 refresh
});

test("登出:吊销尽力而为,本地必定清场", async () => {
  const transport = new FakeTransport();
  const store = new MemoryTokenStore();
  const client = new CourierClient({ config, transport, tokenStore: store, sleep: async () => {} });
  transport.enqueue(200, successRaw(sessionJson));
  await client.identity.guest({ deviceId: "device-1" });

  transport.enqueue(200, JSON.stringify({ data: null }));
  await client.session.logout();
  assert.equal(store.load(), null);

  // 服务端失败也清本地(尽力而为)
  transport.enqueue(200, successRaw(sessionJson));
  await client.identity.guest({ deviceId: "device-1" });
  for (let i = 0; i < 3; i++) {
    transport.enqueue(500, JSON.stringify({
      error: { code: "COMMON_INTERNAL", message: "boom", retryable: true },
    }));
  }
  await client.session.logout();
  assert.equal(store.load(), null);
});

test("bind:Bearer 请求,返回账号不落会话", async () => {
  const transport = new FakeTransport();
  const store = new MemoryTokenStore();
  const client = new CourierClient({ config, transport, tokenStore: store, sleep: async () => {} });
  transport.enqueue(200, successRaw(sessionJson));
  await client.identity.guest({ deviceId: "device-1" });

  transport.enqueue(200, success({
    id: "acc_1", type: "EMAIL", email: "a@b.c", status: "ACTIVE",
    createdAt: "2026-10-09T12:00:00.000Z",
  }));
  const account = await client.identity.bind({ email: "a@b.c", password: "pw" });

  assert.equal(transport.requests[1].method, "POST");
  assert.equal(transport.requests[1].headers["Authorization"], "Bearer access-1");
  assert.deepEqual(JSON.parse(transport.requests[1].jsonBody ?? "{}"),
    { email: "a@b.c", password: "pw" });
  assert.equal(account.type, "EMAIL");
  assert.equal(store.load()?.accessToken, "access-1"); // 会话未被覆盖
});

test("501 判定:isCapabilityDisabled 命中降级信号", async () => {
  const transport = new FakeTransport();
  const { client } = newClient(transport);
  transport.enqueue(501, JSON.stringify({
    error: { code: "COMMON_CAPABILITY_DISABLED", message: "not configured", retryable: false },
  }));

  const e = await client.api.request("GET", "/x", undefined, false).catch((x) => x);
  assert.ok(isCapabilityDisabled(e));
  assert.ok(!isCapabilityDisabled(new Error("x")));
  assert.ok(!isCapabilityDisabled(undefined));
});

test("endpoint 尾斜杠归一 + 配置校验(gameId/env 必填)", async () => {
  const transport = new FakeTransport();
  const client = new CourierClient({
    config: { ...config, endpoint: "https://api.example.com/" },
    transport,
    sleep: async () => {},
  });
  transport.enqueue(200, success({}));
  await client.api.request("GET", "/x", undefined, false).catch(() => undefined);
  assert.equal(transport.requests[0].url, "https://api.example.com/x");

  assert.throws(() => new CourierClient({
    config: { ...config, gameId: " " }, transport, sleep: async () => {},
  }), /gameId\/env 必填/);
});

test("未登记 wire code 容忍:原样透传 + 兜底 retryable=false", async () => {
  const transport = new FakeTransport();
  const { client } = newClient(transport);
  transport.enqueue(400, JSON.stringify({
    error: { code: "FUTURE_CODE_X", message: "new server", retryable: false },
  }));

  const e = isErr(await client.api.request("GET", "/x", undefined, false).catch((x) => x));
  assert.equal(e.wire, "FUTURE_CODE_X");
  assert.equal(e.code, undefined); // 未知落兜底分支
  assert.equal(e.retryable, false);
});

test("会话 DTO 全字段 wire 对齐(auth.md)", async () => {
  const transport = new FakeTransport();
  const { client } = newClient(transport);
  transport.enqueue(200, successRaw(sessionJson));

  const s = (await client.identity.guest({ deviceId: "device-1" })) as SessionDto;
  assert.deepEqual(Object.keys(s).sort(), [
    "accessExpiresAt", "accessToken", "account", "accountId",
    "deviceId", "refreshExpiresAt", "refreshToken",
  ]);
  assert.equal(s.account?.type, "GUEST");
  assert.ok(!("email" in (s.account as object))); // GUEST 时 email 缺省
});
