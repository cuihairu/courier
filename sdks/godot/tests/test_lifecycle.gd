# 生命周期状态机全事件单测(cocos lifecycle.test.ts 同构):全合法迁移表 +
# 非法迁移拒绝 + 契约事件面 + 挂起恢复回跳 + 切号检测。
# 运行(装 Godot 4 后,从仓库根):
#   godot --headless --path sdks/godot --script tests/test_lifecycle.gd
extends SceneTree

var failed: int = 0


class Recorder:
	var events: Array = []
	var off: Callable

	func attach(machine: CourierLifecycle) -> void:
		off = machine.on_event(_on_event)

	func _on_event(event: Dictionary) -> void:
		events.append(event)

	func types() -> Array:
		return events.map(func(e): return e["type"])


func check(cond: bool, msg: String) -> void:
	if not cond:
		print("FAIL: " + msg)
		failed += 1


func recorder(machine: CourierLifecycle) -> Recorder:
	var r := Recorder.new()
	r.attach(machine)
	return r


# 构造到指定状态(cocos machineAt 同构,合法快路径)。
func machine_at(state_name: String) -> CourierLifecycle:
	var m := CourierLifecycle.new()
	if state_name == "Uninitialized":
		return m
	m.fire(CourierLifecycle.INIT_STARTED)
	if state_name == "Initializing":
		return m
	m.fire(CourierLifecycle.INIT_COMPLETED)
	if state_name == "Ready":
		return m
	m.fire(CourierLifecycle.AUTH_STARTED)
	if state_name == "Authenticating":
		return m
	m.fire(CourierLifecycle.AUTH_SUCCEEDED)
	if state_name == "Authenticated":
		return m
	m.fire(CourierLifecycle.ENTER_PLAYER_READY)
	if state_name == "PlayerReady":
		return m
	if state_name == "SignedOut":
		m.fire(CourierLifecycle.SIGNED_OUT_TRIGGER)  # 直接从 PlayerReady 登出
		return m
	m.fire(CourierLifecycle.SUSPENDED_TRIGGER)
	if state_name == "Suspended":
		return m
	m.fire(CourierLifecycle.RESUME_STARTED)
	if state_name == "Resuming":
		return m
	check(false, "未知构造态: " + state_name)
	return m


