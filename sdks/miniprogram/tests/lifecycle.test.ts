// 生命周期状态机全事件单测(批次 4 unity LifecycleMachineTests 同构):
// 全合法迁移表 + 非法迁移拒绝 + 契约事件面 + 挂起恢复回跳 + 会话域接线。
import assert from "node:assert/strict";
import { test } from "node:test";
import { CourierClient } from "../src/core/courierClient.ts";
import {
  LifecycleMachine,
  LifecycleState,
  LifecycleTrigger,
} from "../src/core/lifecycle.ts";
import type { LifecycleEvent } from "../src/core/lifecycle.ts";
import type { Transport, TransportRequest, TransportResponse } from "../src/core/transport.ts";

const config = { endpoint: "https://api.example.com", gameId: "game_demo", env: "prod" };
const sessionJson = JSON.stringify({
  account: { id: "acc_1", type: "GUEST", status: "ACTIVE", createdAt: "t" },
  accessToken: "access-1", accessExpiresAt: "t",
  refreshToken: "refresh-1", refreshExpiresAt: "t", deviceId: "device-1",
});

class Recorder {
  readonly events: LifecycleEvent[] = [];
  private readonly off: () => void;
  constructor(machine: LifecycleMachine) {
    this.off = machine.onEvent((e) => this.events.push(e));
  }
  get types(): string[] {
    return this.events.map((e) => e.type);
  }
  dispose(): void {
    this.off();
  }
}

// 构造到指定状态(unity MachineAt 同构,合法快路径)。
function machineAt(state: string): LifecycleMachine {
  const m = new LifecycleMachine();
  if (state === "Uninitialized") return m;
  m.fire(LifecycleTrigger.InitStarted);
  if (state === "Initializing") return m;
  m.fire(LifecycleTrigger.InitCompleted);
  if (state === "Ready") return m;
  m.fire(LifecycleTrigger.AuthStarted);
  if (state === "Authenticating") return m;
  m.fire(LifecycleTrigger.AuthSucceeded);
  if (state === "Authenticated") return m;
  m.fire(LifecycleTrigger.EnterPlayerReady);
  if (state === "PlayerReady") return m;
  if (state === "SignedOut") {
    m.fire(LifecycleTrigger.SignedOut); // 直接从 PlayerReady 登出
    return m;
  }
  m.fire(LifecycleTrigger.Suspended);
  if (state === "Suspended") return m;
  m.fire(LifecycleTrigger.ResumeStarted);
  if (state === "Resuming") return m;
  throw new Error("未知构造态: " + state);
}

// ---- 全合法迁移表(architecture.md 状态图) ----

const fullTable: Array<[string, LifecycleTrigger, string]> = [
  ["Uninitialized", LifecycleTrigger.InitStarted, "Initializing"],
  ["Initializing", LifecycleTrigger.InitCompleted, "Ready"],
  ["Ready", LifecycleTrigger.AuthStarted, "Authenticating"],
  ["Authenticating", LifecycleTrigger.AuthSucceeded, "Authenticated"],
  ["Authenticating", LifecycleTrigger.AuthFailed, "Ready"],
  ["Authenticated", LifecycleTrigger.EnterPlayerReady, "PlayerReady"],
  ["Authenticated", LifecycleTrigger.SignedOut, "SignedOut"],
  ["Authenticated", LifecycleTrigger.Suspended, "Suspended"],
  ["PlayerReady", LifecycleTrigger.SignedOut, "SignedOut"],
  ["PlayerReady", LifecycleTrigger.Suspended, "Suspended"],
  ["Suspended", LifecycleTrigger.ResumeStarted, "Resuming"],
  ["Resuming", LifecycleTrigger.ResumeCompleted, "PlayerReady"], // 挂起于 PlayerReady
  ["SignedOut", LifecycleTrigger.AuthStarted, "Authenticating"],
];

for (const [from, trigger, to] of fullTable) {
  test(`迁移:${from} --${trigger}--> ${to}`, () => {
    const m = machineAt(from);
    m.fire(trigger);
    assert.equal(m.current, to);
  });
}

