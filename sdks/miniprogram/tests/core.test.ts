// L2 Core 测试(零依赖,node:test):wire 形状、scope/Bearer 头、信封解析、
// 重试退避、TOKEN_EXPIRED 自动 refresh 重放、安全事件清场、本地未认证预检。
import assert from "node:assert/strict";
import { test } from "node:test";
import { ApiClient, parseEnvelope } from "../src/core/apiClient.ts";
import { CourierApiError, isCapabilityDisabled } from "../src/core/courierError.ts";
import { CourierClient } from "../src/core/courierClient.ts";
import { MemoryTokenStore } from "../src/core/tokenStore.ts";
import type { Transport, TransportRequest, TransportResponse } from "../src/core/transport.ts";
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

test("refresh 轮换:REVOKED 同走安全清场;网络错误非安全事件照常上抛且会话保留", async () => {
  const transport = new FakeTransport();
  const store = new MemoryTokenStore();
  const client = new CourierClient({ config, transport, tokenStore: store, sleep: async () => {} });
  transport.enqueue(200, successRaw(sessionJson));
  await client.identity.guest({ deviceId: "device-1" });

  transport.enqueueHandler(() => { throw new Error("ECONNRESET"); });
  const e1 = isErr(await client.session.refresh().catch((x) => x));
  assert.equal(e1.wire, "COMMON_UNAVAILABLE"); // 非安全事件:原样上抛
  assert.ok(store.load()); // 会话保留,网络恢复后可重试

  transport.enqueue(401, JSON.stringify({
    error: { code: "AUTH_TOKEN_REVOKED", message: "revoked", retryable: false },
  }));
  assert.equal(await client.session.refresh(), false);
  assert.equal(store.load(), null); // 吊销同走清场(重登)
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
    "accessExpiresAt", "accessToken", "account",
    "deviceId", "refreshExpiresAt", "refreshToken",
  ]);
  assert.equal(s.account?.type, "GUEST");
  assert.ok(!("email" in (s.account as object))); // GUEST 时 email 缺省
});

// --- 注销与撤销(v1.1,auth.md POST /account/delete + /account/delete/cancel)---

test("deleteAccount:Bearer POST account/delete,PENDING 响应带一次性 cancelToken", async () => {
  const transport = new FakeTransport();
  const { client } = newClient(transport);
  transport.enqueue(200, successRaw(sessionJson));
  await client.identity.guest({ deviceId: "device-1" });
  transport.enqueue(200, success({
    status: "PENDING_DELETE",
    cancelToken: "deltok_" + "a".repeat(48),
    pendingDelete: true,
  }));

  const r = await client.identity.deleteAccount();

  const req = transport.requests[1];
  assert.equal(req.method, "POST");
  assert.equal(req.url, "https://api.example.com/v1/identity/account/delete");
  assert.equal(req.headers["Authorization"], "Bearer access-1");
  assert.equal(r.status, "PENDING_DELETE");
  assert.equal(r.pendingDelete, true);
  assert.ok(r.cancelToken);
});

test("cancelDelete:匿名 POST account/delete/cancel 携带 cancelToken", async () => {
  const transport = new FakeTransport();
  const { client } = newClient(transport);
  transport.enqueue(200, successRaw(sessionJson));
  await client.identity.guest({ deviceId: "device-1" });
  transport.enqueue(200, success({}));

  await client.identity.cancelDelete("deltok_x");

  const req = transport.requests[1];
  assert.equal(req.method, "POST");
  assert.equal(req.url, "https://api.example.com/v1/identity/account/delete/cancel");
  assert.ok(!("Authorization" in req.headers)); // 匿名端点
  assert.deepEqual(JSON.parse(req.jsonBody ?? "{}"), { cancelToken: "deltok_x" });
});

// --- 第三方身份(v1.1,oauth.md exchange / bind / 解绑 / 绑定列表)---

