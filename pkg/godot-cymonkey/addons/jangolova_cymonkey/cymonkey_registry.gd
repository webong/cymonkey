class_name CymonkeyRegistry
extends Node

const CymonkeyProtocol = preload("res://addons/jangolova_cymonkey/cymonkey_protocol.gd")

signal event_published(event: Dictionary)

@export var registrations: Array = []

var _allowlist: Dictionary = {}
var _event_log: Array[Dictionary] = []
var _revision := 1
var _event_sequence := 0

func _ready() -> void:
	_build_allowlist()

func dispatch(method: String, params: Dictionary):
	match method:
		CymonkeyProtocol.METHOD_HELLO:
			return _hello()
		CymonkeyProtocol.METHOD_CAPABILITIES:
			return _capabilities()
		CymonkeyProtocol.METHOD_DESCRIBE:
			return _describe()
		CymonkeyProtocol.METHOD_ACT:
			return _act(params)
		CymonkeyProtocol.METHOD_EVENTS:
			return _read_events(params)
		CymonkeyProtocol.METHOD_HEALTH:
			return _health()
		_:
			return _error("method_not_found", "Unsupported Cymonkey method.")

func _build_allowlist() -> void:
	_allowlist.clear()
	for registration in registrations:
		if not registration is Dictionary:
			push_error("Cymonkey registrations must be dictionaries.")
			continue
		var stable_id: String = str(registration.get("id", ""))
		var kind: String = str(registration.get("kind", ""))
		var target: Node = registration.get("target") as Node
		if target == null or kind not in CymonkeyProtocol.RESOURCE_KINDS or not CymonkeyProtocol.is_stable_id(stable_id, kind):
			push_error("Cymonkey registration requires a target and stable kind-prefixed ID.")
			continue
		if _allowlist.has(stable_id):
			push_error("Duplicate Cymonkey resource ID: %s" % stable_id)
			continue
		_allowlist[stable_id] = registration

func _hello() -> Dictionary:
	return {
		"protocolVersion": CymonkeyProtocol.VERSION,
		"compatibleProtocols": CymonkeyProtocol.COMPATIBLE_PROTOCOLS,
		"implementation": {"name": "jangolova-godot-cymonkey", "version": "0.2.0"},
		"profiles": [CymonkeyProtocol.PROFILE_ENGINE],
		"backends": [CymonkeyProtocol.BACKEND_GODOT],
		"features": ["events.cursor", "resources.explicit-allowlist"]
	}

func _capabilities() -> Array:
	return [
		{"name": "resource.describe", "profile": CymonkeyProtocol.PROFILE_ENGINE, "backend": CymonkeyProtocol.BACKEND_GODOT,
			"support": "native", "lifetime": "attachment", "persistence": "session",
			"effect": "read", "targetKinds": CymonkeyProtocol.RESOURCE_KINDS,
			"inputSchema": {"type": "object", "additionalProperties": false}},
		{"name": "object.visible.set", "profile": CymonkeyProtocol.PROFILE_ENGINE, "backend": CymonkeyProtocol.BACKEND_GODOT,
			"support": "native", "lifetime": "attachment", "persistence": "session",
			"effect": "write", "targetKinds": ["object", "ui", "camera"],
			"inputSchema": {"type": "object", "properties": {"visible": {"type": "boolean"}}, "required": ["visible"], "additionalProperties": false}},
		{"name": "object.transform.set", "profile": CymonkeyProtocol.PROFILE_ENGINE, "backend": CymonkeyProtocol.BACKEND_GODOT,
			"support": "native", "lifetime": "attachment", "persistence": "session",
			"effect": "write", "targetKinds": ["scene", "object", "ui", "camera"],
			"inputSchema": {"type": "object", "properties": {"position": {"type": "object"}, "rotationDegrees": {"type": "number"}, "scale": {"type": "object"}}, "additionalProperties": false}},
		{"name": "material.color.set", "profile": CymonkeyProtocol.PROFILE_ENGINE, "backend": CymonkeyProtocol.BACKEND_GODOT,
			"support": "native", "lifetime": "attachment", "persistence": "session",
			"effect": "write", "targetKinds": ["object", "ui", "material"],
			"inputSchema": {"type": "object", "properties": {"color": {"type": "string"}}, "required": ["color"], "additionalProperties": false}},
		{"name": "camera.transform.set", "profile": CymonkeyProtocol.PROFILE_ENGINE, "backend": CymonkeyProtocol.BACKEND_GODOT,
			"support": "native", "lifetime": "attachment", "persistence": "session",
			"effect": "write", "targetKinds": ["camera"],
			"inputSchema": {"type": "object", "properties": {"position": {"type": "object"}, "zoom": {"type": "object"}, "rotationDegrees": {"type": "number"}}, "additionalProperties": false}},
		{"name": "ui.text.set", "profile": CymonkeyProtocol.PROFILE_ENGINE, "backend": CymonkeyProtocol.BACKEND_GODOT,
			"support": "native", "lifetime": "attachment", "persistence": "session",
			"effect": "write", "targetKinds": ["ui"],
			"inputSchema": {"type": "object", "properties": {"text": {"type": "string"}}, "required": ["text"], "additionalProperties": false}}
	]

