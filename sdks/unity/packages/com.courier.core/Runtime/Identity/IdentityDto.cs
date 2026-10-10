// L2 Identity 域 DTO(auth.md Frozen v1,字段逐一对应 wire camelCase;
// Newtonsoft camelCase resolver 统一映射,未知字段容忍)。
// 可选字段不设 = 序列化缺省(Json.NullValueHandling.Ignore)。
using System.Collections.Generic;

namespace Courier.Identity
{
    // --- 请求 ---

    /// <summary>POST /v1/identity/register</summary>
    public sealed class RegisterRequest
    {
        public string Email { get; set; }
        public string Password { get; set; }
        public string DeviceId { get; set; }    // 可选
        public string Platform { get; set; }    // 可选
    }

    /// <summary>POST /v1/identity/login</summary>
    public sealed class LoginRequest
    {
        public string Email { get; set; }
        public string Password { get; set; }
        public string DeviceId { get; set; }    // 可选
        public string Platform { get; set; }    // 可选
    }

    /// <summary>POST /v1/identity/guest(按设备幂等)</summary>
    public sealed class GuestRequest
    {
        public string DeviceId { get; set; }
        public string Platform { get; set; }    // 可选
    }

    /// <summary>POST /v1/identity/bind(游客转正)</summary>
    public sealed class BindRequest
    {
        public string Email { get; set; }
        public string Password { get; set; }
    }

    /// <summary>POST /v1/identity/refresh</summary>
    public sealed class RefreshRequest
    {
        public string RefreshToken { get; set; }
    }

    // --- 响应 ---

    public sealed class AccountDto
    {
        public string Id { get; set; }
        public string Type { get; set; }        // EMAIL / GUEST
        public string Email { get; set; }       // GUEST 时缺省
        public string Status { get; set; }      // ACTIVE / DISABLED
        public string CreatedAt { get; set; }   // RFC3339 ms UTC
    }

    /// <summary>登录/注册/游客/refresh 的会话响应。</summary>
    public sealed class SessionDto
    {
        public AccountDto Account { get; set; }
        public string AccessToken { get; set; }
        public string AccessExpiresAt { get; set; }
        public string RefreshToken { get; set; }
        public string RefreshExpiresAt { get; set; }
        public string DeviceId { get; set; }
    }

    /// <summary>GET /v1/identity/session。</summary>
    public sealed class SessionInfoDto
    {
        public AccountDto Account { get; set; }
        public string SessionId { get; set; }
        public string DeviceId { get; set; }
    }

    public sealed class DeviceDto
    {
        public string Id { get; set; }
        public string DeviceId { get; set; }
        public string Platform { get; set; }
        public string CreatedAt { get; set; }
    }

    /// <summary>GET /v1/identity/devices(分页信封:items + nextCursor;缺失/空 = 末页)。</summary>
    public sealed class DeviceListDto
    {
        public List<DeviceDto> Items { get; set; }
        public string NextCursor { get; set; }
    }
}
