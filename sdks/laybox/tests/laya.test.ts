// Layabox 平台适配器测试(零依赖):fake 注入工厂;storage 落盘、XHR wire 归一、
// 可见性/断网信号幂等挂起恢复、与 core 全链组合。
import assert from "node:assert/strict";
import { test } from "node:test";
import { CourierClient } from "../src/core/courierClient.ts";
import { LifecycleMachine, LifecycleState, LifecycleTrigger } from "../src/core/lifecycle.ts";
import { TransportError } from "../src/core/transport.ts";
import { LayaboxLifecycleMonitor } from "../src/platform/laya/lifecycleMonitor.ts";
import { createLayaboxTokenStore } from "../src/platform/laya/tokenStore.ts";
import { createLayaboxTransport } from "../src/platform/laya/transport.ts";
import type { XhrLike } from "../src/platform/laya/layaApi.ts";

const sessionDto = {
  account: { id: "acc_1", type: "GUEST", status: "ACTIVE", createdAt: "t" },
  accessToken: "access-1", accessExpiresAt: "t",
  refreshToken: "refresh-1", refreshExpiresAt: "t", deviceId: "device-1",
};
const sessionJson = JSON.stringify(sessionDto);

// ---- fake 平台面(结构类型鸭子匹配) ----

class FakeStorage {
  store = new Map<string, string>();
  getItem(key: string): string | null {
    return this.store.get(key) ?? null;
  }
  setItem(key: string, value: string): void {
    this.store.set(key, value);
  }
  removeItem(key: string): void {
    this.store.delete(key);
  }
}

interface XhrScript {
  status?: number;
  rawHeaders?: string;
  body?: string;
  errMsg?: string; // 有值 = onerror;errMsg = "timeout" 走 ontimeout
}

class FakeXhr implements XhrLike {
  method = "";
  url = "";
  headers: Record<string, string> = {};
  sentBody: string | null = null;
  timeout = 0;
  status = 0;
  responseText = "";
  rawHeaders = "";
  script: XhrScript[] = [];

  onload: (() => void) | null = null;
  onerror: (() => void) | null = null;
  ontimeout: (() => void) | null = null;

  open(method: string, url: string): void {
    this.method = method;
    this.url = url;
  }
  setRequestHeader(name: string, value: string): void {
    this.headers[name] = value;
  }
  getAllResponseHeaders(): string {
    return this.rawHeaders;
  }
  send(body?: string | null): void {
    this.sentBody = body ?? null;
    const item = this.script.shift() ?? { errMsg: "fail" };
    queueMicrotask(() => {
      if (item.errMsg === "timeout") {
        this.ontimeout?.();
      } else if (item.errMsg !== undefined) {
        this.onerror?.();
      } else {
        this.status = item.status ?? 200;
        this.rawHeaders = item.rawHeaders ?? "";
        this.responseText = item.body ?? "";
        this.onload?.();
      }
    });
  }
}

function fakeXhrFactory(script: XhrScript[]): { factory: () => FakeXhr; made: FakeXhr[] } {
  const made: FakeXhr[] = [];
  const factory = (): FakeXhr => {
    const xhr = new FakeXhr();
    xhr.script = script; // 实例间共享同一队列,顺序消费
    made.push(xhr);
    return xhr;
  };
  return { factory, made };
}

class FakeHooks {
  handlers: {
    visibility?: (visible: boolean) => void;
    net?: (isConnected: boolean) => void;
  } = {};
  onVisibilityChange(callback: (visible: boolean) => void): void {
    this.handlers.visibility = callback;
  }
  onNetworkStatusChange(callback: (isConnected: boolean) => void): void {
    this.handlers.net = callback;
  }
}

// ---- TokenStore(LocalStorage) ----

test("laya tokenStore:save/load 往返;clear 移除;损坏数据清场;自定义 key", () => {
  const storage = new FakeStorage();
  const store = createLayaboxTokenStore(storage);

  assert.equal(store.load(), null); // 空存储
  store.save(sessionDto);
  assert.equal(storage.store.get("courier.session"), sessionJson); // 落盘为 JSON 字符串
  assert.deepEqual(store.load(), sessionDto);

  const dev = createLayaboxTokenStore(storage, "courier.session.dev");
  storage.store.set("courier.session.dev", "{not-json");
  assert.equal(dev.load(), null);
  assert.equal(storage.store.get("courier.session.dev"), undefined); // 已清场

  store.clear();
  assert.equal(storage.store.get("courier.session"), undefined);
});

// ---- Transport(XMLHttpRequest) ----

test("laya transport:wire 形状(method/url/header/body/timeout);响应头小写归一", async () => {
  const { factory } = fakeXhrFactory([
    { status: 200, rawHeaders: "Content-Type: application/json\r\nX-Trace-Id: t-1\r\n", body: '{"data":{"ok":true}}' },
  ]);
  const transport = createLayaboxTransport(factory);

  const resp = await transport.send({
    method: "POST",
    url: "https://api.example.com/v1/identity/guest",
    headers: { "X-Courier-Game-Id": "game_demo", "Content-Type": "application/json" },
    jsonBody: '{"deviceId":"device-1"}',
    timeoutMs: 5000,
  });

  assert.equal(resp.statusCode, 200);
  assert.equal(resp.headers["content-type"], "application/json"); // 小写归一
  assert.equal(resp.headers["x-trace-id"], "t-1");
  assert.equal(resp.body, '{"data":{"ok":true}}');
});