func _describe() -> Dictionary:
	var surfaces: Array = []
	var ids := _allowlist.keys()
	ids.sort()
	for stable_id in ids:
		surfaces.append(_describe_resource(_allowlist[stable_id]))
	return {"revision": str(_revision), "surfaces": surfaces, "augmentations": []}

func _act(params: Dictionary) -> Dictionary:
	var name := str(params.get("name", ""))
	var input: Dictionary = params.get("input", {})
	if not input is Dictionary:
		input = {}
	var target_id := str(input.get("targetId", params.get("targetId", "")))
	if target_id.is_empty():
		return _error("invalid_input", "Cymonkey action requires input.targetId.")
	if not _allowlist.has(target_id):
		return _error("target_not_allowlisted", "Cymonkey target is not allowlisted.")
	var expected_revision := str(input.get("expectedRevision", ""))
	if not expected_revision.is_empty() and expected_revision != str(_revision):
		return _error("stale_revision", "Revision %s is stale; current revision is %d." % [expected_revision, _revision])
	var registration: Dictionary = _allowlist[target_id]
	var actions: Array = registration.get("actions", [])
	if name not in actions:
		return _error("action_not_allowlisted", "Cymonkey action is not allowlisted for this target.")
	if name == "resource.describe":
		return _describe_resource(registration)
	if name == "object.visible.set":
		if not input.has("visible") or not (input["visible"] is bool):
			return _error("invalid_input", "visible is required.")
		var target: Node = registration["target"] as Node
		if not target is CanvasItem:
			return _error("invalid_target", "Target does not expose visibility.")
		target.visible = input["visible"]
		_revision += 1
		_publish("event:resource-changed", target_id, {"visible": target.visible})
		return {"ok": true, "revision": str(_revision)}
	if name == "object.transform.set":
		return _set_transform(registration, target_id, input)
	if name == "material.color.set":
		return _set_color(registration, target_id, input)
	if name == "camera.transform.set":
		return _set_camera_transform(registration, target_id, input)
	if name == "ui.text.set":
		return _set_text(registration, target_id, input)
	return _error("action_not_implemented", "Allowlisted action has no Godot handler.")

func _set_transform(registration: Dictionary, target_id: String, input: Dictionary) -> Dictionary:
	var target = registration["target"]
	if not target is Node2D and not target is Control:
		return _error("invalid_target", "Target does not expose a 2D transform.")
	if input.has("position"):
		var position = _vector2_from(input["position"])
		if position == null:
			return _error("invalid_input", "position requires numeric x and y.")
		target.position = position
	if input.has("rotationDegrees"):
		if not _is_number(input["rotationDegrees"]):
			return _error("invalid_input", "rotationDegrees must be numeric.")
		_set_rotation_degrees(target, float(input["rotationDegrees"]))
	if input.has("scale"):
		var scale = _vector2_from(input["scale"])
		if scale == null:
			return _error("invalid_input", "scale requires numeric x and y.")
		target.scale = scale
	_revision += 1
	_publish("event:resource-changed", target_id, _transform_properties(target))
	return {"ok": true, "revision": str(_revision)}

func _set_color(registration: Dictionary, target_id: String, input: Dictionary) -> Dictionary:
	var target = registration["target"]
	if not target is CanvasItem or not input.has("color"):
		return _error("invalid_input", "color is required for a CanvasItem target.")
	var color_text := str(input["color"])
	var color := Color.from_string(color_text, Color(-1, -1, -1, -1))
	if color.a < 0.0:
		return _error("invalid_input", "color must be a valid Godot color string.")
	target.self_modulate = color
	_revision += 1
	_publish("event:resource-changed", target_id, {"color": color.to_html(true)})
	return {"ok": true, "revision": str(_revision)}

