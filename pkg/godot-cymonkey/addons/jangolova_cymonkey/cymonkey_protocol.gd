class_name CymonkeyProtocol
extends RefCounted

const VERSION := "jangolova.cymonkey/v1alpha2"
const MAXIMUM_MESSAGE_BYTES := 4 * 1024 * 1024

const PROFILE_ENGINE := "engine"
const BACKEND_GODOT := "engine-godot"

const METHOD_HELLO := "hello"
const METHOD_CAPABILITIES := "capabilities"
const METHOD_DESCRIBE := "describe"
const METHOD_ACT := "act"
const METHOD_EVENTS := "events"
const METHOD_HEALTH := "health"

const RESOURCE_KINDS := [
	"scene", "object", "ui", "camera", "material", "animation", "timeline", "artifact", "event"
]

static func is_stable_id(value: String, kind: String) -> bool:
	var expression := RegEx.new()
	expression.compile("^[a-z][a-z0-9-]{0,31}:[A-Za-z0-9][A-Za-z0-9._/-]{0,127}$")
	return expression.search(value) != null and value.begins_with(kind + ":")
