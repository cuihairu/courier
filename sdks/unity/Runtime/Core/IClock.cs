// L2 Core:时钟注入(纯逻辑单测用 FixedClock;平台无需真实时间源时由 Adapter 覆盖)。
using System;

namespace Courier.Core
{
    public interface IClock
    {
        DateTimeOffset UtcNow { get; }
    }

    /// <summary>默认系统时钟(UTC)。</summary>
    public sealed class SystemClock : IClock
    {
        public static readonly SystemClock Instance = new SystemClock();
        public DateTimeOffset UtcNow { get { return DateTimeOffset.UtcNow; } }
    }
}
