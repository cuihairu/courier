// Courier Unity SDK 最小可跑示例:接入 → 登录 → 收公告 → 发工单。
//
// SDK 纯逻辑层(Core/Service)零引擎依赖,故本示例在 dotnet 下即可实测全链路
// (CI 口径一致);进引擎时只换两件:传输与存储(见文末「进引擎替换」)。
//
// 运行:
//   1) 起本地网关:cd gateway && go build -o /tmp/courier-gateway ./cmd/gateway
//      && /tmp/courier-gateway(默认 :8080,COURIER_GATEWAY_ADDR 可覆盖)
//   2) dotnet run --project examples/unity -- <endpoint>
//      例:dotnet run --project examples/unity -- http://127.0.0.1:18099
//
// 接入点只有两个:CourierConfig(gameId/env = scope 头)+ ITransport/ITokenStore 适配。
using System;
using System.Collections.Generic;
using System.Net.Http;
using System.Text;
using System.Threading;
using System.Threading.Tasks;
using Courier.Core;
using Courier.Identity;
using Courier.Service;

// CourierClient 在根命名空间 Courier,其余(Core 传输/存储、Identity DTO、Service 域件)
// 在各自子命名空间。
using Courier;

// 接入方实现 ITransport:引擎内换 UnityWebRequest,形态不变(网络异常归一 TransportException)。
public sealed class HttpClientTransport : ITransport
{
    private readonly HttpClient _http = new HttpClient();

    public async Task<TransportResponse> SendAsync(TransportRequest request, CancellationToken ct)
    {
        try
        {
            using var msg = new HttpRequestMessage(new HttpMethod(request.Method), request.Url);
            foreach (var kv in request.Headers)
            {
                msg.Headers.TryAddWithoutValidation(kv.Key, kv.Value);
            }
            if (request.JsonBody != null)
            {
                msg.Content = new StringContent(request.JsonBody, Encoding.UTF8, "application/json");
            }
            using var resp = await _http.SendAsync(msg, ct).ConfigureAwait(false);
            var body = await resp.Content.ReadAsStringAsync(ct).ConfigureAwait(false);
            return new TransportResponse { StatusCode = (int)resp.StatusCode, Headers = new Dictionary<string, string>(), Body = body };
        }
        catch (Exception e)
        {
            throw new TransportException("http transport failed: " + e.Message, e);
        }
    }
}

// 演示用内存存储;正式接入换成平台安全存储(架构红线:禁明文 PlayerPrefs)。
public sealed class MemoryTokenStore : ITokenStore
{
    private StoredTokens _tokens;

    public StoredTokens Load() { return _tokens; }
    public void Save(StoredTokens tokens) { _tokens = tokens; }
    public void Clear() { _tokens = null; }
}

public static class Program
{
    public static async Task Main(string[] args)
    {
        var endpoint = args.Length > 0 ? args[0] : "http://127.0.0.1:8080";

        // 1) 接入:config 是 scope(gameId/env),传输与存储为接入方适配件。
        var client = new CourierClient(
            new CourierConfig("game_demo", "prod", endpoint),
            new HttpClientTransport(),
            new MemoryTokenStore());
        var services = new CourierServices(client);

        // 初始化:空存储不发请求,只是把生命周期机置 Ready(后续登录编排需要)。
        await client.InitAsync(CancellationToken.None);

        // 2) 登录:游客静默登录(不阻断进游戏)。
        var session = await client.GuestAsync(
            new GuestRequest { DeviceId = "example-device-1" }, CancellationToken.None);
        Console.WriteLine("login: type=" + session.Account.Type + " id=" + session.Account.Id);

        // 3) 收公告:匿名可见;本地网关无发布数据时空列表属正常。
        var page = await services.Announcements.ListAsync(20, null, CancellationToken.None);
        Console.WriteLine("announcements: " + page.Items.Count);
        foreach (var item in page.Items)
        {
            Console.WriteLine("  [" + item.Severity + "] " + item.Title);
        }

        // 4) 发工单:提交后拉详情,消息含玩家首条正文。
        var ticket = await services.Support.CreateTicketAsync(new CreateTicketRequest
        {
            Title = "示例:闪退反馈",
            Body = "启动即崩溃,登录账号 " + session.Account.Id,
        }, CancellationToken.None);
        Console.WriteLine("ticket: id=" + ticket.Id + " status=" + ticket.Status);
        var detail = await services.Support.GetTicketAsync(ticket.Id, CancellationToken.None);
        Console.WriteLine("detail: messages=" + detail.Messages.Count);
        Console.WriteLine("example: done");
    }
}
