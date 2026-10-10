// 微信小程序适配器测试(零依赖):fake wx 注入工厂;storage 落盘、
// request wire 归一、前后台/断网信号幂等挂起恢复、与 core 全链组合。
import assert from "node:assert/strict";
import { test } from "node:test";
import { CourierClient } from "../src/core/courierClient.ts";
import { LifecycleMachine, LifecycleState, LifecycleTrigger } from "../src/core/lifecycle.ts";
import { TransportError } from "../src/core/transport.ts";
import { WechatLifecycleMonitor } from "../src/platform/wechat/lifecycleMonitor.ts";
import { createWechatTokenStore } from "../src/platform/wechat/tokenStore.ts";
import { createWechatTransport } from "../src/platform/wechat/transport.ts";
import type { WxRequestOptions } from "../src/platform/wechat/wxApi.ts";

const sessionDto = {
  account: { id: "acc_1", type: "GUEST", status: "ACTIVE", createdAt: "t" },
  accessToken: "access-1", accessExpiresAt: "t",
  refreshToken: "refresh-1", refreshExpiresAt: "t", deviceId: "device-1",
};

// ---- fake wx(结构类型鸭子匹配 WechatApi) ----

interface ScriptItem {
  statusCode?: number;
  header?: Record<string, string>;
  data?: unknown;
  errMsg?: string;
}

class FakeWx {
  storage = new Map<string, string>();
  requests: WxRequestOptions[] = [];
  script: ScriptItem[] = [];
  handlers: {
    show?: () => void;
    hide?: () => void;
    net?: (res: { isConnected: boolean }) => void;
  } = {};

  getStorageSync(key: string): string {
    return this.storage.get(key) ?? "";
  }
  setStorageSync(key: string, value: string): void {
    this.storage.set(key, value);
  }
  removeStorageSync(key: string): void {
    this.storage.delete(key);
  }
  request(options: WxRequestOptions): { abort(): void } {
    this.requests.push(options);
    const next = this.script.shift() ?? { errMsg: "request:fail script exhausted" };
    queueMicrotask(() => {
      if (next.errMsg !== undefined) {
        options.fail({ errMsg: next.errMsg });
      } else {
        options.success({
          statusCode: next.statusCode ?? 200,
          header: next.header ?? {},
          data: next.data,
        });
      }
    });
    return { abort: () => {} };
  }
  onAppShow(callback: () => void): void {
    this.handlers.show = callback;
  }
  onAppHide(callback: () => void): void {
    this.handlers.hide = callback;
  }
  onNetworkStatusChange(callback: (res: { isConnected: boolean }) => void): void {
    this.handlers.net = callback;
  }
}

const sessionJson = JSON.stringify(sessionDto);

// ---- TokenStore(wx storage) ----

test("wx tokenStore:save/load 往返;clear 移除;缺失返回 null", () => {
  const wx = new FakeWx();
  const store = createWechatTokenStore(wx);

  assert.equal(store.load(), null); // 空存储
  store.save(sessionDto);
  assert.equal(wx.storage.get("courier.session"), sessionJson); // 落盘为 JSON 字符串
  assert.deepEqual(store.load(), sessionDto);

  store.clear();
  assert.equal(wx.storage.get("courier.session"), undefined);
  assert.equal(store.load(), null);
});

test("wx tokenStore:自定义 key 前缀;损坏数据清场返回 null", () => {
  const wx = new FakeWx();
  const store = createWechatTokenStore(wx, "courier.session.dev");
  assert.equal(store.load(), null);

  wx.storage.set("courier.session.dev", "{not-json");
  assert.equal(store.load(), null);
  assert.equal(wx.storage.get("courier.session.dev"), undefined); // 已清场
});

// ---- Transport(wx.request) ----

test("wx transport:wire 形状(url/method/header/body/timeout);响应头小写归一", async () => {
  const wx = new FakeWx();
  const transport = createWechatTransport(wx);
  wx.script.push({
    statusCode: 200,
    header: { "Content-Type": "application/json", "X-Trace-Id": "t-1" },
    data: '{"data":{"ok":true}}',
  });

  const resp = await transport.send({
    method: "POST",
    url: "https://api.example.com/v1/identity/guest",
    headers: { "X-Courier-Game-Id": "game_demo", "Content-Type": "application/json" },
    jsonBody: '{"deviceId":"device-1"}',
    timeoutMs: 5000,
  });

  const req = wx.requests[0];
  assert.equal(req.url, "https://api.example.com/v1/identity/guest");
  assert.equal(req.method, "POST");
  assert.deepEqual(req.header, {
    "X-Courier-Game-Id": "game_demo", "Content-Type": "application/json",
  }); // 请求头原样透传
  assert.equal(req.data, '{"deviceId":"device-1"}');
  assert.equal(req.timeout, 5000);

  assert.equal(resp.statusCode, 200);
  assert.equal(resp.headers["content-type"], "application/json"); // 小写归一
  assert.equal(resp.headers["x-trace-id"], "t-1");
  assert.equal(resp.body, '{"data":{"ok":true}}');
});

test("wx transport:对象响应重新序列化;失败归一 TransportError;缺省超时 10s", async () => {
  const wx = new FakeWx();
  const transport = createWechatTransport(wx);

  wx.script.push({ statusCode: 200, data: { data: { ok: true } } }); // wx 默认解析 JSON
  const parsed = await transport.send({
    method: "GET", url: "https://api.example.com/x", headers: {}, timeoutMs: 0,
  });
  assert.equal(parsed.body, '{"data":{"ok":true}}');
  assert.equal(wx.requests[0].timeout, 10000); // timeoutMs<=0 → 缺省

  wx.script.push({ errMsg: "request:fail timeout" });
  await assert.rejects(
    () => transport.send({ method: "GET", url: "https://api.example.com/x", headers: {}, timeoutMs: 10 }),
    (e: unknown) => e instanceof TransportError && /timeout/.test(e.message),
  );
});

