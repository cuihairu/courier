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

        /// <summary>POST /v1/identity/account/delete(v1.1):受理即吊销全部会话。
        /// EMAIL 走宽限期(响应 PENDING_DELETE + 一次性 CancelToken),GUEST 立即删;
        /// 宽限期内登录被拒(AUTH_ACCOUNT_PENDING_DELETE)。</summary>
        public Task<DeleteAccountDto> DeleteAccountAsync(CancellationToken ct)
        {
            return Send<DeleteAccountDto>("POST", "account/delete", null, true, ct);
        }

        /// <summary>POST /v1/identity/account/delete/cancel(v1.1,匿名):宽限期内撤销,
        /// 一次性 CancelToken;已吊销会话不恢复,需重登;token 无效/过期 → AUTH_DELETE_TOKEN_INVALID。</summary>
        public Task CancelDeleteAsync(string cancelToken, CancellationToken ct)
        {
            return SendVoid("POST", "account/delete/cancel",
                new CancelDeleteRequest { CancelToken = cancelToken }, false, ct);
        }

        /// <summary>POST /v1/identity/oauth/exchange(v1.1 oauth.md):第三方凭证换会话,
        /// 无匹配即创建 type=OAUTH 账号,命中即登录同账号(每次签发新会话);
        /// OAUTH_IDENTITY_INVALID → 引导重新授权,不自动重试;
        /// OAUTH_PROVIDER_UNKNOWN → 该登录方式不可用(可能未配置)。</summary>
        public Task<SessionDto> ExchangeAsync(OAuthExchangeRequest request, CancellationToken ct)
        {
            return Send<SessionDto>("POST", "oauth/exchange", request, false, ct);
        }

        /// <summary>POST /v1/identity/oauth/bind(v1.1 oauth.md;Bearer):绑定第三方身份,
        /// 供下次一键登录;OAUTH_IDENTITY_TAKEN → 该身份已绑定其他账号,可引导改用该账号登录。</summary>
        public Task<OAuthBindingDto> OauthBindAsync(string provider, string token, CancellationToken ct)
        {
            return Send<OAuthBindingDto>("POST", "oauth/bind",
                new OauthBindRequest { Provider = provider, Token = token }, true, ct);
        }

        /// <summary>DELETE /v1/identity/oauth/bindings/{provider}(v1.1 oauth.md;Bearer,幂等;
        /// 解绑不影响账号本身)。</summary>
        public Task OauthUnbindAsync(string provider, CancellationToken ct)
        {
            return SendVoid("DELETE", "oauth/bindings/" + provider, null, true, ct);
        }

        /// <summary>GET /v1/identity/oauth/bindings(v1.1 oauth.md;Bearer;按 CreatedAt 升序,
        /// 不返回 Subject)。</summary>
        public Task<OAuthBindingListDto> OauthBindingsAsync(CancellationToken ct)
        {
            return Send<OAuthBindingListDto>("GET", "oauth/bindings", null, true, ct);
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
