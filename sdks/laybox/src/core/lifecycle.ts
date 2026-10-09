// L2 Core:生命周期状态机(architecture.md「SDK 生命周期状态机」;事件面契约
// events.md Frozen v1)。状态机平台无关:平台差异(前后台/进程信号)由 L3
// Adapter 注入 Trigger(unity Lifecycle.cs 同构)。
//
//   Uninitialized → Initializing → Ready → Authenticating → Authenticated → PlayerReady
//                                    ▲                        │              │
//                                    │        ┌───────────────┘              │
//                                    │        ▼                              ▼
//                                    └── SignedOut                Suspended ⇄ Resuming

export const LifecycleState = {
  Uninitialized: "Uninitialized",
  Initializing: "Initializing",
  Ready: "Ready",
  Authenticating: "Authenticating",
  Authenticated: "Authenticated",
  PlayerReady: "PlayerReady",
  Suspended: "Suspended",
  Resuming: "Resuming",
  SignedOut: "SignedOut",
} as const;
export type LifecycleState = (typeof LifecycleState)[keyof typeof LifecycleState];

/** 状态机触发器(平台信号与业务动作的统一入口)。 */
export const LifecycleTrigger = {
  InitStarted: "InitStarted",
  InitCompleted: "InitCompleted",
  AuthStarted: "AuthStarted",
  AuthSucceeded: "AuthSucceeded",
  AuthFailed: "AuthFailed",
  EnterPlayerReady: "EnterPlayerReady",
  SignedOut: "SignedOut",
  Suspended: "Suspended",
  ResumeStarted: "ResumeStarted",
  ResumeCompleted: "ResumeCompleted",
} as const;
export type LifecycleTrigger = (typeof LifecycleTrigger)[keyof typeof LifecycleTrigger];

/** 契约生命周期事件(events.md「生命周期事件」之五;
 *  token_expired 由 SessionService 在自动 refresh 开始时发出)。 */
export interface LifecycleEvent {
  readonly type: string;
  readonly from: LifecycleState;
  readonly to: LifecycleState;
}

export type LifecycleListener = (event: LifecycleEvent) => void;

type TransitionTable = Readonly<Record<string, LifecycleState>>;
const key = (state: LifecycleState, trigger: LifecycleTrigger): string => state + ">" + trigger;

/** 合法迁移表(architecture.md 状态图全表;表外一律非法)。 */
const transitions: TransitionTable = Object.freeze({
  [key(LifecycleState.Uninitialized, LifecycleTrigger.InitStarted)]: LifecycleState.Initializing,
  [key(LifecycleState.Initializing, LifecycleTrigger.InitCompleted)]: LifecycleState.Ready,
  [key(LifecycleState.Ready, LifecycleTrigger.AuthStarted)]: LifecycleState.Authenticating,
  [key(LifecycleState.Authenticating, LifecycleTrigger.AuthSucceeded)]: LifecycleState.Authenticated,
  [key(LifecycleState.Authenticating, LifecycleTrigger.AuthFailed)]: LifecycleState.Ready,
  [key(LifecycleState.Authenticated, LifecycleTrigger.EnterPlayerReady)]: LifecycleState.PlayerReady,
  [key(LifecycleState.Authenticated, LifecycleTrigger.SignedOut)]: LifecycleState.SignedOut,
  [key(LifecycleState.Authenticated, LifecycleTrigger.Suspended)]: LifecycleState.Suspended,
  [key(LifecycleState.PlayerReady, LifecycleTrigger.SignedOut)]: LifecycleState.SignedOut,
  [key(LifecycleState.PlayerReady, LifecycleTrigger.Suspended)]: LifecycleState.Suspended,
  [key(LifecycleState.Suspended, LifecycleTrigger.ResumeStarted)]: LifecycleState.Resuming,
  // Resuming → ResumeCompleted 回挂起前稳定态(表外特判 _preSuspend)。
  [key(LifecycleState.SignedOut, LifecycleTrigger.AuthStarted)]: LifecycleState.Authenticating,
});

/** 状态机触发 → 契约事件 type 映射;非契约触发返回 null(不对外发)。 */
function eventOf(trigger: LifecycleTrigger): string | null {
  switch (trigger) {
    case LifecycleTrigger.InitCompleted:
      return "lifecycle.initialized"; // Init 完成,进入 Ready
    case LifecycleTrigger.SignedOut:
      return "lifecycle.signed_out"; // 登出/被踢(未认证)
    case LifecycleTrigger.Suspended:
      return "lifecycle.suspended"; // 切后台/断网
    case LifecycleTrigger.ResumeCompleted:
      return "lifecycle.resumed"; // 恢复,重新可用
    default:
      return null;
  }
}

/** 生命周期状态机:非法触发 fire 抛错(严格表,全事件单测覆盖);
 *  tryFire 非抛版供 Adapter 平台信号幂等使用(平台消息处理器不许崩)。 */
export class LifecycleMachine {
  private state: LifecycleState = LifecycleState.Uninitialized;
  private preSuspend: LifecycleState = LifecycleState.Uninitialized; // Suspended 前的稳定态
  private lastAccountId: string | null = null; // 切号检测(契约 events.md account_switched)
  private readonly listeners: LifecycleListener[] = [];

  get current(): LifecycleState {
    return this.state;
  }

  /** 订阅契约生命周期事件;返回退订函数。 */
  onEvent(listener: LifecycleListener): () => void {
    this.listeners.push(listener);
    return () => {
      const i = this.listeners.indexOf(listener);
      if (i >= 0) {
        this.listeners.splice(i, 1);
      }
    };
  }

  /** 推进状态机;成功时发出对应契约事件。非法转移抛错。 */
  fire(trigger: LifecycleTrigger): LifecycleState {
    if (!this.tryFire(trigger)) {
      throw new Error("invalid lifecycle transition: " + this.state + " --" + trigger + "--> ?");
    }
    return this.state;
  }

  /** 非抛版:非法转移返回 false 且状态不变(Adapter 平台信号幂等用)。 */
  tryFire(trigger: LifecycleTrigger): boolean {
    if (this.state === LifecycleState.Resuming && trigger === LifecycleTrigger.ResumeCompleted) {
      this.transition(trigger, this.preSuspend);
      return true;
    }
    const target = transitions[key(this.state, trigger)];
    if (target === undefined) {
      return false;
    }
    this.transition(trigger, target);
    return true;
  }

  private transition(trigger: LifecycleTrigger, target: LifecycleState): void {
    if (target === LifecycleState.Suspended) {
      this.preSuspend = this.state;
    }
    const from = this.state;
    this.state = target;
    const type = eventOf(trigger);
    if (type !== null) {
      const event: LifecycleEvent = { type, from, to: target };
      for (const listener of [...this.listeners]) {
        listener(event);
      }
    }
  }

  /** 切号检测:新账号与上次不同 → account_switched(SessionService 调用)。
   *  首次登录不算切换;同号重登不算切换。 */
  reportAccountId(accountId: string | null): boolean {
    const switched = this.lastAccountId !== null && accountId !== null &&
      this.lastAccountId !== accountId;
    this.lastAccountId = accountId ?? this.lastAccountId;
    if (switched) {
      this.emit("lifecycle.account_switched");
    }
    return switched;
  }

  /** access 过期,自动 refresh 开始(SessionService 调用);状态不变。 */
  raiseTokenExpired(): void {
    this.emit("lifecycle.token_expired");
  }

  private emit(type: string): void {
    const event: LifecycleEvent = { type, from: this.state, to: this.state };
    for (const listener of [...this.listeners]) {
      listener(event);
    }
  }
}
