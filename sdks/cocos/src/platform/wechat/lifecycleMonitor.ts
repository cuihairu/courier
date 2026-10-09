// L3 平台适配(微信小游戏):前后台/断网信号 → 状态机事件(unity
// ApplicationLifecycleMonitor 同构)。resume 恢复到挂起前状态(转移表归 L2);
// 两路信号(onShow/onHide 与网络变化)会成对触发,此处幂等去重;
// 非法转移走 tryFire 不抛(平台消息处理器不许崩)。
import { LifecycleMachine, LifecycleTrigger } from "../../core/lifecycle.ts";
import type { WxAppHooks } from "./wxApi.ts";

export class WechatLifecycleMonitor {
  private readonly lifecycle: LifecycleMachine;
  private suspended = false;

  constructor(lifecycle: LifecycleMachine) {
    this.lifecycle = lifecycle;
  }

  /** 注册平台信号(建议客户端 init 后立即绑定)。 */
  bind(wx: WxAppHooks): void {
    wx.onShow(() => this.resume());
    wx.onHide(() => this.suspend());
    wx.onNetworkStatusChange((res) => {
      if (res.isConnected) {
        this.resume();
      } else {
        this.suspend();
      }
    });
  }

  private suspend(): void {
    if (this.suspended) {
      return;
    }
    if (this.lifecycle.tryFire(LifecycleTrigger.Suspended)) {
      this.suspended = true;
    }
  }

  private resume(): void {
    if (!this.suspended) {
      return;
    }
    if (this.lifecycle.tryFire(LifecycleTrigger.ResumeStarted) &&
      this.lifecycle.tryFire(LifecycleTrigger.ResumeCompleted)) {
      this.suspended = false;
    }
  }
}
