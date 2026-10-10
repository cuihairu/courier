# L3 生命周期适配单测:CourierLifecycleMonitor 前后台信号 → 状态机事件
# (可见性注入驱动,幂等去重、恢复挂起前状态、隐藏启动留帧重试、未绑定 no-op)。
# 运行(从仓库根):
#   godot --headless --path sdks/godot --script tests/test_adapter.gd
extends SceneTree

var failed: int = 0
var _machine: CourierLifecycle
var _monitor: CourierLifecycleMonitor
var _visible: bool = true
var _suspend_count: int = 0
var _resume_count: int = 0


func check(cond: bool, msg: String) -> void:
	if not cond:
		print("FAIL: " + msg)
		failed += 1


func _probe() -> bool:
	return _visible


func _on_suspend() -> void:
	_suspend_count += 1


func _on_resume() -> void:
	_resume_count += 1


func new_machine(state: String) -> CourierLifecycle:
	var m := CourierLifecycle.new()
	if state == "Uninitialized":
		return m
	m.fire(CourierLifecycle.INIT_STARTED)
	m.fire(CourierLifecycle.INIT_COMPLETED)
	if state == "PlayerReady":
		m.fire(CourierLifecycle.AUTH_STARTED)
		m.fire(CourierLifecycle.AUTH_SUCCEEDED)
		m.fire(CourierLifecycle.ENTER_PLAYER_READY)
	return m


func attach(machine: CourierLifecycle) -> CourierLifecycleMonitor:
	_machine = machine
	_suspend_count = 0
	_resume_count = 0
	var mon := CourierLifecycleMonitor.new()
	mon.visibility_probe = Callable(self, "_probe")
	mon.on_suspended = Callable(self, "_on_suspend")
	mon.on_resumed = Callable(self, "_on_resume")
	mon.bind(machine)
	_monitor = mon
	root.add_child(mon)
	return mon


func settle() -> void:
	for i in range(3):
		await process_frame


## 摘除用例 monitor:先停处理再释放,避免跨用例的残留 monitor 污染计数。
func detach(mon: CourierLifecycleMonitor) -> void:
	mon.set_process(false)
	mon.queue_free()
	_monitor = null
	_visible = true
	_suspend_count = 0
	_resume_count = 0


# ---- 后台/前台:挂起与恢复回到挂起前状态 ----

func t_suspend_resume_player_ready() -> void:
	var m := new_machine("PlayerReady")
	attach(m)
	await settle()
	check(m.state == CourierLifecycle.PLAYER_READY, "装配后应停在 PlayerReady")
	_visible = false
	await settle()
	check(m.state == CourierLifecycle.SUSPENDED, "切后台应挂起(实得 %s)" % m.state)
	check(_suspend_count == 1 and _resume_count == 0, "应仅发一次后台回调")
	_visible = true
	await settle()
	check(m.state == CourierLifecycle.PLAYER_READY, "回前台应恢复到挂起前(实得 %s)" % m.state)
	check(_resume_count == 1, "应仅发一次前台回调(实得 %d)" % _resume_count)
	detach(_monitor)


# ---- 恢复到挂起前的深层状态 ----

func t_restore_player_ready() -> void:
	var m := new_machine("PlayerReady")
	attach(m)
	await settle()
	check(m.state == CourierLifecycle.PLAYER_READY, "前置应停在 PlayerReady")
	_visible = false
	await settle()
	_visible = true
	await settle()
	check(m.state == CourierLifecycle.PLAYER_READY, "PlayerReady 挂起后应原样恢复(实得 %s)" % m.state)
	detach(_monitor)


# ---- 幂等:重复信号不重复触发 ----

func t_idempotent() -> void:
	var m := new_machine("PlayerReady")
	attach(m)
	await settle()
	_visible = false
	await settle()
	for i in range(5):
		await process_frame
	check(_suspend_count == 1, "连续后台信号应去重(实得 %d)" % _suspend_count)
	check(m.state == CourierLifecycle.SUSPENDED, "去重后仍为挂起")
	detach(_monitor)


# ---- 未绑定:全 no-op 不崩 ----

func t_unbound_noop() -> void:
	_suspend_count = 0
	_resume_count = 0
	_monitor = CourierLifecycleMonitor.new()
	_monitor.visibility_probe = Callable(self, "_probe")
	_monitor.on_suspended = Callable(self, "_on_suspend")
	_monitor.on_resumed = Callable(self, "_on_resume")
	root.add_child(_monitor)
	_visible = false
	await settle()
	_visible = true
	await settle()
	check(_suspend_count == 0 and _resume_count == 0, "未绑定应静默 no-op")
	detach(_monitor)


# ---- 隐藏启动:转移被拒时留帧重试,状态机就绪后补挂起 ----

func t_hidden_start_retry() -> void:
	var m := new_machine("Uninitialized")
	attach(m)
	_visible = false
	await settle()
	check(m.state == CourierLifecycle.UNINITIALIZED, "非法转移不应改变状态")
	check(_suspend_count == 0, "非法转移不应发回调")
	m.fire(CourierLifecycle.INIT_STARTED)
	m.fire(CourierLifecycle.INIT_COMPLETED)
	m.fire(CourierLifecycle.AUTH_STARTED)
	m.fire(CourierLifecycle.AUTH_SUCCEEDED)
	m.fire(CourierLifecycle.ENTER_PLAYER_READY)
	await settle()
	check(m.state == CourierLifecycle.SUSPENDED, "就绪后应补挂起(实得 %s)" % m.state)
	check(_suspend_count == 1, "补挂起应发一次回调")
	detach(_monitor)


# ---- 引擎通知路径:NOTIFICATION_APPLICATION_* → 状态机 ----

func t_notification_path() -> void:
	var m := new_machine("PlayerReady")
	attach(m)
	_visible = true
	await settle()
	# 通知与探测同变(真实环境一致;探测不一致会与之竞争)
	_visible = false
	_monitor.notification(Node.NOTIFICATION_APPLICATION_PAUSED)
	await settle()
	check(m.state == CourierLifecycle.SUSPENDED, "PAUSED 通知应挂起(实得 %s)" % m.state)
	_visible = true
	_monitor.notification(Node.NOTIFICATION_APPLICATION_RESUMED)
	await settle()
	check(m.state == CourierLifecycle.PLAYER_READY, "RESUMED 通知应恢复(实得 %s)" % m.state)
	_visible = false
	_monitor.notification(Node.NOTIFICATION_APPLICATION_FOCUS_OUT)
	await settle()
	check(m.state == CourierLifecycle.SUSPENDED, "FOCUS_OUT 通知应挂起(实得 %s)" % m.state)
	_visible = true
	_monitor.notification(Node.NOTIFICATION_APPLICATION_FOCUS_IN)
	await settle()
	check(m.state == CourierLifecycle.PLAYER_READY, "FOCUS_IN 通知应恢复(实得 %s)" % m.state)
	detach(_monitor)


# ---- 缺省探测:未注入时视为可见,不臆断 ----

func t_default_probe() -> void:
	_monitor = CourierLifecycleMonitor.new()
	root.add_child(_monitor)
	check(typeof(_monitor._visible()) == TYPE_BOOL, "缺省探测应返回 bool")
	detach(_monitor)


func _init() -> void:
	await t_suspend_resume_player_ready()
	await t_restore_player_ready()
	await t_idempotent()
	await t_unbound_noop()
	await t_hidden_start_retry()
	await t_notification_path()
	await t_default_probe()
	print("godot adapter tests: %s (%d failed)" % ["all green" if failed == 0 else "FAILED", failed])
	quit(1 if failed > 0 else 0)