func _init() -> void:
	var full_table: Array = [
		["Uninitialized", CourierLifecycle.INIT_STARTED, "Initializing"],
		["Initializing", CourierLifecycle.INIT_COMPLETED, "Ready"],
		["Ready", CourierLifecycle.AUTH_STARTED, "Authenticating"],
		["Authenticating", CourierLifecycle.AUTH_SUCCEEDED, "Authenticated"],
		["Authenticating", CourierLifecycle.AUTH_FAILED, "Ready"],
		["Authenticated", CourierLifecycle.ENTER_PLAYER_READY, "PlayerReady"],
		["Authenticated", CourierLifecycle.SIGNED_OUT_TRIGGER, "SignedOut"],
		["Authenticated", CourierLifecycle.SUSPENDED_TRIGGER, "Suspended"],
		["PlayerReady", CourierLifecycle.SIGNED_OUT_TRIGGER, "SignedOut"],
		["PlayerReady", CourierLifecycle.SUSPENDED_TRIGGER, "Suspended"],
		["Suspended", CourierLifecycle.RESUME_STARTED, "Resuming"],
		["Resuming", CourierLifecycle.RESUME_COMPLETED, "PlayerReady"],  # 挂起于 PlayerReady
		["SignedOut", CourierLifecycle.AUTH_STARTED, "Authenticating"],
	]

	# ---- 全合法迁移表(architecture.md 状态图) ----
	for row in full_table:
		var m := machine_at(row[0])
		var new_state := m.fire(row[1])
		check(new_state == row[2] and m.state == row[2],
				"迁移:%s --%s--> %s(实际 %s)" % [row[0], row[1], row[2], m.state])

	# 迁移表与状态图逐条对齐(表外无额外迁移)。
	check(CourierLifecycle.TRANSITIONS.size() + 1 == full_table.size(),
			"迁移表条目数 = %d(状态图 %d,含 Resuming 特判)" % [
				CourierLifecycle.TRANSITIONS.size(), full_table.size()])

	# 状态集与契约一致。
	check(CourierLifecycle.STATES.size() == 9, "状态集应有 9 态(实际 %d)" % CourierLifecycle.STATES.size())

	# ---- 恢复:回到挂起前状态 ----
	var resume := machine_at("Authenticated")
	resume.fire(CourierLifecycle.SUSPENDED_TRIGGER)
	resume.fire(CourierLifecycle.RESUME_STARTED)
	resume.fire(CourierLifecycle.RESUME_COMPLETED)
	check(resume.state == "Authenticated", "恢复应回到挂起前状态(实际 %s)" % resume.state)

	# ---- 非法迁移拒绝(状态不被非法触发破坏) ----
	var invalid_table: Array = [
		["Ready", CourierLifecycle.SUSPENDED_TRIGGER],
		["Uninitialized", CourierLifecycle.AUTH_STARTED],
		["Uninitialized", CourierLifecycle.INIT_COMPLETED],
		["Ready", CourierLifecycle.ENTER_PLAYER_READY],
		["PlayerReady", CourierLifecycle.AUTH_STARTED],
		["PlayerReady", CourierLifecycle.INIT_COMPLETED],
		["SignedOut", CourierLifecycle.SUSPENDED_TRIGGER],
		["Authenticating", CourierLifecycle.SIGNED_OUT_TRIGGER],
	]
	for row in invalid_table:
		var m := machine_at(row[0])
		var r := recorder(m)
		var new_state := m.fire(row[1])
		check(new_state == "" and m.state == row[0] and r.events.is_empty(),
				"非法迁移:%s --%s--> 应拒绝且状态/事件不变(实际 %s, %s)" % [
					row[0], row[1], m.state, str(r.events)])

	# ---- 契约事件面 ----
	var ev := CourierLifecycle.new()
	var rec := recorder(ev)
	ev.fire(CourierLifecycle.INIT_STARTED)
	ev.fire(CourierLifecycle.INIT_COMPLETED)
	ev.fire(CourierLifecycle.AUTH_STARTED)
	ev.fire(CourierLifecycle.AUTH_SUCCEEDED)
	ev.fire(CourierLifecycle.ENTER_PLAYER_READY)
	ev.fire(CourierLifecycle.SUSPENDED_TRIGGER)
	ev.fire(CourierLifecycle.RESUME_STARTED)
	ev.fire(CourierLifecycle.RESUME_COMPLETED)
	ev.fire(CourierLifecycle.SIGNED_OUT_TRIGGER)
	check(rec.types() == ["lifecycle.initialized", "lifecycle.suspended", "lifecycle.resumed",
			"lifecycle.signed_out"], "契约事件面应为 initialized/suspended/resumed/signed_out(实际 %s)" % str(rec.types()))
	check(rec.events[1]["from"] == "PlayerReady" and rec.events[1]["to"] == "Suspended",
			"suspended 事件 from/to 应正确")

	# token_expired:事件发出但状态不变。
	var te := machine_at("PlayerReady")
	var tre := recorder(te)
	te.raise_token_expired()
	check(tre.types() == ["lifecycle.token_expired"] and te.state == "PlayerReady",
			"token_expired 应发事件且状态不变")

	# account_switched:首登不发、切号发、同号重登不发。
	var sw := CourierLifecycle.new()
	var swr := recorder(sw)
	check(not sw.report_account_id("acc_A"), "首次登录不应算切换")
	check(swr.types() == [], "首登不应发 account_switched")
	check(sw.report_account_id("acc_B"), "切号应算切换")
	check(swr.types() == ["lifecycle.account_switched"], "切号应发 account_switched")
	check(not sw.report_account_id("acc_B"), "同号重登不应算切换")

	# onEvent 退订后不再接收。
	var un := CourierLifecycle.new()
	var seen: Array = []
	var off := un.on_event(func(e): seen.append(e["type"]))
	off.call()
	var nested := Recorder.new()
	un.on_event(nested._on_event)
	un.fire(CourierLifecycle.INIT_STARTED)
	un.fire(CourierLifecycle.INIT_COMPLETED)
	check(seen.is_empty() and nested.types() == ["lifecycle.initialized"],
			"退订后不应再接收(实际 %s)" % str(seen))

	# tryFire:非法迁移返回 false、状态不变、不发事件。
	var tf := machine_at("PlayerReady")
	var tfr := recorder(tf)
	check(not tf.try_fire(CourierLifecycle.AUTH_STARTED), "tryFire 非法应返回 false")
	check(not tf.try_fire(CourierLifecycle.RESUME_STARTED), "未挂起 tryFire 恢复应返回 false")
	check(tf.state == "PlayerReady" and tfr.events.is_empty(),
			"tryFire 非法不应改状态/发事件")

	print("godot lifecycle tests: %s (%d failed)" % ["all green" if failed == 0 else "FAILED", failed])
	quit(1 if failed > 0 else 0)