// ---- LifecycleMonitor(onShow/onHide/网络信号) ----

function playerReadyMachine(): { machine: LifecycleMachine; events: string[] } {
  const machine = new LifecycleMachine();
  const events: string[] = [];
  machine.onEvent((e) => events.push(e.type));
  return { machine, events };
}

test("wx monitor:hide 挂起/ show 恢复回 PlayerReady;成对信号幂等去重", () => {
  const wx = new FakeWx();
  const { machine, events } = playerReadyMachine();
  machine.fire(LifecycleTrigger.InitStarted);
  machine.fire(LifecycleTrigger.InitCompleted);
  machine.fire(LifecycleTrigger.AuthStarted);
  machine.fire(LifecycleTrigger.AuthSucceeded);
  machine.fire(LifecycleTrigger.EnterPlayerReady);

  const monitor = new WechatLifecycleMonitor(machine);
  monitor.bind(wx);

  wx.handlers.hide?.();
  assert.equal(machine.current, LifecycleState.Suspended);
  wx.handlers.hide?.(); // 重复 hide:幂等,不再发事件
  wx.handlers.net?.({ isConnected: false }); // 网络断:已挂起,幂等

  wx.handlers.show?.();
  assert.equal(machine.current, LifecycleState.PlayerReady); // 回挂起前状态
  wx.handlers.show?.(); // 重复 show:幂等
  wx.handlers.net?.({ isConnected: true }); // 网络恢复:已恢复,幂等

  assert.deepEqual(events, [
    "lifecycle.initialized", "lifecycle.suspended", "lifecycle.resumed",
  ]);
});

test("wx monitor:断网挂起/恢复;未认证态(Ready)挂起被状态机否决,不粘死", () => {
  const wx = new FakeWx();
  const machine = new LifecycleMachine();
  const events: string[] = [];
  machine.onEvent((e) => events.push(e.type));
  machine.fire(LifecycleTrigger.InitStarted);
  machine.fire(LifecycleTrigger.InitCompleted); // Ready:未认证无挂起语义

  const monitor = new WechatLifecycleMonitor(machine);
  monitor.bind(wx);

  wx.handlers.net?.({ isConnected: false }); // Ready--Suspended--> 非法,tryFire 否决
  assert.equal(machine.current, LifecycleState.Ready);
  wx.handlers.show?.(); // suspended 标志未置位,恢复信号无副作用

  wx.handlers.hide?.(); // 仍非法
  assert.equal(machine.current, LifecycleState.Ready);
  assert.deepEqual(events, ["lifecycle.initialized"]);

  // 认证后挂起/恢复照常工作(否决没有粘死后续合法信号)。
  machine.fire(LifecycleTrigger.AuthStarted);
  machine.fire(LifecycleTrigger.AuthSucceeded);
  machine.fire(LifecycleTrigger.EnterPlayerReady);
  wx.handlers.hide?.();
  assert.equal(machine.current, LifecycleState.Suspended);
  wx.handlers.show?.();
  assert.equal(machine.current, LifecycleState.PlayerReady);
  assert.deepEqual(events, [
    "lifecycle.initialized", "lifecycle.suspended", "lifecycle.resumed",
  ]);
});

// ---- 全链组合(wechat 适配器 × core) ----

test("wx 全链:CourierClient(wechat store+transport)+ monitor,登录落盘微信 storage", async () => {
  const wx = new FakeWx();
  const client = new CourierClient({
    config: { endpoint: "https://api.example.com", gameId: "game_demo", env: "prod" },
    transport: createWechatTransport(wx),
    tokenStore: createWechatTokenStore(wx),
    sleep: async () => {},
  });
  const events: string[] = [];
  client.lifecycle.onEvent((e) => events.push(e.type));
  new WechatLifecycleMonitor(client.lifecycle).bind(wx);

  wx.script.push({ statusCode: 200, data: { data: sessionDto } }); // wx 对象形态响应
  client.init();
  const session = await client.guestLoginAsync({ deviceId: "device-1" });

  assert.equal(session.accessToken, "access-1");
  assert.equal(client.lifecycle.current, LifecycleState.PlayerReady);
  assert.deepEqual(wx.requests[0].header["X-Courier-Game-Id"], "game_demo"); // scope 头
  assert.equal(wx.storage.get("courier.session"), sessionJson); // 会话落盘微信 storage
  assert.deepEqual(events, ["lifecycle.initialized"]);

  // 切后台挂起 → 状态机事件;冷启动重开:新 client 从 wx storage 恢复会话。
  wx.handlers.hide?.();
  assert.equal(client.lifecycle.current, LifecycleState.Suspended);
  wx.handlers.show?.();

  const reopened = new CourierClient({
    config: { endpoint: "https://api.example.com", gameId: "game_demo", env: "prod" },
    transport: createWechatTransport(wx),
    tokenStore: createWechatTokenStore(wx),
    sleep: async () => {},
  });
  assert.equal(reopened.session.accessToken(), "access-1"); // 冷启动恢复
  assert.equal(reopened.lifecycle.current, LifecycleState.Uninitialized); // 状态机不跨进程
});