func _set_camera_transform(registration: Dictionary, target_id: String, input: Dictionary) -> Dictionary:
	var target = registration["target"]
	if not target is Camera2D:
		return _error("invalid_target", "camera.transform.set requires a Camera2D target.")
	if input.has("position"):
		var position = _vector2_from(input["position"])
		if position == null:
			return _error("invalid_input", "position requires numeric x and y.")
		target.position = position
	if input.has("zoom"):
		var zoom = _vector2_from(input["zoom"])
		if zoom == null or zoom.x <= 0.0 or zoom.y <= 0.0:
			return _error("invalid_input", "zoom requires positive numeric x and y.")
		target.zoom = zoom
	if input.has("rotationDegrees"):
		if not _is_number(input["rotationDegrees"]):
			return _error("invalid_input", "rotationDegrees must be numeric.")
		_set_rotation_degrees(target, float(input["rotationDegrees"]))
	_revision += 1
	_publish("event:resource-changed", target_id, {"position": {"x": target.position.x, "y": target.position.y}, "zoom": {"x": target.zoom.x, "y": target.zoom.y}})
	return {"ok": true, "revision": str(_revision)}

func _set_text(registration: Dictionary, target_id: String, input: Dictionary) -> Dictionary:
	var target = registration["target"]
	if not target is Label or not input.has("text") or not (input["text"] is String):
		return _error("invalid_input", "text requires a Label target and string value.")
	target.text = input["text"]
	_revision += 1
	_publish("event:resource-changed", target_id, {"text": target.text})
	return {"ok": true, "revision": str(_revision)}

func _vector2_from(value):
	if not value is Dictionary or not value.has("x") or not value.has("y"):
		return null
	if not _is_number(value["x"]) or not _is_number(value["y"]):
		return null
	return Vector2(float(value["x"]), float(value["y"]))

func _is_number(value) -> bool:
	return value is int or value is float

func _set_rotation_degrees(target, value: float) -> void:
	target.rotation = deg_to_rad(value)

func _read_events(query: Dictionary) -> Dictionary:
	var after := int(str(query.get("after", "0")))
	var limit := clampi(int(query.get("limit", 100)), 1, 1000)
	var selected: Array = []
	var cursor := str(after)
	for event in _event_log:
		if int(str(event["id"])) <= after:
			continue
		selected.append(event.duplicate(true))
		cursor = str(event["id"])
		if selected.size() >= limit:
			break
	return {"events": selected, "cursor": cursor}

func _health() -> Dictionary:
	return {"status": "ready", "observedAt": Time.get_datetime_string_from_system(true)}

func _describe_resource(registration: Dictionary) -> Dictionary:
	var target: Node = registration["target"] as Node
	return {"id": registration["id"], "profile": CymonkeyProtocol.PROFILE_ENGINE, "kind": registration["kind"],
		"label": registration.get("label", ""), "properties": _properties_for(target)}

func _properties_for(target: Node) -> Dictionary:
	var properties: Dictionary = {}
	if target is CanvasItem:
		properties["visible"] = target.visible
	if target is Node2D or target is Control:
		properties.merge(_transform_properties(target))
	if target is Label:
		properties["text"] = target.text
	if target is Camera2D:
		properties["enabled"] = target.enabled
		properties["zoom"] = {"x": target.zoom.x, "y": target.zoom.y}
	return properties

func _transform_properties(target) -> Dictionary:
	var rotation_degrees := rad_to_deg(target.rotation)
	return {"position": {"x": target.position.x, "y": target.position.y}, "rotationDegrees": rotation_degrees,
		"scale": {"x": target.scale.x, "y": target.scale.y}}

func _publish(kind: String, source_id: String, data: Dictionary) -> void:
	_event_sequence += 1
	var event := {"id": str(_event_sequence), "type": kind, "sourceId": source_id,
		"occurredAt": Time.get_datetime_string_from_system(true), "data": data}
	_event_log.append(event)
	if _event_log.size() > 256:
		_event_log.pop_front()
	event_published.emit(event)

func _error(code: String, message: String) -> Dictionary:
	return {"__cymonkey_error": {"code": code, "message": message}}
