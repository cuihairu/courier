// L2 Core:token 存储接口。L3 Adapter 用平台安全存储实现
// (Unity:加密存储,禁明文 PlayerPrefs —— architecture.md「安全边界」)。
namespace Courier.Core
{
    /// <summary>当前会话的持久化凭证(字段对齐 auth.md Frozen v1 会话 DTO)。</summary>
    public sealed class StoredTokens
    {
        public string AccountId { get; set; }
        public string SessionId { get; set; }
        public string DeviceId { get; set; }
        public string AccessToken { get; set; }
        public string AccessExpiresAt { get; set; }  // RFC3339 ms UTC(契约 primitives.md)
        public string RefreshToken { get; set; }
        public string RefreshExpiresAt { get; set; }
    }

    public interface ITokenStore
    {
        /// <summary>读取持久化凭证;无会话返回 null。</summary>
        StoredTokens Load();
        void Save(StoredTokens tokens);
        void Clear();
    }
}
