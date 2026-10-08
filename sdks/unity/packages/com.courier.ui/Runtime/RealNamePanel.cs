// 批次 7 UI 包:实名列表面板骨架(todo 批次 7「脱敏口径落 UI」)。
// 边界同批次 6 双面板:编排与文本产出在包内,控件绑定经事件交游戏侧。
// 红线:姓名/证件号只经提交传输一次——面板不缓存明文,展示一律走
// RealNameMask(service 包统一口径),接入方不得自造。
// S2S 上报(playtime-report)是接入方服务端职责,面板不涉及。
using System;
using System.Threading;
using System.Threading.Tasks;
using Courier.Service;
using UnityEngine;

namespace Courier.UI
{
    public sealed class RealNamePanel : MonoBehaviour
    {
        public enum DisplayState
        {
            Disabled,        // 能力未开启(null):隐藏实名入口
            Unverified,      // 未提交:引导进入实名流程
            PendingReview,   // 安全态:提示审核中,不引导重试
            Verified,
            Rejected
        }

        [Tooltip("宿主装配:与登录后的 CourierClient 同生命周期")]
        public CourierServices Services;

        /// <summary>状态渲染出口(含脱敏后的展示文案)。</summary>
        public event Action<DisplayState, string> StateRendered;

        /// <summary>错误出口:typed 错误码(参数/限流等)交游戏侧提示。</summary>
        public event Action<string> ErrorRendered;

        DisplayState _state = DisplayState.Unverified;
        string _lastSubmittedMasked; // 只留脱敏指纹用于展示,不留明文(红线)

        public DisplayState CurrentState { get { return _state; } }

        /// <summary>刷新状态(F27)。能力未开启 → Disabled(隐藏入口,不报错)。</summary>
        public async Task RefreshAsync(CancellationToken ct)
        {
            if (Services == null)
            {
                return;
            }
            var status = await Services.RealName.StatusAsync(ct); // null = 未启用
            if (status == null)
            {
                SetState(DisplayState.Disabled, "");
                return;
            }
            ApplyStatus(status);
        }

        /// <summary>提交核验(F27):姓名/证件号只在本次调用栈存活。
        /// PENDING_REVIEW 是安全态——不引导重试、不放行受保护操作。</summary>
        public async Task SubmitAsync(string name, string idNumber, CancellationToken ct)
        {
            if (Services == null)
            {
                return;
            }
            try
            {
                var status = await Services.RealName.SubmitAsync(name, idNumber, ct);
                if (status == null)
                {
                    SetState(DisplayState.Disabled, "");
                    return;
                }
                _lastSubmittedMasked = RealNameMask.MaskName(name) + " " +
                    RealNameMask.MaskIdNumber(idNumber); // 展示用脱敏快照
                ApplyStatus(status);
            }
            catch (CourierException ex)
            {
                ErrorRendered?.Invoke(ex.Error.WireCode);
            }
        }

        /// <summary>可玩时段查询(F28):不可玩时文案带 nextWindowAt,不自造口径。</summary>
        public async Task<string> CurfewTextAsync(CancellationToken ct)
        {
            if (Services == null)
            {
                return "";
            }
            var curfew = await Services.RealName.CurfewAsync(ct);
            if (curfew == null)
            {
                return "";
            }
            return curfew.Playable ? "当前可玩"
                : "当前不可玩,下一可玩时段:" + curfew.NextWindowAt;
        }

        void ApplyStatus(RealNameStatusDto status)
        {
            switch (status.State)
            {
                case "VERIFIED":
                    SetState(DisplayState.Verified, "已实名" +
                        (status.IsMinor == true ? "(未成年)" : "") +
                        (_lastSubmittedMasked == null ? "" : "\n" + _lastSubmittedMasked));
                    break;
                case "PENDING_REVIEW":
                    SetState(DisplayState.PendingReview, "审核中");
                    break;
                case "REJECTED":
                    SetState(DisplayState.Rejected, "未通过,请核对后重新提交");
                    break;
                default:
                    SetState(DisplayState.Unverified, "未实名");
                    break;
            }
        }

        void SetState(DisplayState state, string text)
        {
            _state = state;
            StateRendered?.Invoke(state, text);
        }
    }
}
