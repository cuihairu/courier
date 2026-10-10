# L3 平台生命周期(Godot):前后台信号 → 状态机事件(layers.md L3「生命周期」),
# 与 Unity ApplicationLifecycleMonitor / cocos LifecycleMonitor 同构。
# 驱动两路合一:引擎通知(NOTIFICATION_APPLICATION_PAUSED/RESUMED 移动端前后台、
# FOCUS_OUT/IN 桌面焦点)+ 每帧可见性探测轮询(visibility_probe 注入;无注入时
# 缺省视为可见,以通知为准),统一经 _set_visible 幂等去重——移动端与桌面端信号
# 常成对触发,去重后才进状态机。实测入档:Godot 4.3 无
# DisplayServer.screen_is_visible、SceneTree 无 visibility_changed 信号,故不走信号。
# 转移被拒(如 Uninitialized 不可挂起)时不更新基线可见性,下一帧重试:
# 隐藏启动、状态机稍后才就绪的场景不丢挂起。
# 契约 messages.md 语义规则 4:切后台可主动断流省电、回前台重连,由
# on_suspended / on_resumed 回调交给通道管理方处理(本类不持通道)。
class_name CourierLifecycleMonitor
extends Node

var lifecycle: CourierLifecycle = null  # bind() 注入;未绑定全 no-op
var on_suspended: Callable = Callable()  # () -> void 后台:通道可断流
var on_resumed: Callable = Callable()    # () -> void 前台:通道可重连
var visibility_probe: Callable = Callable()  # () -> bool 可见性探测(注入即启用轮询)

var _suspended: bool = false
var _last_visible: bool = true


func bind(machine: CourierLifecycle) -> void:
	lifecycle = machine


func _notification(what: int) -> void:
	match what:
		Node.NOTIFICATION_APPLICATION_PAUSED, Node.NOTIFICATION_APPLICATION_FOCUS_OUT:
			_set_visible(false)
		Node.NOTIFICATION_APPLICATION_RESUMED, Node.NOTIFICATION_APPLICATION_FOCUS_IN:
			_set_visible(true)


func _process(_delta: float) -> void:
	_set_visible(_visible())


func _visible() -> bool:
	if visibility_probe.is_valid():
		return bool(visibility_probe.call())
	return true  # 无注入探测:以引擎通知为准,不臆断可见性


func _set_visible(visible: bool) -> void:
	if visible == _last_visible:
		return
	var applied := _resume() if visible else _suspend()
	if applied:
		_last_visible = visible  # 被拒不更新基线:下帧重试,不丢挂起


func _suspend() -> bool:
	if _suspended or lifecycle == null:
		return false
	if not lifecycle.try_fire(CourierLifecycle.SUSPENDED_TRIGGER):
		return false  # 非法转移:不动状态、不发回调
	_suspended = true
	if on_suspended.is_valid():
		on_suspended.call()
	return true


func _resume() -> bool:
	if not _suspended or lifecycle == null:
		return false
	if not (lifecycle.try_fire(CourierLifecycle.RESUME_STARTED) and lifecycle.try_fire(CourierLifecycle.RESUME_COMPLETED)):
		return false
	_suspended = false
	if on_resumed.is_valid():
		on_resumed.call()
	return true
