# L3 平台传输(Godot HTTPClient):L2 Transport 抽象的平台实现。
# HTTPClient 为 poll 增量模型(无 Node 依赖,headless 可跑):每次 poll 推进一步,
# 帧间用 frame_f 让出(默认取 Engine 主循环;--script 测试环境注入)。
# 返回 TransportResult 契约(见 src/core/transport.gd):响应头键小写归一。

class_name CourierHTTPTransport
extends CourierTransport

const DEFAULT_TIMEOUT_MS := 15000

var frame_f: Callable = Callable()  # () -> void 协程:让出一帧(测试注入;默认 Engine 主循环)
var timeout_ms: int = DEFAULT_TIMEOUT_MS


## 协程发送;调用方必须 await。
func send(request: Dictionary) -> Dictionary:
	var u: Dictionary = _split_url(str(request.get("url", "")))
	if str(u["host"]) == "":
		return {"ok": false, "message": "courier: url 无效:" + str(request.get("url", ""))}
	var hc := HTTPClient.new()
	var err: int
	if bool(u["tls"]):
		err = hc.connect_to_host(str(u["host"]), int(u["port"]), TLSOptions.client())
	else:
		err = hc.connect_to_host(str(u["host"]), int(u["port"]))
	if err != OK:
		return {"ok": false, "message": "courier: connect 失败(%d)" % err}
	var deadline := Time.get_ticks_msec() + timeout_ms
	while hc.get_status() == HTTPClient.STATUS_RESOLVING or hc.get_status() == HTTPClient.STATUS_CONNECTING:
		if Time.get_ticks_msec() > deadline:
			return {"ok": false, "message": "courier: connect 超时"}
		await _frame()
		hc.poll()
	if hc.get_status() != HTTPClient.STATUS_CONNECTED:
		return {"ok": false, "message": "courier: 连接失败(status %d)" % hc.get_status()}

	var headers := PackedStringArray()
	var req_headers: Dictionary = request.get("headers", {})
	for k in req_headers:
		headers.append(str(k) + ": " + str(req_headers[k]))
	var body_str := str(request["jsonBody"]) if request.has("jsonBody") else ""
	err = hc.request(_method(str(request.get("method", "GET"))), str(u["path"]), headers, body_str)
	if err != OK:
		return {"ok": false, "message": "courier: request 失败(%d)" % err}

	var resp_body := PackedByteArray()
	var resp_headers := {}
	var expected := -1  # 响应体长度(Content-Length);-1 = 未得头
	deadline = Time.get_ticks_msec() + timeout_ms
	while true:
		hc.poll()
		var st := hc.get_status()
		# 响应头仅在收体期间可读(收完后被引擎清空),须在循环内捕获。
		if resp_headers.is_empty() and hc.has_response():
			resp_headers = _response_headers(hc)
			var cl := str(resp_headers.get("content-length", ""))
			if cl.is_valid_int():
				expected = int(cl)
		if st == HTTPClient.STATUS_BODY:
			var chunk := hc.read_response_body_chunk()
			if chunk.size() > 0:
				resp_body.append_array(chunk)
		# 完整性以 Content-Length 计数,不看终态:对端发完即断时,EOF 可与末字节
		# 同一 poll 到达,状态直接跳 DISCONNECTED,CONNECTED 未必可观察。
		if expected >= 0 and resp_body.size() >= expected:
			break
		if st == HTTPClient.STATUS_CONNECTED and hc.has_response() and expected < 0:
			break  # 无 CL 头且已收全(如零体响应):头到即完成
		if st == HTTPClient.STATUS_CONNECTION_ERROR:
			return {"ok": false, "message": "courier: 连接错误"}
		if st == HTTPClient.STATUS_DISCONNECTED:
			if expected < 0 and hc.has_response():
				break  # 无 CL:以断连收尾(close 分隔)
			return {"ok": false, "message": "courier: 连接错误"}
		if Time.get_ticks_msec() > deadline:
			return {"ok": false, "message": "courier: 响应超时"}
		await _frame()
	return {
		"ok": true,
		"status_code": hc.get_response_code(),
		"headers": resp_headers,
		"body": resp_body.get_string_from_utf8(),
	}


# 让出一帧(协程):注入的 frame_f 优先;缺省取 Engine 主循环。
func _frame() -> void:
	if frame_f.is_valid():
		await frame_f.call()
		return
	var ml: Variant = Engine.get_main_loop()
	if ml is SceneTree:
		await (ml as SceneTree).process_frame


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


# 方法名 → HTTPClient 枚举(未知容忍落 GET)。
func _method(m: String) -> int:
	match m:
		"POST":
			return HTTPClient.METHOD_POST
		"PUT":
			return HTTPClient.METHOD_PUT
		"PATCH":
			return HTTPClient.METHOD_PATCH
		"DELETE":
			return HTTPClient.METHOD_DELETE
		_:
			return HTTPClient.METHOD_GET


# 响应头 → Dictionary(键小写归一,契约 transport.gd)。
func _response_headers(hc: HTTPClient) -> Dictionary:
	var out := {}
	for line in hc.get_response_headers():
		var colon := line.find(":")
		if colon > 0:
			out[line.substr(0, colon).to_lower()] = line.substr(colon + 1).strip_edges()
	return out
