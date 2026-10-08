// M2 推送通道帧解析(契约 messages.md:SSE text/event-stream)。
// 纯逻辑、平台无关:流式读入的行喂进来,完整帧析出事件。
// 心跳注释(: ping)忽略;未知 event type 原样吐出——调用方按契约容忍未知。
using System.Text;

namespace Courier.Service
{
    /// <summary>SSE 单事件(type 两段小写,data 单行 JSON,序号)。</summary>
    public sealed class SseEvent
    {
        public string Type { get; set; }
        public string Data { get; set; }
        public string Id { get; set; }
    }

    /// <summary>SSE 帧解析器(增量):按行喂入,遇空行结算一帧。
    /// data 多行时按 SSE 规范以 \n 连接(契约 data 为单行 JSON,此为健壮性)。</summary>
    public sealed class SseParser
    {
        StringBuilder _data = new StringBuilder();
        string _type;
        string _id;
        bool _inFrame;

        /// <summary>喂入一行(不含换行符)。返回结算出的事件;否则 null。</summary>
        public SseEvent FeedLine(string line)
        {
            if (line == null)
            {
                line = "";
            }
            if (line.Length == 0)
            {
                // 空行 = 帧结束。
                if (!_inFrame)
                {
                    return null; // 心跳/连接初始化(: connected)后无内容的空行
                }
                var evt = new SseEvent
                {
                    Type = _type ?? "",
                    Data = _data.ToString(),
                    Id = _id
                };
                _type = null;
                _id = null;
                _data.Length = 0;
                _inFrame = false;
                // 纯注释帧(心跳)不出事件:注释行不置 _inFrame。
                return evt.Data.Length == 0 && evt.Type.Length == 0 ? null : evt;
            }
            if (line[0] == ':')
            {
                return null; // 注释帧(: ping / : connected)——保活语义,无业务
            }
            _inFrame = true;
            var colon = line.IndexOf(':');
            var field = colon < 0 ? line : line.Substring(0, colon);
            var value = colon < 0 ? "" : line.Substring(colon + 1);
            if (value.Length > 0 && value[0] == ' ')
            {
                value = value.Substring(1); // SSE 规范:冒号后单个前导空格剥除
            }
            switch (field)
            {
                case "event":
                    _type = value;
                    break;
                case "data":
                    if (_data.Length > 0)
                    {
                        _data.Append('\n');
                    }
                    _data.Append(value);
                    break;
                case "id":
                    _id = value;
                    break;
                default:
                    break; // 未知字段容忍(契约 versioning.md)
            }
            return null;
        }

        /// <summary>重置(断线重连后复用)。</summary>
        public void Reset()
        {
            _type = null;
            _id = null;
            _data.Length = 0;
            _inFrame = false;
        }
    }
}
