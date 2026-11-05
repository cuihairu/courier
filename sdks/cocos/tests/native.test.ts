// Cocos 原生/Web 适配器测试(零依赖):fake localStorage 注入工厂;
// storage 落盘、损坏清场、自定义 key、读取异常降级、与 core 全链组合。
import assert from "node:assert/strict";
import { test } from "node:test";
import { CourierClient } from "../src/core/courierClient.ts";
import { LifecycleState } from "../src/core/lifecycle.ts";
import { createCocosTokenStore } from "../src/platform/native/tokenStore.ts";
import type { CocosStorage } from "../src/platform/native/cocosApi.ts";

const sessionDto = {
  account: { id: "acc_1", type: "GUEST", status: "ACTIVE", createdAt: "t" },
  accessToken: "access-1", accessExpiresAt: "t",
  refreshToken: "refresh-1", refreshExpiresAt: "t", deviceId: "device-1",
};

// ---- fake localStorage(结构类型鸭子匹配 CocosStorage) ----

class FakeStorage {
  items = new Map<string, string>();

  getItem(key: string): string | null {
    return this.items.get(key) ?? null;
  }
  setItem(key: string, value: string): void {
    this.items.set(key, value);
  }
  removeItem(key: string): void {
    this.items.delete(key);
  }
}

const sessionJson = JSON.stringify(sessionDto);

// ---- TokenStore(localStorage) ----

test("cocos tokenStore:save/load 往返;clear 移除;缺失返回 null", () => {
  const storage = new FakeStorage();
  const store = createCocosTokenStore(storage);

  assert.equal(store.load(), null); // 空存储
  store.save(sessionDto);
  assert.equal(storage.getItem("courier.session"), sessionJson); // 落盘为 JSON 字符串
  assert.deepEqual(store.load(), sessionDto);

  store.clear();
  assert.equal(storage.getItem("courier.session"), null);
  assert.equal(store.load(), null);
});

test("cocos tokenStore:storage 读取异常安全降级返回 null(不反噬)", () => {
  const storage = new FakeStorage();
  storage.setItem("courier.session", sessionJson);
  const store = createCocosTokenStore(storage);
  storage.getItem = () => { throw new Error("storage broken"); };
  assert.equal(store.load(), null); // 读取异常:按未认证降级,不抛出
});

test("cocos tokenStore:自定义 key 前缀;损坏数据清场返回 null", () => {
  const storage = new FakeStorage();
  const store = createCocosTokenStore(storage, "courier.session.dev");
  assert.equal(store.load(), null);

  storage.items.set("courier.session.dev", "{not-json");
  assert.equal(store.load(), null);
  assert.equal(storage.getItem("courier.session.dev"), null); // 已清场
});

test("cocos tokenStore:合法 JSON 非会话对象清场返回 null", () => {
  const badShapes = [
    JSON.stringify("access-1"),
    JSON.stringify(42),
    JSON.stringify([]),
    JSON.stringify({}),
    JSON.stringify({ accessToken: "x" }), // 缺 refreshToken/expires/deviceId
    JSON.stringify({ accessToken: "", refreshToken: "r", accessExpiresAt: "t", refreshExpiresAt: "t", deviceId: "d" }),
    JSON.stringify({ accessToken: "a", refreshToken: "r", accessExpiresAt: "t", refreshExpiresAt: "t", deviceId: 42 }),
  ];

  for (const raw of badShapes) {
    const storage = new FakeStorage();
    storage.setItem("courier.session", raw);
    const store = createCocosTokenStore(storage);
    assert.equal(store.load(), null); // 字符串/数字/数组/`{}`/缺字段/空 token 均非会话
    assert.equal(storage.getItem("courier.session"), null); // 已清场
  }
});

test("cocos tokenStore:完整会话通过校验(不误清)", () => {
  const storage = new FakeStorage();
  storage.setItem("courier.session", sessionJson);
  const store = createCocosTokenStore(storage);
  assert.deepEqual(store.load(), sessionDto); // 完整会话原样返回
  assert.equal(storage.getItem("courier.session"), sessionJson); // 未清场
});

// ---- 全链组合(cocos 适配器 × core) ----

test("cocos 全链:CourierClient(native store)+ fetch 登录落盘 localStorage", async () => {
  const storage = new FakeStorage();
  const fakeTransport = {
    async send(): Promise<{ statusCode: number; headers: Record<string, string>; body: string }> {
      return { statusCode: 200, headers: { "content-type": "application/json" }, body: JSON.stringify({ data: sessionDto }) };
    },
  };
  const client = new CourierClient({
    config: { endpoint: "https://api.example.com", gameId: "game_demo", env: "prod" },
    transport: fakeTransport as never,
    tokenStore: createCocosTokenStore(storage),
    sleep: async () => {},
  });

  client.init();
  const session = await client.guestLoginAsync({ deviceId: "device-1" });
  assert.equal(session.accessToken, "access-1");
  assert.equal(client.lifecycle.current, LifecycleState.PlayerReady);
  assert.equal(storage.getItem("courier.session"), sessionJson); // 会话落盘 localStorage

  // 冷启动重开:新 client 从 localStorage 恢复会话;状态机不跨进程。
  const reopened = new CourierClient({
    config: { endpoint: "https://api.example.com", gameId: "game_demo", env: "prod" },
    transport: fakeTransport as never,
    tokenStore: createCocosTokenStore(storage),
    sleep: async () => {},
  });
  assert.equal(reopened.session.accessToken(), "access-1"); // 冷启动恢复
  assert.equal(reopened.lifecycle.current, LifecycleState.Uninitialized); // 状态机不跨进程
});
