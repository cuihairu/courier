# L2 Core 门面(unity CourierClient 同构):装配生命周期状态机 + ApiClient + 身份/会话。
# 依赖方向 service→core;core 零平台引用(平台差异收敛在 Transport/TokenStore)。
# GDScript 无异常:配置校验失败 push_error 且 valid=false;auth_flow 守卫失败返回
# {ok:false, message:...}(TS 为 throw;错误对象一律 Dictionary 惯用法)。

class_name CourierClient

var config: Dictionary
var lifecycle: CourierLifecycle
var api: CourierApiClient
var identity: CourierIdentityService
var session: CourierSessionService
var valid: bool = false


func _init(options: Dictionary):
	var cfg: Dictionary = options.get("config", {})
	if str(cfg.get("gameId", "")).strip_edges() == "" or str(cfg.get("env", "")).strip_edges() == "":
		push_error("courier: gameId/env 必填(scope 头,契约 scope.md)")
		return
	var t: Variant = options.get("transport", null)
	if t == null:
		push_error("courier: transport 必填(平台实现)")
		return
	valid = true
	config = cfg
	lifecycle = CourierLifecycle.new()
	var store: CourierTokenStore = options.get("tokenStore", CourierTokenStore.MemoryTokenStore.new())
	# 两段装配:session 先建(api 的 token_provider/refresh_hook 回调它),
	# api 建成后回填——环只能延迟接线。
	session = CourierSessionService.new(store, lifecycle)
	var retry: Dictionary = options.get("retry", {})
	api = CourierApiClient.new()
	api.endpoint = str(cfg.get("endpoint", ""))
	api.game_id = str(cfg.get("gameId", ""))
	api.env_str = str(cfg.get("env", ""))
	api.transport = t
	api.retry_max_attempts = int(retry.get("maxAttempts", CourierApiClient.DEFAULT_RETRY_MAX_ATTEMPTS))
	api.retry_base_delay_ms = int(retry.get("baseDelayMs", CourierApiClient.DEFAULT_RETRY_BASE_DELAY_MS))
	api.sleep_f = options.get("sleep", Callable())
	api.token_provider = Callable(session, "access_token")
	api.refresh_hook = Callable(self, "_refresh_for_replay")
	session.attach_api(api)
	identity = CourierIdentityService.new(api, Callable(session, "adopt"))


## 换发钩子:委托 session.refresh();成功(新会话落库)返回 true 让 ApiClient 重放。
func _refresh_for_replay() -> bool:
	var r: Dictionary = await session.refresh()
	return bool(r.get("ok", false))


## 初始化(Uninitialized→Initializing→Ready):校验配置后推进状态机。
func init() -> void:
	if not valid:
		return
	lifecycle.fire(CourierLifecycle.INIT_STARTED)
	lifecycle.fire(CourierLifecycle.INIT_COMPLETED)


## 游客登录完整流(Ready/SignedOut → Authenticating → PlayerReady)。
func guest_login(device_id: String, platform: String = "") -> Dictionary:
	return await auth_flow(func(): return await identity.guest(device_id, platform))


## 邮箱+密码登录完整流。
func login(email: String, password: String) -> Dictionary:
	return await auth_flow(func(): return await identity.login(email, password))


## 邮箱+密码注册并登录完整流。
func register(email: String, password: String) -> Dictionary:
	return await auth_flow(func(): return await identity.register(email, password))


## 认证状态流(unity LoginAsync 同构):守卫 Ready/SignedOut 才可发起;
## AuthFailed 只在仍处 Authenticating 时回退(成功后失败不存在)。
func auth_flow(send_fn: Callable) -> Dictionary:
	if not valid:
		return {"ok": false, "message": "courier: client 未通过配置校验"}
	if lifecycle.state != CourierLifecycle.READY and lifecycle.state != CourierLifecycle.SIGNED_OUT:
		return {"ok": false, "message": "courier: login requires state Ready/SignedOut, current: " + lifecycle.state}
	lifecycle.fire(CourierLifecycle.AUTH_STARTED)
	var r: Dictionary = await send_fn.call()
	if not r.get("ok", false) and lifecycle.state == CourierLifecycle.AUTHENTICATING:
		lifecycle.fire(CourierLifecycle.AUTH_FAILED)
		return r
	if r.get("ok", false):
		lifecycle.fire(CourierLifecycle.AUTH_SUCCEEDED)
		lifecycle.fire(CourierLifecycle.ENTER_PLAYER_READY)
	return r
