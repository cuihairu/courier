# 测试件:原始 HTTP/1.1 mock(TCPServer 真监听)。记录解析后的请求
# (method/path/headers/body,头键小写);按序回放 responses(「HTTP/」开头原样
# 发送,裸串包成 200 JSON 响应)。enqueue_stream 入队 SSE 流响应并持有连接,
# pushes 逐段推送,hangup() 断开;非流模式响应后即断。
# 分框:stream_chunked 默认真——Godot HTTPClient 不支持无 Content-Length 的
# close 分隔响应体(收完头即判响应完成,永不进 STATUS_BODY),SSE 必须 chunked;
# 自建网关(Go net/http 在 Content-Length 缺省时自动补 chunked)天然满足。
extends RefCounted

const SSE_HEAD := "HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\nTransfer-Encoding: chunked\r\n\r\n"

var srv: TCPServer
var responses: Array = []
var seen: Array = []
var port: int = 0
var stream_chunked: bool = true
var held: StreamPeerTCP = null  # 长连当前连接(SSE 响应后持有)
var pushes: Array = []  # 待推送给长连的原始文本段(跨 chunk 分行用例由此构造)
var close_requested: bool = false


func _init() -> void:
	srv = TCPServer.new()
	for p in range(18092, 18097):
		if srv.listen(p, "127.0.0.1") == OK:
			port = p
			break


func stop() -> void:
	srv.stop()


## 入队一条 SSE 流响应;first 为首批帧文本(可空,随响应头同段发出)。
func enqueue_stream(first: String = "") -> void:
	responses.append("__SSE__" + first)


func hangup() -> void:
	close_requested = true


func _chunk(text: String) -> PackedByteArray:
	var body := text.to_utf8_buffer()
	var out := PackedByteArray()
	out.append_array(("%x\r\n" % body.size()).to_utf8_buffer())
	out.append_array(body)
	out.append_array("\r\n".to_utf8_buffer())
	return out


func _wire(text: String) -> PackedByteArray:
	if stream_chunked:
		return _chunk(text)
	return text.to_utf8_buffer()


## 协程:泵一步。长连保持期先推积压、按需断开;否则服务一个请求
## (读全头 + Content-Length 体后应答)。调用方 await。
func pump(tree: SceneTree) -> void:
	if held != null:
		var pending := PackedByteArray()
		while not pushes.is_empty():
			pending.append_array(_wire(str(pushes.pop_front())))
		if pending.size() > 0:
			held.put_data(pending)
		if close_requested:
			if stream_chunked:
				held.put_data("0\r\n\r\n".to_utf8_buffer())  # chunked 终止块
			held.disconnect_from_host()
			held = null
			close_requested = false
		await tree.process_frame
		return
	if not srv.is_connection_available():
		await tree.process_frame
		return
	var c := srv.take_connection()
	var raw := ""
	while not raw.contains("\r\n\r\n"):
		c.poll()
		var n := c.get_available_bytes()
		if n > 0:
			raw += c.get_utf8_string(n)
		else:
			await tree.process_frame
	var head_end := raw.find("\r\n\r\n") + 4
	var cl := _content_length(raw)
	while raw.length() - head_end < cl:
		c.poll()
		var n2 := c.get_available_bytes()
		if n2 > 0:
			raw += c.get_utf8_string(n2)
		else:
			await tree.process_frame
	seen.append(_parse_request(raw))
	var resp := "HTTP/1.1 500 Bad Script\r\nContent-Length: 0\r\n\r\n"
	var stream_first := ""
	var is_stream := false
	if not responses.is_empty():
		resp = str(responses.pop_front())
		if resp.begins_with("__SSE__"):
			is_stream = true
			stream_first = resp.substr(7)
		elif not resp.begins_with("HTTP/"):
			# 裸体入队:包成 200 JSON 响应(信封 JSON 由用例预包)。
			resp = "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n%s" % [resp.length(), resp]
	if is_stream:
		var head := SSE_HEAD if stream_chunked else SSE_HEAD.replace("Transfer-Encoding: chunked\r\n", "")
		var wire := head.to_utf8_buffer()
		if stream_first != "":
			wire.append_array(_wire(stream_first))  # 首批帧随头同段发出
		c.put_data(wire)
		held = c  # SSE 长连:不断开,后续 pump 推送/挂断
	else:
		c.put_data(resp.to_utf8_buffer())
		c.disconnect_from_host()


func _content_length(raw: String) -> int:
	for line in raw.split("\r\n"):
		if line.to_lower().begins_with("content-length:"):
			return int(line.substr(15).strip_edges())
	return 0


func _parse_request(raw: String) -> Dictionary:
	var head := raw.substr(0, raw.find("\r\n\r\n"))
	var lines := head.split("\r\n")
	var parts := (lines[0] as String).split(" ")
	var headers := {}
	for i in range(1, lines.size()):
		var colon := (lines[i] as String).find(":")
		if colon > 0:
			headers[(lines[i] as String).substr(0, colon).to_lower()] = (lines[i] as String).substr(colon + 1).strip_edges()
	var body := raw.substr(raw.find("\r\n\r\n") + 4)
	return {"method": parts[0], "path": parts[1], "headers": headers, "body": body}
