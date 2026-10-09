// M3/M4/M5 服务域测试(零依赖,node:test):app/config/branding/player/assistant/
// payment/realname 的 wire 形状、501 降级语义、typed 错误、缓存判据。
import assert from "node:assert/strict";
import { test } from "node:test";
import { CourierClient } from "../src/core/courierClient.ts";
import type { Transport, TransportRequest, TransportResponse } from "../src/core/transport.ts";
import { CourierServices } from "../src/service/courierServices.ts";

const config = { endpoint: "https://api.example.com", gameId: "game_demo", env: "prod" };
const sessionJson = JSON.stringify({
  accountId: "acc_1", accessToken: "access-1", accessExpiresAt: "t",
  refreshToken: "refresh-1", refreshExpiresAt: "t", deviceId: "device-1",
});

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

async function newServices(transport: FakeTransport): Promise<CourierServices> {
  const client = new CourierClient({ config, transport, sleep: async () => {} });
  transport.enqueue(200, '{"data":' + sessionJson + "}");
  await client.identity.guest({ deviceId: "device-1" });
  return new CourierServices(client);
}

const disabled = JSON.stringify({
  error: { code: "COMMON_CAPABILITY_DISABLED", message: "not configured", retryable: false },
});

// ---- App 域(匿名可,501 → null) ----

test("app:三端点匿名(无 Bearer),501 → null(跳过检查直接登录)", async () => {
  const transport = new FakeTransport();
  const svc = await newServices(transport);

  transport.enqueue(200, JSON.stringify({
    data: { latestVersion: "1.2.0", minVersion: "1.0.0", updateUrl: "https://dl", forceUpdate: false },
  }));
  const version = await svc.app.checkUpdateAsync("1.1.0", "android");

  assert.ok(!("Authorization" in transport.requests[1].headers)); // 匿名
  assert.equal(transport.requests[1].url,
    "https://api.example.com/v1/app/version?appVersion=1.1.0&platform=android");
  assert.equal(version?.forceUpdate, false);

  transport.enqueue(501, disabled);
  transport.enqueue(501, disabled); // 每端点一份(非 retryable,单次)
  assert.equal(await svc.app.checkMaintenanceAsync(), null);
  assert.equal(await svc.app.getEnvironmentAsync(), null);
});

test("app:维护中 typed APP_MAINTENANCE(503)照常抛,不进降级路径", async () => {
  const transport = new FakeTransport();
  const svc = await newServices(transport);
  transport.enqueue(503, JSON.stringify({
    error: { code: "APP_MAINTENANCE", message: "维护中", retryable: false },
  }));

  const e = await svc.app.checkMaintenanceAsync().catch((x) => x);
  assert.equal(e.code, "AppMaintenance");
  assert.equal(e.http, 503);
});

// ---- Config 域(缓存 + 重拉判据 + 501 → 空结果) ----

test("config:拉取缓存;needsRefetch 判据;501 → v0 空 items(用内建默认值)", async () => {
  const transport = new FakeTransport();
  const svc = await newServices(transport);

  assert.equal(svc.config.needsRefetch(7), true); // 从未拉到 → 一律重拉
  transport.enqueue(200, JSON.stringify({
    data: { configVersion: 7, items: { shop_banner: "https://cdn/x.png", limit: 3 } },
  }));
  const snapshot = await svc.config.fetchAsync("android", "1.1.0");

  assert.equal(transport.requests[1].url,
    "https://api.example.com/v1/app/config?platform=android&appVersion=1.1.0");
  assert.equal(snapshot.configVersion, 7);
  assert.equal(svc.config.cachedConfig?.configVersion, 7);
  assert.equal(svc.config.get<string>("shop_banner"), "https://cdn/x.png");
  assert.equal(svc.config.get<number>("limit"), 3);
  assert.equal(svc.config.get("missing"), undefined);

  assert.equal(svc.config.needsRefetch(7), false); // 相同 version 忽略
  assert.equal(svc.config.needsRefetch(8), true);

  transport.enqueue(501, disabled);
  const empty = await svc.config.fetchAsync();
  assert.deepEqual(empty, { configVersion: 0, items: {} }); // 不进报错路径
  assert.equal(svc.config.cachedConfig?.configVersion, 7); // 失败沿用缓存
});