test("恢复:回到挂起前状态(Authenticated 挂起 → 恢复回 Authenticated)", () => {
  const m = machineAt("Authenticated");
  m.fire(LifecycleTrigger.Suspended);
  m.fire(LifecycleTrigger.ResumeStarted);
  m.fire(LifecycleTrigger.ResumeCompleted);
  assert.equal(m.current, LifecycleState.Authenticated);
});

// ---- 非法迁移拒绝(状态不被非法触发破坏) ----

const invalidTable: Array<[string, LifecycleTrigger]> = [
  ["Ready", LifecycleTrigger.Suspended], // 未认证无挂起语义
  ["Uninitialized", LifecycleTrigger.AuthStarted],
  ["Uninitialized", LifecycleTrigger.InitCompleted],
  ["Ready", LifecycleTrigger.EnterPlayerReady], // 必须先认证
  ["PlayerReady", LifecycleTrigger.AuthStarted],
  ["PlayerReady", LifecycleTrigger.InitCompleted],
  ["SignedOut", LifecycleTrigger.Suspended],
  ["Authenticating", LifecycleTrigger.SignedOut],
];

for (const [from, trigger] of invalidTable) {
  test(`非法迁移:${from} --${trigger}--> 拒绝`, () => {
    const m = machineAt(from);
    assert.throws(() => m.fire(trigger), /invalid lifecycle transition/);
    assert.equal(m.current, from);
  });
}

// ---- 契约事件面 ----

test("契约事件:仅 init/suspend/resume/signout 对外;auth 三触发不发事件", () => {
  const m = new LifecycleMachine();
  const rec = new Recorder(m);
  m.fire(LifecycleTrigger.InitStarted);
  m.fire(LifecycleTrigger.InitCompleted);
  m.fire(LifecycleTrigger.AuthStarted);
  m.fire(LifecycleTrigger.AuthSucceeded);
  m.fire(LifecycleTrigger.EnterPlayerReady);
  m.fire(LifecycleTrigger.Suspended);
  m.fire(LifecycleTrigger.ResumeStarted);
  m.fire(LifecycleTrigger.ResumeCompleted);
  m.fire(LifecycleTrigger.SignedOut);
  // 契约 events.md:AuthStarted/Succeeded/EnterPlayerReady 不是契约生命周期事件。
  assert.deepEqual(rec.types, [
    "lifecycle.initialized", "lifecycle.suspended", "lifecycle.resumed", "lifecycle.signed_out",
  ]);
  assert.equal(rec.events[1].from, LifecycleState.PlayerReady); // suspended from
  assert.equal(rec.events[1].to, LifecycleState.Suspended);
  rec.dispose();
});

test("token_expired:事件发出但状态不变", () => {
  const m = machineAt("PlayerReady");
  const rec = new Recorder(m);
  m.raiseTokenExpired();
  assert.deepEqual(rec.types, ["lifecycle.token_expired"]);
  assert.equal(m.current, LifecycleState.PlayerReady);
});

test("account_switched:首登不发、切号发、同号重登不发", () => {
  const m = new LifecycleMachine();
  const rec = new Recorder(m);
  assert.equal(m.reportAccountId("acc_A"), false); // 首次登录不算切换
  assert.deepEqual(rec.types, []);
  assert.equal(m.reportAccountId("acc_B"), true); // 切号
  assert.deepEqual(rec.types, ["lifecycle.account_switched"]);
  assert.equal(m.reportAccountId("acc_B"), false); // 同号重登不算切换
});

test("onEvent 退订后不再接收;监听器内新订阅不被当前分发吞掉", () => {
  const m = new LifecycleMachine();
  const seen: string[] = [];
  const off = m.onEvent((e) => seen.push(e.type));
  off();
  m.onEvent((e) => seen.push("nested:" + e.type));
  m.fire(LifecycleTrigger.InitStarted);
  m.fire(LifecycleTrigger.InitCompleted);
  assert.deepEqual(seen, ["nested:lifecycle.initialized"]);
});

// ---- TryFire(Adapter 平台信号幂等,非法转移不抛) ----