test("oauthExchange:匿名 POST oauth/exchange,响应会话落库", async () => {
  const transport = new FakeTransport();
  const store = new MemoryTokenStore();
  const client = new CourierClient({ config, transport, tokenStore: store, sleep: async () => {} });
  transport.enqueue(200, success({
    account: {
      id: "acc_oa", type: "OAUTH", status: "ACTIVE", createdAt: "2026-10-12T00:00:00.000Z",
    },
    accessToken: "access-oa",
    accessExpiresAt: "t",
    refreshToken: "refresh-oa",
    refreshExpiresAt: "t",
    deviceId: "device-1",
  }));

  const s = await client.identity.oauthExchange({
    provider: "google", token: "tok_x", deviceId: "device-1", platform: "ios",
  });

  const req = transport.requests[0];
  assert.equal(req.method, "POST");
  assert.equal(req.url, "https://api.example.com/v1/identity/oauth/exchange");
  assert.ok(!("Authorization" in req.headers)); // 匿名端点
  assert.deepEqual(JSON.parse(req.jsonBody ?? "{}"), {
    provider: "google", token: "tok_x", deviceId: "device-1", platform: "ios",
  });
  assert.equal(s.account?.type, "OAUTH");
  assert.equal(store.load()?.accessToken, "access-oa"); // 会话落库,同 login 口径
});

test("oauthBind/oauthUnbind/oauthBindings:Bearer wire 形状", async () => {
  const transport = new FakeTransport();
  const { client } = newClient(transport);
  transport.enqueue(200, successRaw(sessionJson));
  await client.identity.guest({ deviceId: "device-1" });

  transport.enqueue(200, success({ provider: "google", createdAt: "2026-10-12T00:00:00.000Z" }));
  const b = await client.identity.oauthBind("google", "tok_x");
  assert.equal(transport.requests[1].method, "POST");
  assert.equal(transport.requests[1].url, "https://api.example.com/v1/identity/oauth/bind");
  assert.equal(transport.requests[1].headers["Authorization"], "Bearer access-1");
  assert.deepEqual(JSON.parse(transport.requests[1].jsonBody ?? "{}"),
    { provider: "google", token: "tok_x" });
  assert.equal(b.provider, "google");

  transport.enqueue(200, success({}));
  await client.identity.oauthUnbind("google");
  assert.equal(transport.requests[2].method, "DELETE");
  assert.equal(transport.requests[2].url, "https://api.example.com/v1/identity/oauth/bindings/google");

  transport.enqueue(200, success({
    items: [{ provider: "apple", createdAt: "2026-10-12T00:00:00.000Z" }],
  }));
  const list = await client.identity.oauthBindings();
  assert.equal(transport.requests[3].method, "GET");
  assert.equal(transport.requests[3].url, "https://api.example.com/v1/identity/oauth/bindings");
  assert.equal(list.items[0].provider, "apple");
  assert.ok(!("subject" in list.items[0])); // 不返回 subject,防客户端侧泄露
});

// ---- 补口:客户端门面守卫与 register/refresh 直连 ----

test("init:gameId/env 空白拒绝(scope 头契约;构造已验,init 复核被改坏的配置)", () => {
  const transport = new FakeTransport();
  const client = new CourierClient({
    config: { ...config }, // 克隆:避免污染模块级共享 config
    transport,
    sleep: async () => {},
  });
  (client as unknown as { config: { gameId: string } }).config.gameId = "  ";
  assert.throws(() => client.init(), /gameId\/env 必填/);
});

test("registerAsync:邮箱注册匿名 POST register,会话返回(与 loginAsync 同走 authFlow)", async () => {
  const transport = new FakeTransport();
  const { client } = newClient(transport);
  client.init();
  transport.enqueue(200, successRaw(sessionJson));

  const session = await client.registerAsync({ email: "a@b.co", password: "password8" });

  const req = transport.requests[0];
  assert.equal(req.method, "POST");
  assert.equal(req.url, "https://api.example.com/v1/identity/register");
  assert.equal(JSON.parse(req.jsonBody as string).email, "a@b.co");
  assert.ok(!("Authorization" in req.headers)); // 注册前匿名
  assert.equal(session.accessToken, "access-1");
});

test("identity.refresh:轮换 wire 形状(匿名 POST refresh)", async () => {
  const transport = new FakeTransport();
  const { client } = newClient(transport);
  transport.enqueue(200, successRaw(sessionJson));

  const session = await client.identity.refresh("refresh-old");

  const req = transport.requests[0];
  assert.equal(req.method, "POST");
  assert.equal(req.url, "https://api.example.com/v1/identity/refresh");
  assert.equal(JSON.parse(req.jsonBody as string).refreshToken, "refresh-old");
  assert.ok(!("Authorization" in req.headers)); // 刷新带 refresh token,不带 access
  assert.equal(session.refreshToken, "refresh-1");
});

