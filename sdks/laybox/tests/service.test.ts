// M2 服务域测试(零依赖,node:test):SSE 帧解析 + 公告/客服 wire 形状与 typed 错误。
// 运行:node --test tests/service.test.ts
import assert from "node:assert/strict";
import { test } from "node:test";
import { CourierClient } from "../src/core/courierClient.ts";
import type { Transport, TransportRequest, TransportResponse } from "../src/core/transport.ts";
import { CourierServices } from "../src/service/courierServices.ts";
import { SseParser } from "../src/service/sseParser.ts";

const config = { endpoint: "https://api.example.com", gameId: "game_demo", env: "prod" };
const sessionJson = JSON.stringify({
  account: { id: "acc_1", type: "GUEST", status: "ACTIVE", createdAt: "t" },
  accessToken: "access-1", accessExpiresAt: "t",
  refreshToken: "refresh-1", refreshExpiresAt: "t", deviceId: "device-1",
});

class FakeTransport implements Transport {
  requests: TransportRequest[] = [];
  private queue: TransportResponse[] = [];

  enqueue(statusCode: number, body: string, headers?: Record<string, string>): void {
    this.queue.push({ statusCode, headers: headers ?? {}, body });
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

// ---- SSE 帧解析(契约 messages.md) ----

test("sse:标准帧(event/data/id)+ 心跳注释忽略", () => {
  const p = new SseParser();
  assert.equal(p.feedLine(": connected"), null);
  assert.equal(p.feedLine(""), null); // 注释后空行:无帧结算
  assert.equal(p.feedLine(": ping"), null);

  assert.equal(p.feedLine("event: announcement.published"), null);
  assert.equal(p.feedLine("id: 7"), null);
  assert.equal(p.feedLine('data: {"id":"ann_1"}'), null);
  const evt = p.feedLine("");
  assert.ok(evt);
  assert.equal(evt.type, "announcement.published");
  assert.equal(evt.id, "7");
  assert.equal(evt.data, '{"id":"ann_1"}');

  // 纯注释帧不结算事件
  assert.equal(p.feedLine(": ping"), null);
  assert.equal(p.feedLine(""), null);
});

test("sse:多行 data 按规范 \\n 连接;冒号无空格也解析", () => {
  const p = new SseParser();
  p.feedLine("event: x.y");
  p.feedLine("data:line1");
  p.feedLine("data: line2");
  const evt = p.feedLine("");
  assert.ok(evt);
  assert.equal(evt.data, "line1\nline2"); // SSE 规范:仅剥冒号后单个空格
});

test("sse:未知字段容忍;reset 断线重连复用", () => {
  const p = new SseParser();
  p.feedLine("event: a.b");
  p.feedLine("retry: 3000"); // 未知字段容忍(契约 versioning.md)
  p.feedLine("data: 1");
  p.feedLine("");
  p.reset(); // 半帧残留清空
  p.feedLine("data: leftover-from-old-frame");
  const evt = p.feedLine("");
  assert.ok(evt);
  assert.equal(evt.type, ""); // reset 后 type 空
  assert.equal(evt.data, "leftover-from-old-frame");
});

// ---- 公告域(契约 announcement.md) ----

test("公告列表:wire 形状(limit/cursor/Bearer)+ 分页解析", async () => {
  const transport = new FakeTransport();
  const svc = await newServices(transport);
  transport.enqueue(200, JSON.stringify({
    data: {
      items: [{
        id: "ann_1", title: "维护公告", body: "今晚维护", severity: "INFO",
        startAt: "2026-10-09T00:00:00.000Z", endAt: "2026-10-10T00:00:00.000Z",
        publishedAt: "2026-10-09T08:00:00.000Z",
      }],
      nextCursor: "cur-2",
    },
  }));

  const page = await svc.announcements.listAsync(20, "cur-1");

  const req = transport.requests[1];
  assert.equal(req.method, "GET");
  assert.equal(req.url, "https://api.example.com/v1/announcements?limit=20&cursor=cur-1");
  assert.equal(req.headers["Authorization"], "Bearer access-1");
  assert.equal(page.items[0].title, "维护公告");
  assert.equal(page.nextCursor, "cur-2");
});

test("公告详情:typed ANNOUNCEMENT_NOT_FOUND(404,不重试)", async () => {
  const transport = new FakeTransport();
  const svc = await newServices(transport);
  transport.enqueue(404, JSON.stringify({
    error: { code: "ANNOUNCEMENT_NOT_FOUND", message: "不可见", retryable: false },
  }));

  const e = await svc.announcements.getAsync("ann_x").catch((x) => x);
  assert.equal(e.code, "AnnouncementNotFound");
  assert.equal(e.http, 404);
  assert.equal(transport.requests.length, 2); // 无重试
});

// ---- 客服域(契约 support.md) ----

test("提单:wire 形状(category 缺省不下发)+ 工单解析", async () => {
  const transport = new FakeTransport();
  const svc = await newServices(transport);
  transport.enqueue(200, JSON.stringify({
    data: {
      id: "tkt_1", title: "[助手转人工] 充值没到账", status: "OPEN", category: "PAYMENT",
      createdAt: "2026-10-09T12:00:00.000Z", updatedAt: "2026-10-09T12:00:00.000Z",
    },
  }));

  const ticket = await svc.support.createTicketAsync({
    title: "充值没到账", body: "订单未发货", category: "PAYMENT",
  });

  const req = transport.requests[1];
  assert.equal(req.method, "POST");
  assert.equal(req.url, "https://api.example.com/v1/support/tickets");
  assert.deepEqual(JSON.parse(req.jsonBody ?? "{}"),
    { title: "充值没到账", body: "订单未发货", category: "PAYMENT" });
  assert.equal(ticket.status, "OPEN");

  // category 缺省:undefined 键不下发
  transport.enqueue(200, JSON.stringify({
    data: { id: "tkt_2", title: "t", status: "OPEN", createdAt: "t", updatedAt: "t" },
  }));
  await svc.support.createTicketAsync({ title: "t", body: "b" });
  const raw = JSON.parse(transport.requests[2].jsonBody ?? "{}");
  assert.ok(!("category" in raw));
});

test("工单详情:ticket + messages 解析;追加消息 CLOSED → typed 409 不重试", async () => {
  const transport = new FakeTransport();
  const svc = await newServices(transport);
  transport.enqueue(200, JSON.stringify({
    data: {
      ticket: {
        id: "tkt_1", title: "问题", status: "REPLIED", category: "OTHER",
        createdAt: "2026-10-09T12:00:00.000Z", updatedAt: "2026-10-09T12:01:00.000Z",
      },
      messages: [
        { senderType: "PLAYER", body: "充值没到账", createdAt: "2026-10-09T12:00:00.000Z" },
        { senderType: "AGENT", body: "已补发", createdAt: "2026-10-09T12:01:00.000Z" },
      ],
    },
  }));

  const detail = await svc.support.getTicketAsync("tkt_1");
  assert.equal(transport.requests[1].url, "https://api.example.com/v1/support/tickets/tkt_1");
  assert.equal(detail.ticket.status, "REPLIED");
  assert.equal(detail.messages.length, 2);
  assert.equal(detail.messages[1].senderType, "AGENT");

  transport.enqueue(409, JSON.stringify({
    error: { code: "SUPPORT_TICKET_CLOSED", message: "已关单", retryable: false },
  }));
  const e = await svc.support.appendMessageAsync("tkt_1", "再问一句").catch((x) => x);
  assert.equal(e.code, "SupportTicketClosed");
  assert.equal(e.http, 409);
  assert.equal(transport.requests.length, 3); // 不重试
  assert.equal(transport.requests[2].url, "https://api.example.com/v1/support/tickets/tkt_1/messages");
  assert.deepEqual(JSON.parse(transport.requests[2].jsonBody ?? "{}"), { body: "再问一句" });
});

test("申请附件直传:wire 形状 + 详情 attachments 加法字段", async () => {
  const transport = new FakeTransport();
  const svc = await newServices(transport);

  transport.enqueue(200, JSON.stringify({
    data: {
      attachmentId: "att_1",
      uploadUrl: "https://storage.local/support/uploads/a1",
      method: "PUT",
      headers: { "X-Storage-Signature": "sig-1" },
      expiresAt: "2026-10-09T12:10:00.000Z",
    },
  }));
  const r = await svc.support.requestAttachmentUploadAsync("tkt_1", {
    fileName: "crash.log", contentType: "text/plain", sizeBytes: 1200, kind: "LOG",
  });
  assert.equal(transport.requests[1].url,
    "https://api.example.com/v1/support/tickets/tkt_1/attachments");
  assert.equal(transport.requests[1].method, "POST");
  assert.deepEqual(JSON.parse(transport.requests[1].jsonBody ?? "{}"),
    { fileName: "crash.log", contentType: "text/plain", sizeBytes: 1200, kind: "LOG" });
  assert.equal(r.attachmentId, "att_1");
  assert.equal(r.method, "PUT");
  assert.equal(r.headers["X-Storage-Signature"], "sig-1");

  transport.enqueue(200, JSON.stringify({
    data: {
      ticket: {
        id: "tkt_1", title: "问题", status: "OPEN",
        createdAt: "2026-10-09T12:00:00.000Z", updatedAt: "2026-10-09T12:00:00.000Z",
      },
      messages: [],
      attachments: [{
        id: "att_1", fileName: "crash.log", contentType: "text/plain",
        sizeBytes: 1200, kind: "LOG", createdAt: "2026-10-09T12:00:00.000Z",
        downloadUrl: "https://storage.local/d1",
      }],
    },
  }));
  const detail = await svc.support.getTicketAsync("tkt_1");
  assert.equal(detail.attachments?.length, 1);
  assert.equal(detail.attachments?.[0]?.kind, "LOG");

  transport.enqueue(413, JSON.stringify({
    error: { code: "SUPPORT_ATTACHMENT_TOO_LARGE", message: "too large", retryable: false },
  }));
  const e = await svc.support.requestAttachmentUploadAsync("tkt_1", {
    fileName: "big.bin", contentType: "application/zip", sizeBytes: 10485761,
  }).catch((x) => x);
  assert.equal(e.code, "SupportAttachmentTooLarge");
  assert.equal(e.http, 413);
  assert.equal(transport.requests.length, 4); // 不可重试:单次调用
});

test("FAQ 检索:关键词 URL 编码;空关键词不带参数", async () => {
  const transport = new FakeTransport();
  const svc = await newServices(transport);
  transport.enqueue(200, JSON.stringify({
    data: { items: [{ id: "faq_1", question: "怎么找回账号", answer: "点忘记密码" }], nextCursor: "" },
  }));

  const page = await svc.support.faqAsync("找回 账号", 20);

  assert.equal(transport.requests[1].url,
    "https://api.example.com/v1/support/faq?limit=20&keyword=" + encodeURIComponent("找回 账号"));
  assert.equal(page.items[0].id, "faq_1");

  transport.enqueue(200, JSON.stringify({ data: { items: [], nextCursor: "" } }));
  await svc.support.faqAsync(undefined, 20);
  assert.equal(transport.requests[2].url, "https://api.example.com/v1/support/faq?limit=20");
});

// ---- v1.1:公告定向过滤 + 工单上下文 ----

test("公告列表:v1.1 定向参数进查询串;缺省维度不带参数", async () => {
  const transport = new FakeTransport();
  const svc = await newServices(transport);
  transport.enqueue(200, JSON.stringify({ data: { items: [], nextCursor: "" } }));

  await svc.announcements.listAsync(20, "cur-1", { platform: "ios", region: "CN" });

  const req = transport.requests[1];
  assert.equal(req.url,
    "https://api.example.com/v1/announcements?limit=20&cursor=cur-1&platform=ios&region=CN");
});

test("公告列表:v1.1 定向参数缺省 = 维度不过滤(无查询参数)", async () => {
  const transport = new FakeTransport();
  const svc = await newServices(transport);
  transport.enqueue(200, JSON.stringify({ data: { items: [], nextCursor: "" } }));

  await svc.announcements.listAsync(20);

  const req = transport.requests[1];
  assert.equal(req.url, "https://api.example.com/v1/announcements?limit=20");
});

test("提单:v1.1 context 开集 map 原样透传(键值类型 string/number/boolean)", async () => {
  const transport = new FakeTransport();
  const svc = await newServices(transport);
  transport.enqueue(200, JSON.stringify({
    data: {
      id: "tkt_3", title: "t", status: "OPEN", createdAt: "t", updatedAt: "t",
      context: { orderId: "o-1", vip: true, level: 42 },
    },
  }));

  const ticket = await svc.support.createTicketAsync({
    title: "t", body: "b", context: { orderId: "o-1", vip: true, level: 42 },
  });

  const req = transport.requests[1];
  assert.deepEqual(JSON.parse(req.jsonBody ?? "{}").context,
    { orderId: "o-1", vip: true, level: 42 });
  assert.deepEqual(ticket.context, { orderId: "o-1", vip: true, level: 42 });
});

// ---- v1.1 F45:举报与处罚申诉 ----

test("举报:受理返回 rpt_ id;detail 缺省键不下发", async () => {
  const transport = new FakeTransport();
  const svc = await newServices(transport);
  transport.enqueue(200, JSON.stringify({ data: { id: "rpt_1" } }));

  const rep = await svc.support.createReportAsync({
    targetType: "PLAYER", targetId: "acc_evil", reason: "CHEATING", detail: "自瞄",
  });

  const req = transport.requests[1];
  assert.equal(req.method, "POST");
  assert.equal(req.url, "https://api.example.com/v1/support/reports");
  assert.deepEqual(JSON.parse(req.jsonBody ?? "{}"),
    { targetType: "PLAYER", targetId: "acc_evil", reason: "CHEATING", detail: "自瞄" });
  assert.equal(rep.id, "rpt_1");

  transport.enqueue(200, JSON.stringify({ data: { id: "rpt_2" } }));
  await svc.support.createReportAsync({ targetType: "TICKET", targetId: "tkt_1", reason: "SPAM" });
  const raw = JSON.parse(transport.requests[2].jsonBody ?? "{}");
  assert.ok(!("detail" in raw));
});

test("处罚(F45):列表 issuedAt 倒序分页 + 详情申诉字段;申诉 409/404 typed 不重试", async () => {
  const transport = new FakeTransport();
  const svc = await newServices(transport);
  transport.enqueue(200, JSON.stringify({
    data: {
      items: [{
        id: "pen_2", type: "SUSPENSION", reason: "停权 30 天", state: "ACTIVE",
        issuedAt: "2026-10-13T10:00:00.000Z",
        expiresAt: "2026-11-12T10:00:00.000Z",
      }],
      nextCursor: "2",
    },
  }));

  const page = await svc.support.listPenaltiesAsync(10, "1");
  assert.equal(transport.requests[1].url,
    "https://api.example.com/v1/support/penalties?limit=10&cursor=1");
  assert.equal(page.items[0].id, "pen_2");
  assert.equal(page.nextCursor, "2");
  assert.equal(page.items[0].appeal, undefined);

  transport.enqueue(200, JSON.stringify({
    data: {
      id: "pen_2", type: "SUSPENSION", reason: "停权 30 天", state: "ACTIVE",
      issuedAt: "2026-10-13T10:00:00.000Z",
      appeal: { state: "PENDING", submittedAt: "2026-10-13T11:00:00.000Z" },
    },
  }));
  const pen = await svc.support.getPenaltyAsync("pen_2");
  assert.equal(transport.requests[2].url,
    "https://api.example.com/v1/support/penalties/pen_2");
  assert.equal(pen.appeal?.state, "PENDING");

  transport.enqueue(200, JSON.stringify({
    data: {
      id: "pen_2", type: "SUSPENSION", reason: "停权 30 天", state: "ACTIVE",
      issuedAt: "2026-10-13T10:00:00.000Z",
      appeal: { state: "PENDING", submittedAt: "2026-10-13T11:00:00.000Z" },
    },
  }));
  const appealed = await svc.support.appealPenaltyAsync("pen_2");
  assert.equal(transport.requests[3].method, "POST");
  assert.equal(transport.requests[3].url,
    "https://api.example.com/v1/support/penalties/pen_2/appeal");
  assert.equal(appealed.appeal?.state, "PENDING");

  transport.enqueue(409, JSON.stringify({
    error: { code: "SUPPORT_APPEAL_EXISTS", message: "已有申诉", retryable: false },
  }));
  const dup = await svc.support.appealPenaltyAsync("pen_2").catch((x) => x);
  assert.equal(dup.code, "SupportAppealExists");
  assert.equal(dup.http, 409);
  assert.equal(transport.requests.length, 5); // 不重试

  transport.enqueue(404, JSON.stringify({
    error: { code: "SUPPORT_PENALTY_NOT_FOUND", message: "不存在", retryable: false },
  }));
  const miss = await svc.support.getPenaltyAsync("pen_nope").catch((x) => x);
  assert.equal(miss.code, "SupportPenaltyNotFound");
  assert.equal(miss.http, 404);
  assert.equal(transport.requests.length, 6); // 不重试
});