// ---- Branding 域(匿名,501 → null,version 判据) ----

test("branding:匿名拉取透传业务字段;501 → null;needsRefetch 判据", async () => {
  const transport = new FakeTransport();
  const svc = await newServices(transport);

  assert.equal(svc.branding.needsRefetch(3), true);
  transport.enqueue(200, JSON.stringify({
    data: { version: 3, companyName: "Demo", primaryColor: "#00FF00" },
  }));
  const dto = await svc.branding.fetchAsync();

  assert.ok(!("Authorization" in transport.requests[1].headers));
  assert.equal(dto?.version, 3);
  assert.equal(dto?.companyName, "Demo"); // 业务字段透传,SDK 零解释
  assert.equal(svc.branding.needsRefetch(3), false);
  assert.equal(svc.branding.needsRefetch(4), true);

  transport.enqueue(501, disabled);
  assert.equal(await svc.branding.fetchAsync(), null);
});

// ---- Player 域 ----

test("player:档案懒建解析;PATCH undefined 键不下发;501 → null", async () => {
  const transport = new FakeTransport();
  const svc = await newServices(transport);
  transport.enqueue(200, JSON.stringify({
    data: { displayName: "Player", createdAt: "t1", updatedAt: "t1" },
  }));

  const profile = await svc.player.getProfileAsync();
  assert.equal(profile?.displayName, "Player");
  assert.ok(!("avatarUrl" in (profile as object))); // 可选字段缺省

  transport.enqueue(200, JSON.stringify({
    data: { displayName: "新名字", avatarUrl: "https://cdn/a.png", createdAt: "t1", updatedAt: "t2" },
  }));
  const updated = await svc.player.updateProfileAsync("新名字", "https://cdn/a.png");
  assert.deepEqual(JSON.parse(transport.requests[2].jsonBody ?? "{}"),
    { displayName: "新名字", avatarUrl: "https://cdn/a.png" });
  assert.equal(updated?.displayName, "新名字");

  transport.enqueue(501, disabled);
  assert.equal(await svc.player.listCharactersAsync(), null);
});

// ---- Assistant 域 ----

test("assistant:命中原样快照;未命中非错误;501 → null", async () => {
  const transport = new FakeTransport();
  const svc = await newServices(transport);

  transport.enqueue(200, JSON.stringify({
    data: {
      matched: true,
      answer: { id: "faq_1", question: "怎么找回账号", answer: "点忘记密码", keywords: ["找回"] },
      suggestTransfer: false,
    },
  }));
  const hit = await svc.assistant.queryAsync("账号怎么找回");

  assert.equal(transport.requests[1].url, "https://api.example.com/v1/assistant/query");
  assert.deepEqual(JSON.parse(transport.requests[1].jsonBody ?? "{}"), { text: "账号怎么找回" });
  assert.equal(hit?.matched, true);
  assert.equal(hit?.answer?.question, "怎么找回账号"); // 原样快照

  transport.enqueue(200, JSON.stringify({ data: { matched: false, suggestTransfer: true } }));
  const miss = await svc.assistant.queryAsync("如何下载游戏");
  assert.equal(miss?.matched, false);
  assert.equal(miss?.suggestTransfer, true); // UI 据此转人工
  assert.equal(miss?.answer, undefined);

  transport.enqueue(501, disabled);
  assert.equal(await svc.assistant.queryAsync("q"), null);
});

// ---- Payment 域 ----