// --- 补口:parseEnvelope 全分支矩阵、Retry-After 非法值、defaultSleep、refresh 边界 ---

test("parseEnvelope:空体按状态兜底全分支(401/403/404/429/501/其他)", () => {
  const cases: Array<[number, string, boolean]> = [
    [401, "COMMON_UNAUTHENTICATED", false],
    [403, "COMMON_PERMISSION_DENIED", false],
    [404, "COMMON_NOT_FOUND", false],
    [429, "RATE_LIMITED", true],
    [501, "COMMON_CAPABILITY_DISABLED", true], // fb 不可重试,但 5xx 一票可重试
    [418, "COMMON_INTERNAL", false], // 其他:非 5xx/429 → 不可重试
  ];
  for (const [status, wire, retryable] of cases) {
    const e = parseEnvelope({ statusCode: status, headers: {}, body: " \n" }).error;
    assert.ok(e instanceof CourierApiError, String(status));
    assert.equal(e.wire, wire, String(status));
    assert.equal(e.retryable, retryable, String(status));
    assert.match(e.message, new RegExp("HTTP " + status)); // 空白体归一空兜底
  }
});

test("parseEnvelope:信封缺字段降级(空 error/无 message/无 retryable 走登记 spec)", () => {
  // 空 error 对象:code 缺省 ""(未登记),message 缺省 "",retryable 兜底 false
  const e1 = parseEnvelope({ statusCode: 400, headers: {}, body: JSON.stringify({ error: {} }) }).error;
  assert.ok(e1 instanceof CourierApiError);
  assert.equal(e1.wire, "");
  assert.equal(e1.message, "");
  assert.equal(e1.retryable, false);
  assert.equal(e1.traceId, undefined);

  // 无 message、无 retryable:登记码用冻结 spec 的 retryable 兜底
  const denied = parseEnvelope({
    statusCode: 403, headers: {},
    body: JSON.stringify({ error: { code: "COMMON_PERMISSION_DENIED" } }),
  }).error;
  assert.ok(denied instanceof CourierApiError);
  assert.equal(denied.message, ""); // message 缺省
  assert.equal(denied.retryable, false); // spec(COMMON_PERMISSION_DENIED)= false

  // 同路径反向证明 spec 求值:RATE_LIMITED 冻结 retryable=true
  const limited = parseEnvelope({
    statusCode: 429, headers: {},
    body: JSON.stringify({ error: { code: "RATE_LIMITED" } }),
  }).error;
  assert.ok(limited instanceof CourierApiError);
  assert.equal(limited.retryable, true); // 若走 ?? false 兜底则断言失败
});

test("Retry-After 非法值:降级基础间隔退避(非数字/负数)", async () => {
  const transport = new FakeTransport();
  const { client, sleeps } = newClient(transport);
  transport.enqueue(429, JSON.stringify({
    error: { code: "RATE_LIMITED", message: "x", retryable: true },
  }), { "retry-after": "abc" });
  transport.enqueue(429, JSON.stringify({
    error: { code: "RATE_LIMITED", message: "x", retryable: true },
  }), { "retry-after": "-1" });
  transport.enqueue(200, success({ ok: 1 }));

  const data = await client.api.request<{ ok: number }>("GET", "/x", undefined, false);
  assert.deepEqual(data, { ok: 1 });
  assert.deepEqual(sleeps, [300, 300]); // 非法 Retry-After → undefined → baseDelayMs
});

test("默认退避:未注入 sleep 走真实定时器(defaultSleep)", async () => {
  const transport = new FakeTransport();
  const client = new ApiClient({ config, transport, retry: { maxAttempts: 2, baseDelayMs: 1 } });
  transport.enqueue(503, "");
  transport.enqueue(200, success({ ok: 1 }));

  const data = await client.request<{ ok: number }>("GET", "/x", undefined, false);
  assert.deepEqual(data, { ok: 1 });
  assert.equal(transport.requests.length, 2);
});

