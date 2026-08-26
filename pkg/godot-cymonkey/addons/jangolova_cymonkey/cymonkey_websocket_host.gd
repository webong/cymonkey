class_name CymonkeyWebSocketHost
extends Node

const CymonkeyProtocol = preload("res://addons/jangolova_cymonkey/cymonkey_protocol.gd")

@export var listen_host := "0.0.0.0"
@export var listen_port := 8090
@export var bearer_token := ""
@export var registry_path: NodePath

var _server := TCPServer.new()
var _peer: WebSocketPeer
var _registry
var _authenticated := false

func _ready() -> void:
	if bearer_token.is_empty():
		bearer_token = OS.get_environment("JANGOLOVA_CYMONKEY_TOKEN")
	var environment_port := OS.get_environment("JANGOLOVA_CYMONKEY_PORT")
	if not environment_port.is_empty():
		listen_port = int(environment_port)
	_registry = get_node_or_null(registry_path)
	if _registry == null or bearer_token.is_empty():
		push_error("Pacman WebSocket host requires a registry and bearer token.")
		return
	var error := _server.listen(listen_port, listen_host)
	if error != OK:
		push_error("Pacman WebSocket listener failed: %s" % error)

func _process(_delta: float) -> void:
	if _peer == null and _server.is_connection_available():
		var stream := _server.take_connection()
		var candidate := WebSocketPeer.new()
		var error := candidate.accept_stream(stream)
		if error != OK:
			stream.disconnect_from_host()
			return
		_peer = candidate
		_authenticated = false
	if _peer == null:
		return
	_peer.poll()
	if _peer.get_ready_state() == WebSocketPeer.STATE_OPEN:
		if not _authenticated and _has_valid_authorization(_peer.get_handshake_headers()):
			_authenticated = true
		while _peer.get_available_packet_count() > 0:
			var packet := _peer.get_packet()
			if packet.size() > CymonkeyProtocol.MAXIMUM_MESSAGE_BYTES:
				_peer.close(1009, "Pacman message is too large")
				return
			if not _authenticated:
				if not _authenticate_packet(packet):
					_peer.close(1008, "Pacman authorization required")
					return
				_authenticated = true
				_send({"type": "cymonkey.authenticated"})
				continue
			_handle_message(packet.get_string_from_utf8())
	elif _peer.get_ready_state() == WebSocketPeer.STATE_CLOSED:
		_peer = null
		_authenticated = false

func _has_valid_authorization(headers: PackedStringArray) -> bool:
	var expected := "Bearer " + bearer_token
	for index in headers.size():
		var header := headers[index]
		var separator := header.find(":")
		if separator > 0:
			var name := header.substr(0, separator).strip_edges().to_lower()
			if name == "authorization":
				return _constant_time_equals(header.substr(separator + 1).strip_edges(), expected)
		elif header.strip_edges().to_lower() == "authorization" and index + 1 < headers.size():
			return _constant_time_equals(headers[index + 1].strip_edges(), expected)
	return false

func _authenticate_packet(packet: PackedByteArray) -> bool:
	var value = JSON.parse_string(packet.get_string_from_utf8())
	if not value is Dictionary or value.get("type", "") != "auth":
		return false
	var token := str(value.get("token", ""))
	return _constant_time_equals(token, bearer_token)

func _constant_time_equals(left: String, right: String) -> bool:
	if left.length() != right.length():
		return false
	var difference := 0
	for index in left.length():
		difference |= left.unicode_at(index) ^ right.unicode_at(index)
	return difference == 0

func _handle_message(text: String) -> void:
	var request = JSON.parse_string(text)
	if not request is Dictionary:
		_send_error(0, "invalid_json", "Pacman request must be an object.")
		return
	var request_id = request.get("id", 0)
	var method := str(request.get("method", ""))
	var params: Dictionary = request.get("params", {})
	if method.is_empty() or not params is Dictionary:
		_send_error(request_id, "invalid_request", "Pacman request requires method and object params.")
		return
	var result = _registry.dispatch(method, params)
	if result is Dictionary and result.has("__cymonkey_error"):
		var error: Dictionary = result["__cymonkey_error"]
		_send_error(request_id, str(error.get("code", "cymonkey_error")), str(error.get("message", "Cymonkey error.")))
		return
	_send({"id": request_id, "result": result})

func _send_error(request_id, code: String, message: String) -> void:
	_send({"id": request_id, "error": {"code": code, "message": message}})

func _send(value: Dictionary) -> void:
	if _peer != null and _peer.get_ready_state() == WebSocketPeer.STATE_OPEN:
		_peer.send_text(JSON.stringify(value))

func _exit_tree() -> void:
	if _peer != null:
		_peer.close(1000, "Pacman detached")
	_server.stop()