test("payment:下单只发 skuId(服务端定价红线);payToken 仅下单响应;轮询详情", async () => {
  const transport = new FakeTransport();
  const svc = await newServices(transport);

  transport.enqueue(200, JSON.stringify({
    data: { items: [{ id: "sku_gem_60", productId: "com.demo.gem60", form: "DIRECT_PURCHASE",
      amountCents: 600, currency: "CNY" }], nextCursor: "" },
  }));
  const skus = await svc.payments.getSkusAsync();
  assert.equal(skus?.items[0].amountCents, 600);

  transport.enqueue(200, JSON.stringify({
    data: { id: "order_1", status: "CREATED", skuId: "sku_gem_60", productId: "com.demo.gem60",
      form: "DIRECT_PURCHASE", amountCents: 600, currency: "CNY", payToken: "sbox_abc",
      createdAt: "t", updatedAt: "t" },
  }));
  const created = await svc.payments.createOrderAsync("sku_gem_60");

  assert.equal(transport.requests[2].url, "https://api.example.com/v1/payments/orders");
  assert.deepEqual(JSON.parse(transport.requests[2].jsonBody ?? "{}"), { skuId: "sku_gem_60" });
  assert.equal(created?.status, "CREATED");
  assert.equal(created?.payToken, "sbox_abc");

  transport.enqueue(200, JSON.stringify({
    data: { id: "order_1", status: "DELIVERED", skuId: "sku_gem_60", amountCents: 600,
      currency: "CNY", createdAt: "t", updatedAt: "t2", paidAt: "t1", deliveredAt: "t2" },
  }));
  const detail = await svc.payments.getOrderAsync("order_1");
  assert.equal(detail?.status, "DELIVERED");
  assert.ok(!("payToken" in (detail as object))); // 详情不下发 payToken
});

test("payment:他人订单 typed PAYMENT_ORDER_NOT_FOUND;501 → null(隐藏商城)", async () => {
  const transport = new FakeTransport();
  const svc = await newServices(transport);
  transport.enqueue(404, JSON.stringify({
    error: { code: "PAYMENT_ORDER_NOT_FOUND", message: "订单不存在", retryable: false },
  }));

  const e = await svc.payments.getOrderAsync("order_other").catch((x) => x);
  assert.equal(e.code, "PaymentOrderNotFound");
  assert.equal(transport.requests.length, 2); // 不重试

  transport.enqueue(501, disabled);
  assert.equal(await svc.payments.getSkusAsync(), null);
});

// ---- RealName 域 ----

test("realname:提交 wire(字段只在传输链);curfew;charge-check;501 → null", async () => {
  const transport = new FakeTransport();
  const svc = await newServices(transport);

  transport.enqueue(200, JSON.stringify({
    data: { state: "VERIFIED", isMinor: true, verifiedAt: "t" },
  }));
  const status = await svc.realname.submitAsync("张三", "110101199001011234");

  assert.equal(transport.requests[1].url, "https://api.example.com/v1/realname/verify");
  assert.deepEqual(JSON.parse(transport.requests[1].jsonBody ?? "{}"),
    { name: "张三", idNumber: "110101199001011234" });
  assert.equal(status?.state, "VERIFIED");

  transport.enqueue(200, JSON.stringify({ data: { playable: false, nextWindowAt: "t2" } }));
  const curfew = await svc.realname.curfewAsync();
  assert.equal(curfew?.playable, false);
  assert.equal(curfew?.nextWindowAt, "t2");

  transport.enqueue(200, JSON.stringify({
    data: { allowed: true, singleLimitCents: 10000, monthlyLimitCents: 100000, monthlyUsedCents: 600 },
  }));
  const charge = await svc.realname.chargeCheckAsync(600);
  assert.deepEqual(JSON.parse(transport.requests[3].jsonBody ?? "{}"), { amountCents: 600 });
  assert.equal(charge?.allowed, true);

  transport.enqueue(501, disabled);
  assert.equal(await svc.realname.statusAsync(), null); // 隐藏实名 UI
});