test("tryFire:合法迁移与 fire 等价,恢复回挂起前状态", () => {
  const m = machineAt("PlayerReady");
  assert.equal(m.tryFire(LifecycleTrigger.Suspended), true);
  assert.equal(m.current, LifecycleState.Suspended);
  assert.equal(m.tryFire(LifecycleTrigger.ResumeStarted), true);
  assert.equal(m.tryFire(LifecycleTrigger.ResumeCompleted), true);
  assert.equal(m.current, LifecycleState.PlayerReady); // 回挂起前状态
});

test("tryFire:非法迁移返回 false、状态不变、不发事件", () => {
  const m = machineAt("PlayerReady");
  const rec = new Recorder(m);
  assert.equal(m.tryFire(LifecycleTrigger.AuthStarted), false); // PlayerReady 不可再登录
  assert.equal(m.tryFire(LifecycleTrigger.ResumeStarted), false); // 未挂起不可恢复
  assert.equal(m.current, LifecycleState.PlayerReady);
  assert.deepEqual(rec.types, []);
});

// ---- CourierClient 接线(init/登录流/会话域事件) ----

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

function newClient(transport: FakeTransport): CourierClient {
  return new CourierClient({ config, transport, sleep: async () => {} });
}

test("client:init 推进 Uninitialized→Ready 并发契约事件;重复 init 非法", () => {
  const transport = new FakeTransport();
  const client = newClient(transport);
  const rec = new Recorder(client.lifecycle);
  assert.equal(client.lifecycle.current, LifecycleState.Uninitialized);

  client.init();
  assert.equal(client.lifecycle.current, LifecycleState.Ready);
  assert.deepEqual(rec.types, ["lifecycle.initialized"]);

  assert.throws(() => client.init(), /invalid lifecycle transition/);
  assert.equal(client.lifecycle.current, LifecycleState.Ready);
});

test("client:guestLoginAsync 全流 Ready→PlayerReady;未 init 拒绝登录", async () => {
  const transport = new FakeTransport();
  const client = newClient(transport);
  const rec = new Recorder(client.lifecycle);

  // 未 init:状态守卫拒绝,不发包(登录要求 Ready/SignedOut)。
  await assert.rejects(() => client.guestLoginAsync({ deviceId: "device-1" }),
    /login requires state Ready\/SignedOut/);
  assert.equal(transport.requests.length, 0);

  transport.enqueue(200, '{"data":' + sessionJson + "}");
  client.init();
  const session = await client.guestLoginAsync({ deviceId: "device-1" });
  assert.equal(session.accessToken, "access-1");
  assert.equal(client.lifecycle.current, LifecycleState.PlayerReady);
  // 首登只有 initialized(首登不算切号);auth 三触发无契约事件。
  assert.deepEqual(rec.types, ["lifecycle.initialized"]);
});

test("client:登出后换账号登录 → account_switched(切号检测)", async () => {
  const transport = new FakeTransport();
  const client = newClient(transport);
  const rec = new Recorder(client.lifecycle);
  transport.enqueue(200, '{"data":' + sessionJson + "}"); // acc_1
  client.init();
  await client.guestLoginAsync({ deviceId: "device-1" });

  transport.enqueue(200, '{"data":null}');
  await client.session.logout(); // PlayerReady → SignedOut

  const acc2 = JSON.parse(sessionJson) as { account: { id: string }; accessToken: string };
  acc2.account.id = "acc_2";
  acc2.accessToken = "access-9";
  transport.enqueue(200, '{"data":' + JSON.stringify(acc2) + "}");
  await client.guestLoginAsync({ deviceId: "device-1" }); // SignedOut → 重登

  assert.deepEqual(rec.types, [
    "lifecycle.initialized", "lifecycle.signed_out", "lifecycle.account_switched",
  ]);
  assert.equal(client.lifecycle.current, LifecycleState.PlayerReady);
});