test("token 预检与过期:无 provider 本地抛;无 refreshHook 过期直抛不重放", async () => {
  const transport = new FakeTransport();
  const bare = new ApiClient({ config, transport });

  const e1 = isErr(await bare.request("GET", "/x").catch((x) => x));
  assert.equal(e1.wire, "COMMON_UNAUTHENTICATED"); // provider 缺省 → 本地预检
  assert.equal(transport.requests.length, 0);

  const withTok = new ApiClient({ config, transport, accessTokenProvider: () => "tok-1" });
  transport.enqueue(401, JSON.stringify({
    error: { code: "AUTH_TOKEN_EXPIRED", message: "expired", retryable: false },
  }));
  const e2 = isErr(await withTok.request("GET", "/x").catch((x) => x));
  assert.equal(e2.wire, "AUTH_TOKEN_EXPIRED"); // 无 refreshHook:不重放,直抛
  assert.equal(transport.requests[0].headers["Authorization"], "Bearer tok-1");
});

test("refresh 重放边界:二次过期不再 refresh;refresh 后 token 为空 Bearer 不动", async () => {
  const transport = new FakeTransport();
  let token: string | null = "tok-1";
  const client = new ApiClient({
    config, transport,
    accessTokenProvider: () => token,
    refreshHook: async () => { token = null; return true; }, // 刷新"成功"但现取为空
    sleep: async () => {},
  });
  transport.enqueue(401, JSON.stringify({
    error: { code: "AUTH_TOKEN_EXPIRED", message: "expired", retryable: false },
  }));
  transport.enqueue(401, JSON.stringify({
    error: { code: "AUTH_TOKEN_EXPIRED", message: "expired again", retryable: false },
  }));

  const e = isErr(await client.request("GET", "/x").catch((x) => x));
  assert.equal(e.wire, "AUTH_TOKEN_EXPIRED"); // 二次过期:refreshed 已置位,直抛
  assert.equal(transport.requests.length, 2); // 只重放一次
  assert.equal(transport.requests[1].headers["Authorization"], "Bearer tok-1"); // token 空:Bearer 不更新
});

test("refresh 重放:无 accessTokenProvider(匿名请求)hook 仍生效,Bearer 恒不设", async () => {
  const transport = new FakeTransport();
  let refreshCalls = 0;
  const client = new ApiClient({
    config, transport,
    refreshHook: async () => { refreshCalls++; return true; },
    sleep: async () => {},
  });
  transport.enqueue(401, JSON.stringify({
    error: { code: "AUTH_TOKEN_EXPIRED", message: "expired", retryable: false },
  }));
  transport.enqueue(200, success({ ok: 1 }));

  const data = await client.request<{ ok: number }>("GET", "/x", undefined, false); // 匿名:无 provider
  assert.deepEqual(data, { ok: 1 });
  assert.equal(refreshCalls, 1);
  assert.ok(transport.requests.every((r) => !("Authorization" in r.headers))); // token null:不加 Bearer
});

test("transport 抛错归一:CourierApiError 原样透传;非 Error 值归一 COMMON_UNAVAILABLE", async () => {
  const transport = new FakeTransport();
  const client = new ApiClient({ config, transport, sleep: async () => {} });
  const boom = new CourierApiError("AUTH_TOKEN_REVOKED", "revoked", 401, false);
  transport.enqueueHandler(() => { throw boom; });

  const e1 = isErr(await client.request("GET", "/x", undefined, false).catch((x) => x));
  assert.equal(e1, boom); // transport 已归一的契约错误:透传不重包

  const rawBoom = { send: async () => { throw "socket torn"; } };
  const rawClient = new ApiClient({ config, transport: rawBoom as never, sleep: async () => {} });
  const e2 = isErr(await rawClient.request("GET", "/x", undefined, false).catch((x) => x));
  assert.equal(e2.wire, "COMMON_UNAVAILABLE");
  assert.match(e2.message, /socket torn/); // 非 Error 值走 String(e)
});

test("parseEnvelope:非 JSON 404 → fb 不可重试且状态 <500,归一不可重试", () => {
  const e = parseEnvelope({ statusCode: 404, headers: {}, body: "not json" }).error;
  assert.ok(e instanceof CourierApiError);
  assert.equal(e.wire, "COMMON_NOT_FOUND");
  assert.equal(e.retryable, false); // fb.retryable=false || 404>=500=false
});
