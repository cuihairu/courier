# L3 平台流式传输(Godot HTTPClient):契约 messages.md 的 GET /v1/messages/stream
# SSE 长连接读面。poll 增量读体,行缓冲跨 chunk 喂 CourierSseParser,完整帧经
# on_event 上抛。客户端要求全内建:501 → capability_disabled(调用方转纯拉取)、
# 401 → unauthenticated,两者收尾不重试;断线指数退避重连(1s 起、上限 60s,
# Retry-After 秒优先);收过事件的连接断开后退避重置(健康连不等同失败连);
# stop 回调每圈探查,置真即净停。Authorization 每次尝试按 token_provider 现取
# (契约:新连接按当前 token 校验)。
# 分框硬约束(实测):HTTPClient 不支持无 Content-Length 的 close 分隔响应体
# ——收完头即判响应完成,永不进 STATUS_BODY,体数据不可读。SSE 流响应必须带
# Transfer-Encoding: chunked;自建网关(Go net/http 在 Content-Length 缺省时
# 自动补 chunked)天然满足,直连其他无分框的 SSE 端点不可用。

class_name CourierSseStream
extends RefCounted

const BACKOFF_BASE_MS := 1000
const BACKOFF_MAX_MS := 60000

var frame_f: Callable = Callable()  # () -> void 协程:让出一帧(--script 测试注入)
var sleep_f: Callable = Callable()  # (ms:int) -> void 协程:退避等待(缺省主循环 Timer)
var backoff_base_ms: int = BACKOFF_BASE_MS
var backoff_max_ms: int = BACKOFF_MAX_MS
var connect_timeout_ms: int = 15000


## 长连协程:直到 stop 为真或终态(501/401)收尾。调用方必须 await 或点火即弃。
## headers = 基础头(scope 等,不含 Authorization);on_event 收解析帧
## {type, data[, id]};on_closed 收终态 reason。
func stream(url: String, headers: Dictionary, token_provider: Callable, on_event: Callable, on_closed: Callable, stop: Callable) -> void:
	var attempt := 0
	while not bool(stop.call()):
		var r: Dictionary = await _read_once(url, headers, token_provider, on_event, stop)
		var reason := str(r.get("reason", ""))
		if reason == "stopped":
			return
		if reason == "capability_disabled" or reason == "unauthenticated":
			on_closed.call(reason)
			return
		if bool(r.get("healthy", false)):
			attempt = 0  # 健康连断开:退避重置,立即重连
		else:
			attempt += 1
		await _sleep(_backoff_ms(attempt, int(r.get("retry_after_ms", 0))))