test("client:登录失败 AuthFailed 回退 Ready,可重试", async () => {
  const transport = new FakeTransport();
  const client = newClient(transport);
  client.init();
  transport.enqueue(401, JSON.stringify({
    error: { code: "AUTH_INVALID_CREDENTIALS", message: "bad", retryable: false },
  }));

  await assert.rejects(() => client.loginAsync({ email: "a@b.c", password: "x" }),
    (e: { wire?: string }) => e.wire === "AUTH_INVALID_CREDENTIALS");
  assert.equal(client.lifecycle.current, LifecycleState.Ready);

  transport.enqueue(200, '{"data":' + sessionJson + "}");
  await client.loginAsync({ email: "a@b.c", password: "right" });
  assert.equal(client.lifecycle.current, LifecycleState.PlayerReady);
});

test("client:access 过期自动 refresh 发 token_expired;轮换成功回到 PlayerReady", async () => {
  const transport = new FakeTransport();
  const client = newClient(transport);
  const rec = new Recorder(client.lifecycle);
  transport.enqueue(200, '{"data":' + sessionJson + "}");
  client.init();
  await client.guestLoginAsync({ deviceId: "device-1" });

  const refreshed = JSON.stringify({
    account: { id: "acc_1", type: "GUEST", status: "ACTIVE", createdAt: "t" },
    accessToken: "access-2", accessExpiresAt: "t",
    refreshToken: "refresh-2", refreshExpiresAt: "t", deviceId: "device-1",
  });
  transport.enqueue(401, JSON.stringify({
    error: { code: "AUTH_TOKEN_EXPIRED", message: "expired", retryable: false },
  }));
  transport.enqueue(200, '{"data":' + refreshed + "}"); // 刷新
  transport.enqueue(200, '{"data":{"ok":true}}'); // 重放原请求

  await client.session.info();
  assert.deepEqual(rec.types, ["lifecycle.initialized", "lifecycle.token_expired"]);
  assert.equal(client.lifecycle.current, LifecycleState.PlayerReady);
  assert.equal(client.session.accessToken(), "access-2");
});

test("client:refresh 重放检测 → 清场 + signed_out(PlayerReady→SignedOut 可重登)", async () => {
  const transport = new FakeTransport();
  const client = newClient(transport);
  const rec = new Recorder(client.lifecycle);
  transport.enqueue(200, '{"data":' + sessionJson + "}");
  client.init();
  await client.guestLoginAsync({ deviceId: "device-1" });

  transport.enqueue(401, JSON.stringify({
    error: { code: "AUTH_TOKEN_EXPIRED", message: "expired", retryable: false },
  }));
  transport.enqueue(401, JSON.stringify({
    error: { code: "AUTH_REFRESH_REUSED", message: "reused", retryable: false },
  }));
  // 刷新失败:无重放,原 401 照抛(core 既有口径)。

  await client.session.info().catch(() => undefined);
  assert.deepEqual(rec.types, [
    "lifecycle.initialized", "lifecycle.token_expired", "lifecycle.signed_out",
  ]);
  assert.equal(client.lifecycle.current, LifecycleState.SignedOut);
  assert.equal(client.session.accessToken(), null);

  // SignedOut 可再登录(回到 PlayerReady)。
  transport.enqueue(200, '{"data":' + sessionJson + "}");
  await client.guestLoginAsync({ deviceId: "device-1" });
  assert.equal(client.lifecycle.current, LifecycleState.PlayerReady);
});

test("client:logout 尽力而为清场 + signed_out;重复 logout 状态不变", async () => {
  const transport = new FakeTransport();
  const client = newClient(transport);
  const rec = new Recorder(client.lifecycle);
  transport.enqueue(200, '{"data":' + sessionJson + "}");
  client.init();
  await client.guestLoginAsync({ deviceId: "device-1" });

  transport.enqueue(200, '{"data":null}');
  await client.session.logout();

  assert.deepEqual(rec.types.slice(-1), ["lifecycle.signed_out"]);
  assert.equal(client.lifecycle.current, LifecycleState.SignedOut);
  assert.equal(client.session.accessToken(), null);

  // 已 SignedOut:再 logout 不重复发事件(SignedOut--SignedOut--> 非法,tryFire 否决)。
  transport.enqueue(200, '{"data":null}');
  await client.session.logout();
  assert.deepEqual(rec.types.slice(-1), ["lifecycle.signed_out"]);
  assert.equal(client.lifecycle.current, LifecycleState.SignedOut);
});
