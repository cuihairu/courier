# L2 Core:API 客户端(cocos apiClient.ts / ue api_client.hpp 同构)——拼 scope 头、
# 发请求、解析信封、按 retryable 退避重试、AUTH_TOKEN_EXPIRED 时经 refresh_hook
# 换发后重放一次(自动 refresh,契约 auth.md「access 过期」)。
# 协程(async):调用方必须 await;返回 {ok:true, data} 或 {ok:false, error}。

class_name CourierApiClient

const DEFAULT_TIMEOUT_MS := 10000
const DEFAULT_RETRY_MAX_ATTEMPTS := 3
const DEFAULT_RETRY_BASE_DELAY_MS := 300
const HEADER_GAME_ID := "X-Courier-Game-Id"
const HEADER_ENV := "X-Courier-Env"
const HEADER_AUTH := "Authorization"
const HEADER_CT := "Content-Type"
const HEADER_RA := "retry-after"

var endpoint: String = ""
var game_id: String = ""
var env_str: String = ""  # 避免与内置 env 冲突
var transport: CourierTransport
var retry_max_attempts: int = DEFAULT_RETRY_MAX_ATTEMPTS
var retry_base_delay_ms: int = DEFAULT_RETRY_BASE_DELAY_MS
var token_provider: Callable = Callable()  # () -> String
var refresh_hook: Callable = Callable()    # () -> bool(协程;成功拿到新会话才重放)
var sleep_f: Callable = Callable()          # (ms) -> void


## 协程请求;返回 {ok:true, data} 或 {ok:false, error}。
func request(method: String, path: String, body: Variant = null, with_auth: bool = true) -> Dictionary:
	var headers := {HEADER_GAME_ID: game_id, HEADER_ENV: env_str}
	var token := _access_token() if with_auth else ""
	if with_auth and token == "":
		return {"ok": false, "error": CourierApiError.make("COMMON_UNAUTHENTICATED", "not authenticated", 401, false)}
	if body != null:
		headers[HEADER_CT] = "application/json"
	var refresh_used := false
	var attempt := 1
	while true:
		if token != "":
			headers[HEADER_AUTH] = "Bearer " + token
		var req := {"method": method, "url": _url(path), "headers": headers}
		if body != null:
			req["jsonBody"] = JSON.stringify(body)
		var tp: Dictionary = await transport.send(req)
		var parsed: Dictionary = _parse(tp)
		if not parsed.has("error"):
			return {"ok": true, "data": parsed.get("data", {})}
		var err: Dictionary = parsed["error"]

		# access 过期:refresh 一次后重放(不计入退避重试次数);refresh 失败放弃,
		# 返回原 401(与 TS 同构:refreshHook 返回 boolean)。
		if CourierApiError.wire(err) == "AUTH_TOKEN_EXPIRED" and refresh_hook.is_valid() and not refresh_used:
			refresh_used = true
			if await refresh_hook.call():
				token = _access_token()
				attempt -= 1
				continue
			return {"ok": false, "error": err}

		if CourierApiError.retryable(err) and attempt < retry_max_attempts:
			await _sleep(_retry_delay_ms(err))
			attempt += 1
			continue
		return {"ok": false, "error": err}
	# unreachable: 循环内所有路径均 return/continue(GDScript 返回路径分析保守,须兜底)。
	return {"ok": false, "error": CourierApiError.make("COMMON_INTERNAL", "unreachable", 500, true)}


func _access_token() -> String:
	if not token_provider.is_valid(): return ""
	return str(token_provider.call())


func _url(p: String) -> String:
	var e := endpoint
	while e.ends_with("/"):
		e = e.substr(0, e.length() - 1)
	return e + p


func _retry_delay_ms(e: Dictionary) -> int:
	var ra := CourierApiError.retry_after_ms(e)
	return ra if ra >= 0 else retry_base_delay_ms


func _retry_after_ms(headers: Dictionary) -> int:
	var raw := str(headers.get(HEADER_RA, "")).strip_edges()
	if not raw.is_valid_int(): return -1
	var s := int(raw)
	return s * 1000 if s >= 0 else -1


func _fallback_wire(status_code: int) -> Dictionary:
	match status_code:
		401: return {"wire": "COMMON_UNAUTHENTICATED", "retryable": false}
		403: return {"wire": "COMMON_PERMISSION_DENIED", "retryable": false}
		404: return {"wire": "COMMON_NOT_FOUND", "retryable": false}
		429: return {"wire": "RATE_LIMITED", "retryable": true}
		501: return {"wire": "COMMON_CAPABILITY_DISABLED", "retryable": false}
		_: pass
	if status_code >= 500: return {"wire": "COMMON_UNAVAILABLE", "retryable": true}
	return {"wire": "COMMON_INTERNAL", "retryable": status_code >= 500 or status_code == 429}


func _parse(tp: Dictionary) -> Dictionary:
	var sc := int(tp.get("status_code", 500))
	var body := str(tp.get("body", "")).strip_edges()
	if body == "":
		var fb := _fallback_wire(sc)
		return {"error": CourierApiError.make(fb["wire"], "empty response (HTTP %d)" % sc, sc, fb["retryable"])}
	var parsed: Variant = JSON.parse_string(body)
	if typeof(parsed) != TYPE_DICTIONARY:
		var fb := _fallback_wire(sc)
		return {"error": CourierApiError.make(fb["wire"], "non-JSON (HTTP %d)" % sc, sc, fb["retryable"])}
	if not parsed.has("error"):
		return {"data": parsed.get("data", {})}
	var e: Dictionary = parsed["error"]
	var wire := str(e.get("code", ""))
	var spec: Dictionary = CourierErrorCode.spec_of(wire)
	var retryable: bool = bool(e["retryable"]) if e.has("retryable") else CourierApiError.retryable(spec)
	var er := CourierApiError.make(wire, str(e.get("message", "")), sc, retryable)
	er["traceId"] = str(parsed.get("traceId", ""))
	er["retryAfterMs"] = _retry_after_ms(tp.get("headers", {}))
	return {"error": er}


func _sleep(ms: int) -> void:
	if sleep_f.is_valid():
		sleep_f.call(ms)
		return
	var ml: Variant = Engine.get_main_loop()
	if ml == null:
		OS.delay_msec(ms)  # 无主循环(纯脚本环境):阻塞兜底
		return
	var st := Time.get_ticks_msec()
	while Time.get_ticks_msec() - st < ms:
		await ml.process_frame