# 单次连接:建连 → 校验响应码 → 流读。返回 {reason[, retry_after_ms][, healthy]}。
func _read_once(url: String, headers: Dictionary, token_provider: Callable, on_event: Callable, stop: Callable) -> Dictionary:
	var u: Dictionary = _split_url(url)
	if str(u["host"]) == "":
		return {"reason": "error", "message": "courier: url 无效:" + url}
	var hc := HTTPClient.new()
	var err: int
	if bool(u["tls"]):
		err = hc.connect_to_host(str(u["host"]), int(u["port"]), TLSOptions.client())
	else:
		err = hc.connect_to_host(str(u["host"]), int(u["port"]))
	if err != OK:
		return {"reason": "error"}
	var deadline := Time.get_ticks_msec() + connect_timeout_ms
	while hc.get_status() == HTTPClient.STATUS_RESOLVING or hc.get_status() == HTTPClient.STATUS_CONNECTING:
		if bool(stop.call()):
			return {"reason": "stopped"}
		if Time.get_ticks_msec() > deadline:
			return {"reason": "error"}
		await _frame()
		hc.poll()
	if hc.get_status() != HTTPClient.STATUS_CONNECTED:
		return {"reason": "error"}

	var hs := PackedStringArray()
	for k in headers:
		hs.append(str(k) + ": " + str(headers[k]))
	if token_provider.is_valid():
		var tok := str(token_provider.call())
		if tok != "":
			hs.append("Authorization: Bearer " + tok)
	err = hc.request(HTTPClient.METHOD_GET, str(u["path"]), hs, "")
	if err != OK:
		return {"reason": "error"}

	# 等响应头(以 has_response 判定——request 后首个 poll 仍可报 CONNECTED;
	# 响应头收完即被引擎清空,此处捕获快照)。
	deadline = Time.get_ticks_msec() + connect_timeout_ms
	var resp_headers := {}
	while true:
		hc.poll()
		var st := hc.get_status()
		if resp_headers.is_empty() and hc.has_response():
			resp_headers = _response_headers(hc)
		if not resp_headers.is_empty() and (st == HTTPClient.STATUS_CONNECTED or st == HTTPClient.STATUS_BODY):
			break
		if st == HTTPClient.STATUS_CONNECTION_ERROR or st == HTTPClient.STATUS_DISCONNECTED:
			return {"reason": "disconnected"}
		if Time.get_ticks_msec() > deadline:
			return {"reason": "error"}
		await _frame()

	var code := hc.get_response_code()
	if code == 501:
		return {"reason": "capability_disabled"}  # 通道关:SDK 转纯拉取,不重试
	if code == 401:
		return {"reason": "unauthenticated"}
	if code != 200:
		var ra := str(resp_headers.get("retry-after", ""))
		var r := {"reason": "error", "status_code": code}
		if ra.is_valid_int():
			r["retry_after_ms"] = int(ra) * 1000  # Retry-After 头优先(契约语义规则 3)
		return r

	# 流读:行缓冲(跨 chunk 的行不完整即留缓冲),空行结算帧,注释行解析器自弃。
	var parser := CourierSseParser.new()
	var buf := ""
	var healthy := false
	var saw_body := false
	while true:
		if bool(stop.call()):
			return {"reason": "stopped", "healthy": healthy}
		hc.poll()
		var st := hc.get_status()
		if st == HTTPClient.STATUS_BODY:
			saw_body = true
			var chunk := hc.read_response_body_chunk()
			if chunk.size() > 0:
				buf += chunk.get_string_from_utf8()
				while true:
					var nl := buf.find("\n")
					if nl < 0:
						break
					var evt: Variant = parser.feed_line(buf.substr(0, nl))
					buf = buf.substr(nl + 1)
					if evt != null:
						healthy = true
						on_event.call(evt)
			# 有 Content-Length 的流响应收完(罕见)即断流重连
			if hc.get_status() == HTTPClient.STATUS_CONNECTED:
				return {"reason": "disconnected", "healthy": healthy}
		elif st == HTTPClient.STATUS_CONNECTED:
			if saw_body:
				return {"reason": "disconnected", "healthy": healthy}
			# 否则头已收、体未到:长连静默期,继续等
		elif st == HTTPClient.STATUS_CONNECTION_ERROR or st == HTTPClient.STATUS_DISCONNECTED:
			return {"reason": "disconnected", "healthy": healthy}
		await _frame()
	return {"reason": "disconnected", "healthy": healthy}


# 退避:base * 2^(attempt-1) 封顶;Retry-After(ms)优先于算得值。
func _backoff_ms(attempt: int, retry_after_ms: int) -> int:
	if retry_after_ms > 0:
		return retry_after_ms
	var d := backoff_base_ms
	for i in range(attempt - 1):
		d = mini(d * 2, backoff_max_ms)
		if d >= backoff_max_ms:
			break
	return clampi(d, backoff_base_ms, backoff_max_ms)


# 让出一帧(协程):注入的 frame_f 优先;缺省取 Engine 主循环。
func _frame() -> void:
	if frame_f.is_valid():
		await frame_f.call()
		return
	var ml: Variant = Engine.get_main_loop()
	if ml is SceneTree:
		await (ml as SceneTree).process_frame


# 退避等待(协程):注入的 sleep_f 优先;缺省主循环 Timer。
func _sleep(ms: int) -> void:
	if ms <= 0:
		return
	if sleep_f.is_valid():
		await sleep_f.call(ms)
		return
	var ml: Variant = Engine.get_main_loop()
	if ml is SceneTree:
		await (ml as SceneTree).create_timer(ms / 1000.0).timeout


# 拆 url → {host, port, path, tls}(缺省端口随 scheme)。
func _split_url(url: String) -> Dictionary:
	var rest := url
	var tls := false
	if rest.begins_with("https://"):
		tls = true
		rest = rest.substr(8)
	elif rest.begins_with("http://"):
		rest = rest.substr(7)
	var slash := rest.find("/")
	var hostport := rest if slash < 0 else rest.substr(0, slash)
	var path := "/" if slash < 0 else rest.substr(slash)
	var host := hostport
	var port := 443 if tls else 80
	var colon := hostport.rfind(":")
	if colon > 0:
		host = hostport.substr(0, colon)
		var p := int(hostport.substr(colon + 1))
		if p > 0:
			port = p
	return {"host": host, "port": port, "path": path, "tls": tls}


# 响应头 → Dictionary(键小写归一,契约 transport.gd)。
func _response_headers(hc: HTTPClient) -> Dictionary:
	var out := {}
	for line in hc.get_response_headers():
		var colon := line.find(":")
		if colon > 0:
			out[line.substr(0, colon).to_lower()] = line.substr(colon + 1).strip_edges()
	return out
