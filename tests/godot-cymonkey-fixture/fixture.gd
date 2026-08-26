extends Node

var _capture_path := ""

func _ready() -> void:
	var registry = $CymonkeyRegistry
	var host = $CymonkeyWebSocketHost
	registry.registrations = [{
		"id": "object:fixture",
		"kind": "object",
		"label": "House compatibility target",
		"target": $House,
		"actions": ["resource.describe", "object.visible.set", "object.transform.set"]
	}, {
		"id": "object:house",
		"kind": "object",
		"label": "Small Cymonkey test house",
		"target": $House,
		"actions": ["resource.describe", "object.visible.set", "object.transform.set"]
	}, {
		"id": "object:door",
		"kind": "object",
		"label": "Front door",
		"target": $House/Door,
		"actions": ["resource.describe", "object.visible.set", "object.transform.set"]
	}, {
		"id": "object:window-left",
		"kind": "object",
		"label": "Left window",
		"target": $House/WindowLeft,
		"actions": ["resource.describe", "object.visible.set"]
	}, {
		"id": "object:window-right",
		"kind": "object",
		"label": "Right window",
		"target": $House/WindowRight,
		"actions": ["resource.describe", "object.visible.set"]
	}, {
		"id": "object:hero",
		"kind": "object",
		"label": "House visitor",
		"target": $Hero,
		"actions": ["resource.describe", "object.visible.set", "object.transform.set"]
	}, {
		"id": "material:interior-light",
		"kind": "material",
		"label": "Interior light material",
		"target": $House/InteriorLight,
		"actions": ["resource.describe", "material.color.set"]
	}, {
		"id": "camera:main",
		"kind": "camera",
		"label": "Main house camera",
		"target": $CameraMain,
		"actions": ["resource.describe", "camera.transform.set"]
	}, {
		"id": "ui:status",
		"kind": "ui",
		"label": "House status label",
		"target": $Status,
		"actions": ["resource.describe", "object.visible.set", "ui.text.set"]
	}]
	registry._build_allowlist()
	host.registry_path = registry.get_path()
	registry.event_published.connect(_on_registry_event)
	_capture_path = OS.get_environment("JANGOLOVA_CAPTURE_PATH")
	if not _capture_path.is_empty():
		call_deferred("_capture_frame")

func _on_registry_event(_event: Dictionary) -> void:
	if not _capture_path.is_empty():
		call_deferred("_capture_frame")

func _capture_frame() -> void:
	await get_tree().process_frame
	await RenderingServer.frame_post_draw
	var image := get_viewport().get_texture().get_image()
	var error := image.save_png(_capture_path)
	if error != OK:
		push_error("Cymonkey fixture could not save capture: %s" % error)
