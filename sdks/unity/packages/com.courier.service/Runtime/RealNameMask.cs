// 实名脱敏口径(契约 realname.md「UI 展示:一律脱敏」)。
// 全 SDK 唯一口径实现,接入方不得自造:姓名保留姓氏余 *,
// 证件号前 3 后 4 余 *。纯函数、平台无关(dotnet 可测),UI 面板与接入方共用。
using System.Text;

namespace Courier.Service
{
    public static class RealNameMask
    {
        /// <summary>姓名:保留姓氏(首字),其余 *(张三 → 张\*,李四喜 → 李\*\*)。</summary>
        public static string MaskName(string name)
        {
            if (string.IsNullOrEmpty(name))
            {
                return "";
            }
            var sb = new StringBuilder(name.Length);
            sb.Append(name[0]);
            for (var i = 1; i < name.Length; i++)
            {
                sb.Append('*');
            }
            return sb.ToString();
        }

        /// <summary>证件号:前 3 后 4,中间 *(110101199001011234 → 110\*\*\*\*\*\*\*\*\*\*\*\*1234)。
        /// 长度异常(非 8+):只出 *,不回显任何原文段。</summary>
        public static string MaskIdNumber(string idNumber)
        {
            if (string.IsNullOrEmpty(idNumber))
            {
                return "";
            }
            if (idNumber.Length < 8)
            {
                var all = new StringBuilder(idNumber.Length);
                for (var i = 0; i < idNumber.Length; i++)
                {
                    all.Append('*');
                }
                return all.ToString();
            }
            var sb = new StringBuilder(idNumber.Length);
            sb.Append(idNumber, 0, 3);
            for (var i = 3; i < idNumber.Length - 4; i++)
            {
                sb.Append('*');
            }
            sb.Append(idNumber, idNumber.Length - 4, 4);
            return sb.ToString();
        }
    }
}