test("laya transport:请求头/体/超时透传 XHR;网络失败与超时归一 TransportError", async () => {
  const script: XhrScript[] = [{}, { errMsg: "timeout" }];
  const { factory, made } = fakeXhrFactory(script);
  const transport = createLayaboxTransport(factory);

  await transport.send({
    method: "GET", url: "https://api.example.com/x",
    headers: { "X-Courier-Env": "prod" }, timeoutMs: 8000,
  });
  assert.equal(made[0].method, "GET");
  assert.equal(made[0].url, "https://api.example.com/x");
  assert.equal(made[0].headers["X-Courier-Env"], "prod"); // 请求头原样透传
  assert.equal(made[0].sentBody, null); // 无体请求 send(null)
  assert.equal(made[0].timeout, 8000); // 超时透传

  await assert.rejects(
    () => transport.send({ method: "GET", url: "https://api.example.com/x", headers: {}, timeoutMs: 10 }),
    (e: unknown) => e instanceof TransportError && e.message === "xhr timeout",
  );
  await assert.rejects(
    () => transport.send({ method: "GET", url: "https://api.example.com/x", headers: {}, timeoutMs: 10 }),
    (e: unknown) => e instanceof TransportError && e.message === "xhr network error",
  );
});

// ---- LifecycleMonitor(可见性/网络信号) ----

function playerReadyMachine(): { machine: LifecycleMachine; events: string[] } {
  const machine = new LifecycleMachine();
  const events: string[] = [];
  machine.onEvent((e) => events.push(e.type));
  return { machine, events };
}

test("laya monitor:隐藏挂起/可见恢复回 PlayerReady;成对信号幂等去重", () => {
  const hooks = new FakeHooks();
  const { machine, events } = playerReadyMachine();
  machine.fire(LifecycleTrigger.InitStarted);
  machine.fire(LifecycleTrigger.InitCompleted);
  machine.fire(LifecycleTrigger.AuthStarted);
  machine.fire(LifecycleTrigger.AuthSucceeded);
  machine.fire(LifecycleTrigger.EnterPlayerReady);

  new LayaboxLifecycleMonitor(machine).bind(hooks);

  hooks.handlers.visibility?.(false); // 隐藏
  assert.equal(machine.current, LifecycleState.Suspended);
  hooks.handlers.visibility?.(false); // 重复隐藏:幂等
  hooks.handlers.net?.(false); // 断网:已挂起,幂等

  hooks.handlers.visibility?.(true); // 可见
  assert.equal(machine.current, LifecycleState.PlayerReady); // 回挂起前状态
  hooks.handlers.visibility?.(true); // 重复可见:幂等
  hooks.handlers.net?.(true); // 网络恢复:已恢复,幂等

  assert.deepEqual(events, [
    "lifecycle.initialized", "lifecycle.suspended", "lifecycle.resumed",
  ]);
});

test("laya monitor:未认证态(Ready)挂起被状态机否决,不粘死后续合法信号", () => {
  const hooks = new FakeHooks();
  const machine = new LifecycleMachine();
  const events: string[] = [];
  machine.onEvent((e) => events.push(e.type));
  machine.fire(LifecycleTrigger.InitStarted);
  machine.fire(LifecycleTrigger.InitCompleted); // Ready:未认证无挂起语义

  new LayaboxLifecycleMonitor(machine).bind(hooks);

  hooks.handlers.net?.(false); // 非法,tryFire 否决
  assert.equal(machine.current, LifecycleState.Ready);
  hooks.handlers.visibility?.(false); // 仍非法
  assert.equal(machine.current, LifecycleState.Ready);
  assert.deepEqual(events, ["lifecycle.initialized"]);

  machine.fire(LifecycleTrigger.AuthStarted);
  machine.fire(LifecycleTrigger.AuthSucceeded);
  machine.fire(LifecycleTrigger.EnterPlayerReady);
  hooks.handlers.visibility?.(false);
  assert.equal(machine.current, LifecycleState.Suspended);
  hooks.handlers.visibility?.(true);
  assert.equal(machine.current, LifecycleState.PlayerReady);
});

// ---- 全链组合(laya 适配器 × core) ----

test("laya 全链:CourierClient(laya store+transport)+ monitor,登录落盘 LocalStorage", async () => {
  const storage = new FakeStorage();
  const script: XhrScript[] = [
    { status: 200, rawHeaders: "content-type: application/json\r\n", body: JSON.stringify({ data: sessionDto }) },
  ];
  const { factory } = fakeXhrFactory(script);
  const client = new CourierClient({
    config: { endpoint: "https://api.example.com", gameId: "game_demo", env: "prod" },
    transport: createLayaboxTransport(factory),
    tokenStore: createLayaboxTokenStore(storage),
    sleep: async () => {},
  });
  const events: string[] = [];
  client.lifecycle.onEvent((e) => events.push(e.type));
  new LayaboxLifecycleMonitor(client.lifecycle).bind(new FakeHooks());

  client.init();
  const session = await client.guestLoginAsync({ deviceId: "device-1" });

  assert.equal(session.accessToken, "access-1");
  assert.equal(client.lifecycle.current, LifecycleState.PlayerReady);
  assert.equal(storage.store.get("courier.session"), sessionJson); // 会话落盘
  assert.deepEqual(events, ["lifecycle.initialized"]);

  // 冷启动重开:新 client 从 LocalStorage 恢复会话;状态机不跨进程。
  const reopened = new CourierClient({
    config: { endpoint: "https://api.example.com", gameId: "game_demo", env: "prod" },
    transport: createLayaboxTransport(factory),
    tokenStore: createLayaboxTokenStore(storage),
    sleep: async () => {},
  });
  assert.equal(reopened.session.accessToken(), "access-1");
  assert.equal(reopened.lifecycle.current, LifecycleState.Uninitialized);
});
