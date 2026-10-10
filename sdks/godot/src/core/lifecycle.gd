# L2 Core:生命周期状态机(architecture.md「SDK 生命周期状态机」;事件面契约
# events.md Frozen v1)。状态机平台无关:平台差异(前后台/进程信号)由 L3 Adapter
# 注入 Trigger(unity Lifecycle.cs / cocos lifecycle.ts 同构)。
#
#   Uninitialized → Initializing → Ready → Authenticating → Authenticated → PlayerReady
#                                    ▲                        │              │
#                                    │        ┌───────────────┘              │
#                                    │        ▼                              ▼
#                                    └── SignedOut                Suspended ⇄ Resuming

class_name CourierLifecycle


# --- 状态 ---

const UNINITIALIZED := "Uninitialized"
const INITIALIZING := "Initializing"
const READY := "Ready"
const AUTHENTICATING := "Authenticating"
const AUTHENTICATED := "Authenticated"
const PLAYER_READY := "PlayerReady"
const SUSPENDED := "Suspended"
const RESUMING := "Resuming"
const SIGNED_OUT := "SignedOut"


# --- 触发器(平台信号与业务动作的统一入口) ---

const INIT_STARTED := "InitStarted"
const INIT_COMPLETED := "InitCompleted"
const AUTH_STARTED := "AuthStarted"
const AUTH_SUCCEEDED := "AuthSucceeded"
const AUTH_FAILED := "AuthFailed"
const ENTER_PLAYER_READY := "EnterPlayerReady"
const SIGNED_OUT_TRIGGER := "SignedOut"
const SUSPENDED_TRIGGER := "Suspended"
const RESUME_STARTED := "ResumeStarted"
const RESUME_COMPLETED := "ResumeCompleted"


const STATES: Array = [UNINITIALIZED, INITIALIZING, READY, AUTHENTICATING, AUTHENTICATED,
		PLAYER_READY, SUSPENDED, RESUMING, SIGNED_OUT]


# 合法迁移表(architecture.md 状态图全表;表外一律非法)。
const TRANSITIONS: Dictionary = {
	UNINITIALIZED + ">" + INIT_STARTED: INITIALIZING,
	INITIALIZING + ">" + INIT_COMPLETED: READY,
	READY + ">" + AUTH_STARTED: AUTHENTICATING,
	AUTHENTICATING + ">" + AUTH_SUCCEEDED: AUTHENTICATED,
	AUTHENTICATING + ">" + AUTH_FAILED: READY,
	AUTHENTICATED + ">" + ENTER_PLAYER_READY: PLAYER_READY,
	AUTHENTICATED + ">" + SIGNED_OUT_TRIGGER: SIGNED_OUT,
	AUTHENTICATED + ">" + SUSPENDED_TRIGGER: SUSPENDED,
	PLAYER_READY + ">" + SIGNED_OUT_TRIGGER: SIGNED_OUT,
	PLAYER_READY + ">" + SUSPENDED_TRIGGER: SUSPENDED,
	SUSPENDED + ">" + RESUME_STARTED: RESUMING,
	# Resuming → ResumeCompleted 回挂起前稳定态(表外特判 _pre_suspend)。
	SIGNED_OUT + ">" + AUTH_STARTED: AUTHENTICATING,
}


# 契约生命周期事件(events.md「生命周期事件」之五;
# token_expired 由 SessionService 在自动 refresh 开始时发出)。
const EVENT_INITIALIZED := "lifecycle.initialized"
const EVENT_SIGNED_OUT := "lifecycle.signed_out"
const EVENT_SUSPENDED := "lifecycle.suspended"
const EVENT_RESUMED := "lifecycle.resumed"
const EVENT_ACCOUNT_SWITCHED := "lifecycle.account_switched"
const EVENT_TOKEN_EXPIRED := "lifecycle.token_expired"


var state: String = UNINITIALIZED
var pre_suspend: String = UNINITIALIZED  # Suspended 前的稳定态
var last_account_id: String = ""  # 切号检测(契约 events.md account_switched)
var listeners: Array[Callable] = []


# 订阅契约生命周期事件;返回退订 Callable。
func on_event(listener: Callable) -> Callable:
	listeners.append(listener)
	return func(): listeners.erase(listener)


# 推进状态机;成功返回新状态。非法转移 push_error 并返回 ""(严格表)。
func fire(trigger: String) -> String:
	if not try_fire(trigger):
		push_error("invalid lifecycle transition: %s --%s--> ?" % [state, trigger])
		return ""
	return state


# 非抛版:非法转移返回 false 且状态不变(Adapter 平台信号幂等用)。
func try_fire(trigger: String) -> bool:
	if state == RESUMING and trigger == RESUME_COMPLETED:
		_transition(trigger, pre_suspend)
		return true
	if not TRANSITIONS.has(state + ">" + trigger):
		return false
	_transition(trigger, TRANSITIONS[state + ">" + trigger])
	return true


func _transition(trigger: String, target: String) -> void:
	if target == SUSPENDED:
		pre_suspend = state
	var from := state
	state = target
	var etype := _event_of(trigger)
	if etype != "":
		_emit_event({"type": etype, "from": from, "to": target})


# 状态机触发 → 契约事件 type 映射;非契约触发返回 ""(不对外发)。
func _event_of(trigger: String) -> String:
	if trigger == INIT_COMPLETED:
		return EVENT_INITIALIZED  # Init 完成,进入 Ready
	if trigger == SIGNED_OUT_TRIGGER:
		return EVENT_SIGNED_OUT  # 登出/被踢(未认证)
	if trigger == SUSPENDED_TRIGGER:
		return EVENT_SUSPENDED  # 切后台/断网
	if trigger == RESUME_COMPLETED:
		return EVENT_RESUMED  # 恢复,重新可用
	return ""


# 切号检测:新账号与上次不同 → account_switched(SessionService 调用)。
# 首次登录不算切换;同号重登不算切换。
func report_account_id(account_id: String) -> bool:
	var switched := last_account_id != "" and account_id != "" and last_account_id != account_id
	if account_id != "":
		last_account_id = account_id
	if switched:
		_emit_event({"type": EVENT_ACCOUNT_SWITCHED, "from": state, "to": state})
	return switched


# access 过期,自动 refresh 开始(SessionService 调用);状态不变。
func raise_token_expired() -> void:
	_emit_event({"type": EVENT_TOKEN_EXPIRED, "from": state, "to": state})


func _emit_event(event: Dictionary) -> void:
	# 迭代副本:监听器内退订不影响本次广播。
	for listener in listeners.duplicate():
		if listener.is_valid():
			listener.call(event)
