// L2 Session 域:token 轮换与生命周期(auth.md「Token 模型」)。
// - access 过期自动 refresh(单飞);AUTH_REFRESH_REUSED / AUTH_TOKEN_REVOKED → 清本地 + signed_out
// - 持久化经 ITokenStore(L3 安全存储);时间判定经 IClock 注入
using System;
using System.Globalization;
using System.Threading;
using System.Threading.Tasks;
using Courier.Core;
using Courier.Identity;

namespace Courier.Session
{
    public sealed class SessionService
    {
        static readonly TimeSpan ExpirySkew = TimeSpan.FromSeconds(30);

        readonly ApiClient _api;
        readonly IdentityService _identity;
        readonly ITokenStore _store;
        readonly IClock _clock;
        readonly LifecycleMachine _lifecycle;
        readonly SemaphoreSlim _refreshLock = new SemaphoreSlim(1, 1);

        public SessionService(ApiClient api, IdentityService identity, ITokenStore store,
            IClock clock, LifecycleMachine lifecycle)
        {
            _api = api;
            _identity = identity;
            _store = store;
            _clock = clock;
            _lifecycle = lifecycle;
        }

        /// <summary>当前凭证;未认证为 null。</summary>
        public StoredTokens Current { get; private set; }

        public bool IsAuthenticated { get { return Current != null; } }

        /// <summary>登录/注册/游客/refresh 成功后写入并持久化;切号时发 account_switched。</summary>
        public void SetSession(SessionDto dto)
        {
            Current = new StoredTokens
            {
                AccountId = dto.Account == null ? null : dto.Account.Id,
                SessionId = null,  // 会话 ID 不在会话 DTO;经 GET /session 或设备列表获取
                DeviceId = dto.DeviceId,
                AccessToken = dto.AccessToken,
                AccessExpiresAt = dto.AccessExpiresAt,
                RefreshToken = dto.RefreshToken,
                RefreshExpiresAt = dto.RefreshExpiresAt
            };
            _store.Save(Current);
            _lifecycle.ReportAccountId(Current.AccountId);
        }

        /// <summary>Init 时恢复:refresh 已过期 → 清本地(留在 Ready);有效 → 恢复会话态。</summary>
        public async Task<bool> RestoreAsync(CancellationToken ct)
        {
            Current = _store.Load();
            if (Current == null || string.IsNullOrEmpty(Current.RefreshToken))
            {
                Current = null;
                return false;
            }
            if (IsExpired(Current.RefreshExpiresAt))
            {
                ClearLocal();
                return false;
            }
            // 恢复即认证态:access 过期的先换新。
            if (IsExpired(Current.AccessExpiresAt))
            {
                var ok = await TryRefreshAsync(ct).ConfigureAwait(false);
                return ok;
            }
            _lifecycle.ReportAccountId(Current.AccountId);
            return true;
        }

        /// <summary>可用的 access token;过期自动 refresh;未认证抛 COMMON_UNAUTHENTICATED。</summary>
        public async Task<string> GetValidAccessTokenAsync(CancellationToken ct)
        {
            if (Current == null)
            {
                throw new CourierException(CourierError.Unauthenticated("not authenticated"));
            }
            if (IsExpired(Current.AccessExpiresAt) &&
                !await TryRefreshAsync(ct).ConfigureAwait(false))
            {
                throw new CourierException(CourierError.Unauthenticated("session expired"));
            }
            return Current.AccessToken;
        }

        /// <summary>refresh 轮换(单飞)。安全事件(REUSED/REVOKED)→ 清本地 + signed_out。
        /// 返回是否拿到新凭证。</summary>
        public async Task<bool> TryRefreshAsync(CancellationToken ct)
        {
            await _refreshLock.WaitAsync(ct).ConfigureAwait(false);
            try
            {
                if (Current == null)
                {
                    return false;
                }
                _lifecycle.RaiseTokenExpired();
                try
                {
                    var dto = await _identity.RefreshAsync(
                        new RefreshRequest { RefreshToken = Current.RefreshToken }, ct).ConfigureAwait(false);
                    SetSession(dto);
                    return true;
                }
                catch (CourierException e)
                {
                    var wire = e.Error == null ? null : e.Error.WireCode;
                    if (wire == "AUTH_REFRESH_REUSED" || wire == "AUTH_TOKEN_REVOKED")
                    {
                        // 安全事件:整个会话已服务端吊销,本地清场(契约 auth.md「重放检测」)。
                        ClearLocal();
                        if (_lifecycle.State == LifecycleState.Authenticated ||
                            _lifecycle.State == LifecycleState.PlayerReady)
                        {
                            _lifecycle.Fire(LifecycleTrigger.SignedOut);
                        }
                        return false;
                    }
                    throw;
                }
            }
            finally
            {
                _refreshLock.Release();
            }
        }

        /// <summary>登出:服务端吊销(尽力而为)+ 本地清场 + signed_out。</summary>
        public async Task SignOutAsync(CancellationToken ct)
        {
            try
            {
                await _identity.LogoutAsync(ct).ConfigureAwait(false);
            }
            catch (CourierException)
            {
                // 服务端已不可用/已吊销:本地清场即达成登出语义(幂等)。
            }
            ClearLocal();
            if (_lifecycle.State == LifecycleState.Authenticated ||
                _lifecycle.State == LifecycleState.PlayerReady)
            {
                _lifecycle.Fire(LifecycleTrigger.SignedOut);
            }
        }

        /// <summary>清本地凭证(含持久化)。</summary>
        public void ClearLocal()
        {
            Current = null;
            _store.Clear();
        }

        bool IsExpired(string expiresAtIso)
        {
            if (string.IsNullOrEmpty(expiresAtIso))
            {
                return true;
            }
            DateTimeOffset expiresAt;
            if (!DateTimeOffset.TryParse(expiresAtIso, CultureInfo.InvariantCulture,
                    DateTimeStyles.AssumeUniversal, out expiresAt))
            {
                return true;
            }
            return _clock.UtcNow >= expiresAt - ExpirySkew;
        }
    }
}
