// UI 可选包测试(零依赖,node:test):品牌目录兜底链 + 三面板编排与纯文本产出
// (unity UI 包 ServiceTests 同构;渲染出口经回调断言)。
import assert from "node:assert/strict";
import { test } from "node:test";
import { CourierClient } from "../src/core/courierClient.ts";
import type { Transport, TransportRequest, TransportResponse } from "../src/core/transport.ts";
import { CourierServices } from "../src/service/courierServices.ts";
import {
  applyBranding, currentBrandingVersion, getCompanyName, getPrimaryColor,
  getSupportLabel, resetBranding, tryGetString,
} from "../src/ui/brandingCatalog.ts";
import { AnnouncementPanel } from "../src/ui/announcementPanel.ts";
import { AssistantPanel, AssistantPanelDefaultTitle } from "../src/ui/assistantPanel.ts";
import { CustomerServicePanel, CustomerServicePanelDefaultTitle } from "../src/ui/customerServicePanel.ts";

const config = { endpoint: "https://api.example.com", gameId: "game_demo", env: "prod" };
const sessionJson = JSON.stringify({
  account: { id: "acc_1", type: "GUEST", status: "ACTIVE", createdAt: "t" },
  accessToken: "access-1", accessExpiresAt: "t",
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

// ---- 品牌目录(兜底链:远端 → 内置默认) ----

test("branding:apply 热切换;相同 version 忽略;reset 回全默认", () => {
  resetBranding();
  assert.equal(currentBrandingVersion(), -1); // 从未应用
  assert.equal(getCompanyName(), "Courier"); // 内置默认标
  assert.equal(getPrimaryColor(), "#4C8DFF");
  assert.equal(getSupportLabel(), "联系客服");
  assert.equal(tryGetString("productName"), null);

  applyBranding({ version: 3, companyName: "Demo" });
  assert.equal(currentBrandingVersion(), 3);
  assert.equal(tryGetString("companyName"), "Demo");

  applyBranding({ version: 3, companyName: "Ignored" }); // 相同 version 忽略
  assert.equal(tryGetString("companyName"), "Demo");

  applyBranding(null); // null 忽略
  assert.equal(currentBrandingVersion(), 3);

  resetBranding();
  assert.equal(currentBrandingVersion(), -1);
  assert.equal(getCompanyName(), "Courier");
});

test("branding:主题色嵌套取值;supportEntryLabel 覆盖;非字符串回落", () => {
  resetBranding();
  applyBranding({
    version: 4,
    theme: { primaryColor: "#00FF00" },
    supportEntryLabel: "7×24 客服",
    companyName: 42, // 非字符串:回落默认
  });
  assert.equal(getPrimaryColor(), "#00FF00");
  assert.equal(getSupportLabel(), "7×24 客服");
  assert.equal(getCompanyName(), "Courier");
  resetBranding();
});

// ---- 公告栏面板 ----

test("公告面板:refresh 渲染列表文本;空列表兜底文案;open 渲染详情并打点", async () => {
  resetBranding();
  const transport = new FakeTransport();
  const svc = await newServices(transport);
  const panel = new AnnouncementPanel(svc);
  const rendered: string[] = [];
  const opened: string[] = [];
  panel.onListRendered = (text) => rendered.push(text);
  panel.onAnnouncementOpened = (id) => opened.push(id);

  transport.enqueue(200, JSON.stringify({ data: { items: [{
    id: "ann_1", title: "维护公告", body: "今晚维护", severity: "INFO",
    startAt: "t", endAt: "t", publishedAt: "t",
  }], nextCursor: "" } }));
  await panel.refreshAsync();

  assert.match(rendered[0], /^公告\n\[INFO\] 维护公告\n今晚维护$/); // 默认标 + 条目
  assert.equal(transport.requests[1].url, "https://api.example.com/v1/announcements?limit=20");

  transport.enqueue(200, JSON.stringify({ data: {
    id: "ann_1", title: "维护公告", body: "今晚维护", severity: "CRITICAL",
    startAt: "t", endAt: "t", publishedAt: "t",
  } }));
  await panel.openAsync("ann_1");
  assert.deepEqual(opened, ["ann_1"]);
  assert.match(rendered[1], /^\[CRITICAL\] 维护公告\n今晚维护$/); // 详情:标题行 + 全文

  // 空列表兜底文案
  transport.enqueue(200, JSON.stringify({ data: { items: [], nextCursor: "" } }));
  await panel.refreshAsync();
  assert.equal(rendered[2], "公告\n(暂无公告)");
});

test("公告面板:跑马灯出口(F36)——CRITICAL 命中回调;纯 INFO 不回调;详情视图不触发", async () => {
  resetBranding();
  const transport = new FakeTransport();
  const svc = await newServices(transport);
  const panel = new AnnouncementPanel(svc);
  const marquees: string[] = [];
  panel.onMarqueeRendered = (text) => marquees.push(text);

  transport.enqueue(200, JSON.stringify({ data: { items: [
    { id: "ann_1", title: "维护公告", body: "今晚维护", severity: "INFO",
      startAt: "t", endAt: "t", publishedAt: "t" },
    { id: "ann_2", title: "限时活动", body: "双倍经验开启", severity: "CRITICAL",
      startAt: "t", endAt: "t", publishedAt: "t" },
  ], nextCursor: "" } }));
  await panel.refreshAsync();
  assert.deepEqual(marquees, ["限时活动: 双倍经验开启"]); // 仅 CRITICAL 进跑马灯

  transport.enqueue(200, JSON.stringify({ data: { items: [
    { id: "ann_3", title: "日常公告", body: "版本说明", severity: "INFO",
      startAt: "t", endAt: "t", publishedAt: "t" },
  ], nextCursor: "" } }));
  await panel.refreshAsync();
  assert.deepEqual(marquees, ["限时活动: 双倍经验开启"]); // 无 CRITICAL:不回调,不闪空跑马灯

  // 多条 CRITICAL 以 "  |  " 连接为单行滚动文本
  transport.enqueue(200, JSON.stringify({ data: { items: [
    { id: "ann_4", title: "紧急维护", body: "02:00 停服", severity: "CRITICAL",
      startAt: "t", endAt: "t", publishedAt: "t" },
    { id: "ann_5", title: "补偿发放", body: "维护后 48h", severity: "CRITICAL",
      startAt: "t", endAt: "t", publishedAt: "t" },
  ], nextCursor: "" } }));
  await panel.refreshAsync();
  assert.deepEqual(marquees,
    ["限时活动: 双倍经验开启", "紧急维护: 02:00 停服  |  补偿发放: 维护后 48h"]);

  // 详情视图(openAsync)不触发跑马灯
  transport.enqueue(200, JSON.stringify({ data: {
    id: "ann_2", title: "限时活动", body: "双倍经验开启", severity: "CRITICAL",
    startAt: "t", endAt: "t", publishedAt: "t",
  } }));
  await panel.openAsync("ann_2");
  assert.equal(marquees.length, 2);
});

test("公告面板:品牌兜底链(远端 productName → 宿主 brandTitle);typed 404 出错误口;未装配静默", async () => {
  resetBranding();
  const transport = new FakeTransport();
  const svc = await newServices(transport);

  applyBranding({ version: 5, productName: "星尘物语" });
  const panel = new AnnouncementPanel(svc);
  assert.equal(panel.resolveTitle(), "星尘物语"); // 远端覆盖
  applyBranding({ version: 6 }); // 无 productName 字段
  assert.equal(panel.resolveTitle(), "公告"); // 回落默认标

  const panel2 = new AnnouncementPanel(svc, "自定义标");
  assert.equal(panel2.resolveTitle(), "自定义标"); // 宿主 brandTitle

  const errors: string[] = [];
  panel.onError = (code) => errors.push(code);
  transport.enqueue(404, JSON.stringify({
    error: { code: "ANNOUNCEMENT_NOT_FOUND", message: "不可见", retryable: false },
  }));
  await panel.openAsync("ann_x");
  assert.deepEqual(errors, ["ANNOUNCEMENT_NOT_FOUND"]); // typed wire code 出错误口

  const bare = new AnnouncementPanel(); // 未装配:静默不抛
  await bare.refreshAsync();
  await bare.openAsync("ann_x");
  resetBranding();
});

// ---- 客服页面板 ----

test("客服面板:提单即开详情;追加消息重拉;CLOSED 409 出错误口", async () => {
  resetBranding();
  const transport = new FakeTransport();
  const svc = await newServices(transport);
  const panel = new CustomerServicePanel(svc);
  const details: string[] = [];
  const errors: string[] = [];
  panel.onDetailRendered = (d) => details.push(d.ticket.status);
  panel.onError = (code) => errors.push(code);

  transport.enqueue(200, JSON.stringify({ data: {
    id: "tkt_1", title: "问题", status: "OPEN", createdAt: "t", updatedAt: "t",
  } }));
  // 提单即开详情:create + get 两跳(unity 同构),详情响应补在这里
  transport.enqueue(200, JSON.stringify({ data: {
    ticket: { id: "tkt_1", title: "问题", status: "OPEN", createdAt: "t", updatedAt: "t" },
    messages: [{ senderType: "PLAYER", body: "内容", createdAt: "t" }],
  } }));
  await panel.openTicketAsync("问题", "内容", "PAYMENT");
  assert.equal(panel.getCurrentTicketId(), "tkt_1");
  assert.deepEqual(details, ["OPEN"]);
  assert.equal(transport.requests[1].url, "https://api.example.com/v1/support/tickets");

  // 追加消息:成功 → 重拉详情
  transport.enqueue(200, JSON.stringify({ data: {
    senderType: "PLAYER", body: "再问一句", createdAt: "t",
  } }));
  transport.enqueue(200, JSON.stringify({ data: {
    ticket: { id: "tkt_1", title: "问题", status: "REPLIED", createdAt: "t", updatedAt: "t" },
    messages: [
      { senderType: "PLAYER", body: "内容", createdAt: "t" },
      { senderType: "AGENT", body: "已回复", createdAt: "t" },
    ],
  } }));
  await panel.sendMessageAsync("再问一句");
  assert.deepEqual(details, ["OPEN", "REPLIED"]);
  assert.equal(transport.requests[3].url, "https://api.example.com/v1/support/tickets/tkt_1/messages");
  assert.deepEqual(JSON.parse(transport.requests[3].jsonBody ?? "{}"), { body: "再问一句" });

  // CLOSED:追加被拒,typed 409 出错误口
  transport.enqueue(409, JSON.stringify({
    error: { code: "SUPPORT_TICKET_CLOSED", message: "已关单", retryable: false },
  }));
  await panel.sendMessageAsync("再问一句");
  assert.deepEqual(errors, ["SUPPORT_TICKET_CLOSED"]);
});

test("客服面板:notifyTicketReplied 只认当前工单;FAQ 空结果兜底文案;入口文案走品牌", async () => {
  resetBranding();
  const transport = new FakeTransport();
  const svc = await newServices(transport);
  const panel = new CustomerServicePanel(svc);
  const details: number[] = [];
  panel.onDetailRendered = (d) => details.push(d.messages.length);

  transport.enqueue(200, JSON.stringify({ data: {
    ticket: { id: "tkt_1", title: "问题", status: "REPLIED", createdAt: "t", updatedAt: "t" },
    messages: [{ senderType: "AGENT", body: "已回复", createdAt: "t" }],
  } }));
  await panel.loadTicketAsync("tkt_1");
  assert.deepEqual(details, [1]);

  await panel.notifyTicketReplied("tkt_other"); // 非当前工单:忽略
  assert.deepEqual(details, [1]);

  transport.enqueue(200, JSON.stringify({ data: {
    ticket: { id: "tkt_1", title: "问题", status: "REPLIED", createdAt: "t", updatedAt: "t" },
    messages: [{ senderType: "AGENT", body: "已回复", createdAt: "t" }, { senderType: "AGENT", body: "2", createdAt: "t" }],
  } }));
  await panel.notifyTicketReplied("tkt_1"); // 当前工单:重拉
  assert.deepEqual(details, [1, 2]);

  transport.enqueue(200, JSON.stringify({ data: { items: [], nextCursor: "" } }));
  assert.equal(await panel.searchFaqAsync("找回"), "(无匹配问题)");
  transport.enqueue(200, JSON.stringify({ data: { items: [
    { id: "faq_1", question: "怎么找回账号", answer: "点忘记密码" },
  ], nextCursor: "" } }));
  assert.equal(await panel.searchFaqAsync("找回"), "Q: 怎么找回账号\nA: 点忘记密码\n");

  applyBranding({ version: 7, supportEntryLabel: "联系客服小姐姐" });
  assert.equal(panel.supportEntryLabel, "联系客服小姐姐");
  resetBranding();
});

// ---- 小助手面板 ----

test("助手面板:命中原样渲染;未命中 suggestTransfer;501 出错误口", async () => {
  resetBranding();
  const transport = new FakeTransport();
  const svc = await newServices(transport);
  const panel = new AssistantPanel(svc);
  const answers: AssistantResult[] = [];
  const errors: string[] = [];
  panel.onAnswerRendered = (r) => answers.push(r);
  panel.onError = (code) => errors.push(code);

  transport.enqueue(200, JSON.stringify({ data: {
    matched: true,
    answer: { id: "faq_1", question: "怎么找回账号", answer: "点忘记密码", keywords: ["找回"] },
    suggestTransfer: false,
  } }));
  await panel.askAsync("账号怎么找回");
  assert.equal(answers.length, 1);
  assert.equal(answers[0].matched, true);
  assert.equal(answers[0].answer?.question, "怎么找回账号"); // 原样快照

  transport.enqueue(200, JSON.stringify({ data: { matched: false, suggestTransfer: true } }));
  await panel.askAsync("如何下载游戏");
  assert.equal(answers[1].matched, false);
  assert.equal(answers[1].suggestTransfer, true); // UI 据此引导转人工

  transport.enqueue(501, JSON.stringify({
    error: { code: "COMMON_CAPABILITY_DISABLED", message: "not configured", retryable: false },
  }));
  await panel.askAsync("q");
  assert.deepEqual(errors, ["COMMON_CAPABILITY_DISABLED"]); // 隐藏入口
});

type AssistantResult = Awaited<ReturnType<NonNullable<AssistantPanel["onAnswerRendered"]>>>;

test("助手面板:一键转人工带 [助手] 前缀提单(标题截 120);成功回调工单 ID", async () => {
  resetBranding();
  const transport = new FakeTransport();
  const svc = await newServices(transport);
  const panel = new AssistantPanel(svc);
  const created: string[] = [];
  panel.onTicketCreated = (id) => created.push(id);

  transport.enqueue(200, JSON.stringify({ data: {
    id: "tkt_9", title: "[助手] 充值没到账", status: "OPEN", createdAt: "t", updatedAt: "t",
  } }));
  await panel.transferToHumanAsync("充值没到账");

  const req = transport.requests[1];
  assert.equal(req.url, "https://api.example.com/v1/support/tickets");
  const body = JSON.parse(req.jsonBody ?? "{}") as { title: string; body: string };
  assert.equal(body.title, "[助手] 充值没到账"); // 前缀带进工单
  assert.equal(body.body, "充值没到账");
  assert.deepEqual(created, ["tkt_9"]);
  resetBranding();
});

// ---- 面板未装配面与错误面(能力未接/限流/断网均走同一条 onError) ----

test("客服面板:未装配 services 时全方法静默 no-op", async () => {
  const bare = new CustomerServicePanel();
  assert.equal(bare.getCurrentTicketId(), null);
  await bare.openTicketAsync("t", "b");
  await bare.loadTicketAsync("tkt_x");
  await bare.sendMessageAsync("内容"); // 无当前工单:同样静默
  assert.equal(await bare.searchFaqAsync("找回"), "");
  await bare.notifyTicketReplied("tkt_x"); // 非当前工单:忽略
  assert.equal(bare.getCurrentTicketId(), null); // 未装配不写当前工单

  // 未装配面板的标题走宿主 brandTitle
  const titled = new CustomerServicePanel();
  assert.equal(titled.resolveTitle(), CustomerServicePanelDefaultTitle);
  titled.brandTitle = "宿主客服";
  assert.equal(titled.resolveTitle(), "宿主客服");
});

test("客服面板:提单限流 RATE_LIMITED 与详情 404 出错误口;标题走品牌兜底链", async () => {
  resetBranding();
  const transport = new FakeTransport();
  const svc = await newServices(transport);
  const panel = new CustomerServicePanel(svc);
  panel.brandTitle = "宿主客服";
  const errors: string[] = [];
  panel.onError = (code) => errors.push(code);

  // 提单被限流:重试耗尽后错误出口,不写当前工单
  for (let i = 0; i < 3; i++) {
    transport.enqueue(429, JSON.stringify({
      error: { code: "RATE_LIMITED", message: "慢一点", retryable: true },
    }));
  }
  await panel.openTicketAsync("问题", "内容");
  assert.deepEqual(errors, ["RATE_LIMITED"]);
  assert.equal(panel.getCurrentTicketId(), null);

  // 非本人工单(404):详情错误出口,当前工单仍记录(供后续刷新)
  transport.enqueue(200, JSON.stringify({ data: {
    id: "tkt_2", title: "问题", status: "OPEN", createdAt: "t", updatedAt: "t",
  } }));
  transport.enqueue(404, JSON.stringify({
    error: { code: "SUPPORT_TICKET_NOT_FOUND", message: "不属于本人", retryable: false },
  }));
  await panel.openTicketAsync("问题", "内容");
  assert.deepEqual(errors, ["RATE_LIMITED", "SUPPORT_TICKET_NOT_FOUND"]);
  assert.equal(panel.getCurrentTicketId(), "tkt_2");

  // 品牌兜底链:远端 productName → 宿主 brandTitle → 内置默认
  applyBranding({ version: 9, productName: "星尘物语" });
  assert.equal(panel.resolveTitle(), "星尘物语");
  applyBranding({ version: 10 });
  assert.equal(panel.resolveTitle(), "宿主客服");
  assert.equal(new CustomerServicePanel(svc).resolveTitle(), CustomerServicePanelDefaultTitle);
  resetBranding();
});

test("客服面板:FAQ 检索失败出错误口并回落空串", async () => {
  resetBranding();
  const transport = new FakeTransport();
  const svc = await newServices(transport);
  const panel = new CustomerServicePanel(svc);
  const errors: string[] = [];
  panel.onError = (code) => errors.push(code);

  transport.enqueue(503, JSON.stringify({
    error: { code: "COMMON_UNAVAILABLE", message: "busy", retryable: true },
  }));
  for (let i = 0; i < 2; i++) {
    transport.enqueue(503, JSON.stringify({
      error: { code: "COMMON_UNAVAILABLE", message: "busy", retryable: true },
    }));
  }
  assert.equal(await panel.searchFaqAsync("找回"), "");
  assert.deepEqual(errors, ["COMMON_UNAVAILABLE"]);
});

test("助手面板:未装配静默;标题兜底链;提问断网与转人工失败出错误口", async () => {
  resetBranding();
  const bare = new AssistantPanel();
  await bare.askAsync("q");
  await bare.transferToHumanAsync("q");
  assert.equal(bare.resolveTitle(), AssistantPanelDefaultTitle);

  const transport = new FakeTransport();
  const svc = await newServices(transport);
  const panel = new AssistantPanel(svc);
  panel.brandTitle = "宿主助手";
  const errors: string[] = [];
  panel.onError = (code) => errors.push(code);

  applyBranding({ version: 11, productName: "星尘物语" });
  assert.equal(panel.resolveTitle(), "星尘物语"); // 远端优先
  applyBranding({ version: 12, productName: "" });
  assert.equal(panel.resolveTitle(), "宿主助手"); // 空串回落宿主标
  applyBranding({ version: 13 });
  assert.equal(panel.resolveTitle(), "宿主助手");

  // 提问断网:错误出口,不渲染答案
  const rendered: string[] = [];
  panel.onAnswerRendered = (r) => rendered.push(String(r.matched));
  for (let i = 0; i < 3; i++) {
    transport.enqueue(503, JSON.stringify({
      error: { code: "COMMON_UNAVAILABLE", message: "busy", retryable: true },
    }));
  }
  await panel.askAsync("q");
  assert.deepEqual(errors, ["COMMON_UNAVAILABLE"]);
  assert.deepEqual(rendered, []);

  // 转人工提单被拒:错误出口,不回调工单 ID
  const created: string[] = [];
  panel.onTicketCreated = (id) => created.push(id);
  transport.enqueue(409, JSON.stringify({
    error: { code: "SUPPORT_TICKET_CLOSED", message: "已关单", retryable: false },
  }));
  await panel.transferToHumanAsync("q");
  assert.deepEqual(errors, ["COMMON_UNAVAILABLE", "SUPPORT_TICKET_CLOSED"]);
  assert.deepEqual(created, []);
  resetBranding();
});

test("助手面板:转人工标题按 120 字符截断(契约 support.md title 上限)", async () => {
  resetBranding();
  const transport = new FakeTransport();
  const svc = await newServices(transport);
  const panel = new AssistantPanel(svc);
  const long = "问".repeat(200);
  transport.enqueue(200, JSON.stringify({ data: {
    id: "tkt_10", title: "截断", status: "OPEN", createdAt: "t", updatedAt: "t",
  } }));
  await panel.transferToHumanAsync(long);

  const body = JSON.parse(transport.requests[1].jsonBody ?? "{}") as { title: string; body: string };
  assert.equal(body.title.length, 120);
  assert.equal(body.title, ("[助手] " + long).slice(0, 120));
  assert.equal(body.body.length, 200); // body 不截断
  resetBranding();
});
