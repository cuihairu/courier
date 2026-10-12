// Courier Cocos Creator SDK 最小可跑示例:接入 → 登录 → 收公告 → 发工单。
//
// 运行(零依赖,Node 24 直接跑 TS):
//   1) 起本地网关(仓库 gateway 模块,无外部依赖):
//      cd gateway && go build -o /tmp/courier-gateway ./cmd/gateway && /tmp/courier-gateway
//      默认监听 :8080,可 COURIER_GATEWAY_ADDR=:18099 覆盖
//   2) 跑示例:
//      node examples/cocos/example.ts
//      COURIER_ENDPOINT=http://127.0.0.1:18099 node examples/cocos/example.ts
//
// 接入点只有两个:config(gameId/env = scope 头)+ Transport(fetch 默认实现);
// Cocos 项目内把 import 路径换成你的包导入路径,其余原样。
import { CourierClient } from "../../sdks/cocos/src/core/courierClient.ts";
import { FetchTransport } from "../../sdks/cocos/src/core/transport.ts";
import { CourierServices } from "../../sdks/cocos/src/service/courierServices.ts";

const endpoint = process.env.COURIER_ENDPOINT ?? "http://127.0.0.1:8080";

async function main(): Promise<void> {
  // 1) 接入:config 是 scope(gameId/env),传输默认走 WHATWG fetch。
  const client = new CourierClient({
    config: { endpoint, gameId: "game_demo", env: "prod" },
    transport: new FetchTransport(),
  });
  client.init();
  const services = new CourierServices(client);

  // 2) 登录:游客静默登录(不阻断进游戏)。
  const session = await client.guestLoginAsync({ deviceId: "example-device-1" });
  console.log("login: type=" + session.account.type + " id=" + session.account.id);

  // 3) 收公告:匿名可见;本地网关无发布数据时空列表属正常。
  const page = await services.announcements.listAsync(20);
  console.log("announcements: " + page.items.length);
  for (const item of page.items) {
    console.log("  [" + item.severity + "] " + item.title);
  }

  // 4) 发工单:提交后拉详情,消息含玩家首条正文。
  const ticket = await services.support.createTicketAsync({
    title: "示例:闪退反馈",
    body: "启动即崩溃,登录账号 " + session.account.id,
  });
  console.log("ticket: id=" + ticket.id + " status=" + ticket.status);
  const detail = await services.support.getTicketAsync(ticket.id);
  console.log("detail: messages=" + detail.messages.length);
}

main().then(() => console.log("example: done")).catch((err) => {
  console.error("example failed: " + String(err));
  process.exit(1);
});
