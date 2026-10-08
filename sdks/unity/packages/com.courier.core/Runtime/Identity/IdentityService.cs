// L2 Identity 域:auth.md Frozen v1「端点总览」的客户端面。
// 无状态:token 经 ApiClient 的 AccessTokenProvider 现取;生命周期编排在 CourierClient。
using System.Threading;
using System.Threading.Tasks;
using Courier.Core;

namespace Courier.Identity
{
    public sealed class IdentityService
    {
        const string Prefix = "/v1/identity/";

        readonly ApiClient _api;

        public IdentityService(ApiClient api)
        {
            _api = api;
        }

        public Task<SessionDto> RegisterAsync(RegisterRequest request, CancellationToken ct)
        {
            return Send<SessionDto>("POST", "register", request, false, ct);
        }

        public Task<SessionDto> LoginAsync(LoginRequest request, CancellationToken ct)
        {
            return Send<SessionDto>("POST", "login", request, false, ct);
        }

        public Task<SessionDto> GuestAsync(GuestRequest request, CancellationToken ct)
        {
            return Send<SessionDto>("POST", "guest", request, false, ct);
        }

        /// <summary>游客转正(Bearer)。</summary>
        public Task<AccountDto> BindAsync(BindRequest request, CancellationToken ct)
        {
            return Send<AccountDto>("POST", "bind", request, true, ct);
        }

        public Task<SessionDto> RefreshAsync(RefreshRequest request, CancellationToken ct)
        {
            return Send<SessionDto>("POST", "refresh", request, false, ct);
        }

        public Task LogoutAsync(CancellationToken ct)
        {
            return SendVoid("POST", "logout", null, true, ct);
        }

        public Task<SessionInfoDto> GetSessionAsync(CancellationToken ct)
        {
            return Send<SessionInfoDto>("GET", "session", null, true, ct);
        }

        public Task<DeviceListDto> ListDevicesAsync(CancellationToken ct)
        {
            return Send<DeviceListDto>("GET", "devices", null, true, ct);
        }

        /// <summary>DELETE /v1/identity/devices/{deviceId}(解绑并吊销该设备全部会话)。</summary>
        public Task UnbindDeviceAsync(string deviceId, CancellationToken ct)
        {
            return SendVoid("DELETE", "devices/" + deviceId, null, true, ct);
        }

        async Task<T> Send<T>(string method, string action, object body, bool withAuth, CancellationToken ct)
        {
            var data = await _api.SendAsync(method, Prefix + action, body, withAuth, ct).ConfigureAwait(false);
            return Json.Deserialize<T>(data);
        }

        async Task SendVoid(string method, string action, object body, bool withAuth, CancellationToken ct)
        {
            await _api.SendAsync(method, Prefix + action, body, withAuth, ct).ConfigureAwait(false);
        }
    }
}
