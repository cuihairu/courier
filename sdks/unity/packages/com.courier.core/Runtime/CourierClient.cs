// L2 门面:CourierClient.Init({ gameId, env, endpoint }) → 按域子模块(layers.md L2)。
// 编排:配置校验 → 状态机 → token 恢复;登录族统走 AuthStarted/…/EnterPlayerReady。
// 平台无关:传输/存储/时钟经注入接口,L3 Adapter 提供实现。
using System;
using System.Threading;
using System.Threading.Tasks;
using Courier.Core;
using Courier.Identity;
using Courier.Session;

namespace Courier
{
    public sealed class CourierClient
    {
        readonly CourierConfig _config;
        readonly ApiClient _api;
        readonly IdentityService _identity;
        readonly SessionService _session;

        public CourierConfig Config { get { return _config; } }
        public LifecycleMachine Lifecycle { get; }
        public IdentityService Identity { get { return _identity; } }
        public SessionService Session { get { return _session; } }

        public CourierClient(CourierConfig config, ITransport transport, ITokenStore tokenStore,
            IClock clock = null, RetryPolicy retry = null)
        {
            _config = config;
            Lifecycle = new LifecycleMachine();
            _api = new ApiClient(config, transport, retry)
            {
                AccessTokenProvider = () => _session == null || _session.Current == null
                    ? null
                    : _session.Current.AccessToken,
                RefreshHook = async () => _session != null &&
                    await _session.TryRefreshAsync(CancellationToken.None).ConfigureAwait(false)
            };
            _identity = new IdentityService(_api);
            _session = new SessionService(_api, _identity, tokenStore, clock ?? SystemClock.Instance, Lifecycle);
        }

        /// <summary>初始化:校验配置 → 恢复会话 → Ready →(恢复出的会话)进入认证态。</summary>
        public async Task InitAsync(CancellationToken ct)
        {
            _config.Validate();
            Lifecycle.Fire(LifecycleTrigger.InitStarted);
            var restored = await _session.RestoreAsync(ct).ConfigureAwait(false);
            Lifecycle.Fire(LifecycleTrigger.InitCompleted);
            if (restored)
            {
                // 恢复出的会话:静默进入认证态(AuthStarted/AuthSucceeded 非契约事件,不外发)。
                Lifecycle.Fire(LifecycleTrigger.AuthStarted);
                Lifecycle.Fire(LifecycleTrigger.AuthSucceeded);
                Lifecycle.Fire(LifecycleTrigger.EnterPlayerReady);
            }
        }

        /// <summary>邮箱注册并登录(auth.md POST /register)。</summary>
        public async Task<SessionDto> RegisterAsync(RegisterRequest request, CancellationToken ct)
        {
            return await AuthAsync(() => _identity.RegisterAsync(request, ct), ct).ConfigureAwait(false);
        }

        public async Task<SessionDto> LoginAsync(LoginRequest request, CancellationToken ct)
        {
            return await AuthAsync(() => _identity.LoginAsync(request, ct), ct).ConfigureAwait(false);
        }

        /// <summary>游客登录(按设备幂等,auth.md POST /guest)。</summary>
        public async Task<SessionDto> GuestAsync(GuestRequest request, CancellationToken ct)
        {
            return await AuthAsync(() => _identity.GuestAsync(request, ct), ct).ConfigureAwait(false);
        }

        /// <summary>游客转正(当前会话 Bearer;转正不换会话,状态不变)。</summary>
        public Task<AccountDto> BindAsync(BindRequest request, CancellationToken ct)
        {
            return _identity.BindAsync(request, ct);
        }

        /// <summary>登出:服务端吊销 + 本地清场 + signed_out。</summary>
        public Task SignOutAsync(CancellationToken ct)
        {
            return _session.SignOutAsync(ct);
        }

        /// <summary>登录族编排:Ready/SignedOut → Authenticating → Authenticated → PlayerReady。</summary>
        async Task<SessionDto> AuthAsync(Func<Task<SessionDto>> call, CancellationToken ct)
        {
            if (Lifecycle.State != LifecycleState.Ready && Lifecycle.State != LifecycleState.SignedOut)
            {
                throw new InvalidOperationException(
                    "login requires state Ready/SignedOut, current: " + Lifecycle.State);
            }
            Lifecycle.Fire(LifecycleTrigger.AuthStarted);
            try
            {
                var dto = await call().ConfigureAwait(false);
                _session.SetSession(dto);
                Lifecycle.Fire(LifecycleTrigger.AuthSucceeded);
                Lifecycle.Fire(LifecycleTrigger.EnterPlayerReady);
                return dto;
            }
            catch
            {
                if (Lifecycle.State == LifecycleState.Authenticating)
                {
                    Lifecycle.Fire(LifecycleTrigger.AuthFailed);
                }
                throw;
            }
        }
    }
}
