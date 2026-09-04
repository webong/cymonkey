"""Jangolova's dependency-free Cymonkey bridge for Blender.

The module intentionally keeps Blender imports optional. This makes protocol
and registration tests runnable with the system Python while the same file can
be loaded by Blender's bundled Python interpreter.
"""

from __future__ import annotations

import base64
import hashlib
import json
import os
import queue
import re
import socket
import struct
import threading
from dataclasses import dataclass
from datetime import datetime, timezone
from typing import Any, Callable, Iterable


PROTOCOL_VERSION = "cymonkey/v1alpha1"
DOMAIN_RENDER = "render"
RUNTIME_BLENDER = "blender"
DRIVER_WEBSOCKET = "websocket"
RESOURCE_KINDS = {
    "scene",
    "object",
    "ui",
    "camera",
    "material",
    "animation",
    "timeline",
    "artifact",
    "event",
}
ID_PATTERN = re.compile(r"^[a-z][a-z0-9-]{0,31}:[A-Za-z0-9][A-Za-z0-9._/-]{0,127}$")


class CymonkeyError(Exception):
    def __init__(self, code: str, message: str):
        super().__init__(message)
        self.code = code
        self.message = message


def _now() -> str:
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


def _number(value: Any, name: str) -> float:
    if isinstance(value, bool) or not isinstance(value, (int, float)):
        raise CymonkeyError("invalid_input", f"{name} must be numeric")
    return float(value)


def _vector(value: Any, name: str) -> tuple[float, float, float]:
    if isinstance(value, dict):
        return (
            _number(value.get("x"), f"{name}.x"),
            _number(value.get("y"), f"{name}.y"),
            _number(value.get("z"), f"{name}.z"),
        )
    if isinstance(value, (list, tuple)) and len(value) == 3:
        return tuple(_number(item, f"{name}[{index}]") for index, item in enumerate(value))
    raise CymonkeyError("invalid_input", f"{name} must contain x, y, and z")


def _set_vector(target: Any, value: Any, name: str) -> None:
    vector = _vector(value, name)
    if hasattr(target, "x") and hasattr(target, "y") and hasattr(target, "z"):
        target.x, target.y, target.z = vector
    else:
        target[0], target[1], target[2] = vector


def _read_vector(value: Any) -> dict[str, float] | None:
    if value is None:
        return None
    try:
        if all(hasattr(value, key) for key in ("x", "y", "z")):
            return {"x": float(value.x), "y": float(value.y), "z": float(value.z)}
        return {"x": float(value[0]), "y": float(value[1]), "z": float(value[2])}
    except (IndexError, TypeError, ValueError):
        return None


def _set_color(target: Any, value: str) -> str:
    if not isinstance(value, str) or not value.strip():
        raise CymonkeyError("invalid_input", "color is required")
    text = value.strip().lstrip("#")
    if len(text) not in (6, 8) or any(character not in "0123456789abcdefABCDEF" for character in text):
        raise CymonkeyError("invalid_input", "color must be a hex RGB or RGBA value")
    if len(text) == 6:
        text += "FF"
    rgba = tuple(int(text[index : index + 2], 16) / 255 for index in range(0, 8, 2))
    if hasattr(target, "diffuse_color"):
        target.diffuse_color = rgba
    elif hasattr(target, "color"):
        target.color = rgba
    elif isinstance(target, dict):
        target["color"] = "#" + text.upper()
    else:
        raise CymonkeyError("invalid_target", "material does not expose a color")
    return "#" + text.upper()


class CymonkeyRegistry:
    """Explicit resource registry and render-domain protocol implementation."""

    def __init__(self, bpy_api: Any = None):
        self.bpy = bpy_api
        self._registrations: dict[str, dict[str, Any]] = {}
        self._events: list[dict[str, Any]] = []
        self._revision = 1
        self._event_sequence = 0

    def register(self, *, id: str, kind: str, target: Any, actions: Iterable[str], label: str = "") -> Callable[[], None]:
        if not isinstance(id, str) or not ID_PATTERN.fullmatch(id) or not id.startswith(f"{kind}:"):
            raise ValueError("resource ID must be stable and prefixed with its kind")
        if kind not in RESOURCE_KINDS:
            raise ValueError(f"unsupported resource kind {kind!r}")
        if target is None:
            raise ValueError("resource target is required")
        if id in self._registrations:
            raise ValueError(f"duplicate Cymonkey resource {id!r}")
        action_list = sorted({str(action) for action in actions})
        self._registrations[id] = {"id": id, "kind": kind, "target": target, "actions": action_list, "label": label}
        self._revision += 1
        self._publish("event:resource-registered", id, {"kind": kind})
        return lambda: self.unregister(id)

    def unregister(self, id: str) -> None:
        if id not in self._registrations:
            raise KeyError(id)
        del self._registrations[id]
        self._revision += 1
        self._publish("event:resource-unregistered", id, {})

    def hello(self) -> dict[str, Any]:
        return {
            "protocolVersion": PROTOCOL_VERSION,
            "implementation": {"name": "jangolova-blender", "version": "0.1.0"},
            "domains": [DOMAIN_RENDER],
            "runtimes": [RUNTIME_BLENDER],
            "drivers": [DRIVER_WEBSOCKET],
            "features": ["events.cursor", "resources.explicit-allowlist", "render.headless"],
        }

    def capabilities(self) -> list[dict[str, Any]]:
        def capability(name: str, effect: str, resource_kinds: list[str]) -> dict[str, Any]:
            return {
                "name": name,
                "domain": DOMAIN_RENDER,
                "runtime": RUNTIME_BLENDER,
                "driver": DRIVER_WEBSOCKET,
                "support": "native",
                "lifetime": "attachment",
                "persistence": "session",
                "effect": effect,
                "resourceKinds": resource_kinds,
                "inputSchema": {"type": "object", "additionalProperties": False},
            }

        return [
            capability("resource.describe", "read", sorted(RESOURCE_KINDS - {"event"})),
            capability("object.visibility.set", "write", ["object", "ui"]),
            capability("object.transform.set", "write", ["scene", "object", "ui", "camera"]),
            capability("material.color.set", "write", ["object", "material"]),
            capability("camera.transform.set", "write", ["camera"]),
            capability("render.frame", "write", ["scene", "camera"]),
        ]

    def describe(self) -> dict[str, Any]:
        return {
            "revision": str(self._revision),
            "surfaces": [self._describe_resource(self._registrations[id]) for id in sorted(self._registrations)],
            "augmentations": [],
        }

    def health(self) -> dict[str, str]:
        return {"status": "ready", "observedAt": _now()}

    def events(self, params: dict[str, Any] | None = None) -> dict[str, Any]:
        params = params or {}
        try:
            after = int(str(params.get("after", "0")))
            limit = min(max(int(params.get("limit", 100)), 1), 256)
        except (TypeError, ValueError):
            raise CymonkeyError("invalid_input", "events.after and events.limit must be integers")
        if after < 0:
            raise CymonkeyError("invalid_input", "events.after must be non-negative")
        types = set(params.get("types", [])) if isinstance(params.get("types", []), list) else set()
        selected = [event for event in self._events if int(event["id"]) > after and (not types or event["type"] in types)][:limit]
        return {"events": selected, "cursor": str(self._event_sequence)}

    def dispatch(self, method: str, params: dict[str, Any] | None = None) -> dict[str, Any]:
        params = params if isinstance(params, dict) else {}
        if method == "hello":
            return self.hello()
        if method == "capabilities":
            return self.capabilities()
        if method == "describe":
            return self.describe()
        if method == "health":
            return self.health()
        if method == "events":
            return self.events(params)
        if method == "act":
            return self._act(params)
        raise CymonkeyError("method_not_found", f"unsupported Cymonkey method {method!r}")

    def _act(self, params: dict[str, Any]) -> dict[str, Any]:
        name = params.get("name")
        if not isinstance(name, str) or not name:
            raise CymonkeyError("invalid_input", "action name is required")
        input_value = params.get("input") if isinstance(params.get("input"), dict) else {}
        target_id = input_value.get("targetId", params.get("targetId"))
        if not isinstance(target_id, str) or not target_id:
            raise CymonkeyError("invalid_input", "input.targetId is required")
        registration = self._registrations.get(target_id)
        if registration is None:
            raise CymonkeyError("target_not_allowlisted", "Cymonkey target is not allowlisted")
        if name not in registration["actions"]:
            raise CymonkeyError("action_not_allowlisted", "Cymonkey action is not allowlisted for this target")
        expected = input_value.get("expectedRevision")
        if expected not in (None, "") and str(expected) != str(self._revision):
            raise CymonkeyError("stale_revision", f"expected revision {expected} is stale; current revision is {self._revision}")
        if name == "resource.describe":
            return self._describe_resource(registration)
        target = registration["target"]
        if name == "object.visibility.set":
            visible = input_value.get("visible")
            if not isinstance(visible, bool):
                raise CymonkeyError("invalid_input", "visible must be boolean")
            self._set_visibility(target, visible)
            result = {"ok": True, "visible": visible}
        elif name == "object.transform.set":
            if "position" in input_value:
                _set_vector(target.location, input_value["position"], "position")
            if "rotation" in input_value:
                _set_vector(target.rotation_euler, input_value["rotation"], "rotation")
            if "scale" in input_value:
                _set_vector(target.scale, input_value["scale"], "scale")
            result = {"ok": True}
        elif name == "material.color.set":
            result = {"ok": True, "color": _set_color(target, input_value.get("color"))}
        elif name == "camera.transform.set":
            if "position" in input_value:
                _set_vector(target.location, input_value["position"], "position")
            if "rotation" in input_value:
                _set_vector(target.rotation_euler, input_value["rotation"], "rotation")
            if "lens" in input_value:
                target.data.lens = _number(input_value["lens"], "lens")
            result = {"ok": True}
        elif name == "render.frame":
            result = self._render_frame(target, input_value)
        else:
            raise CymonkeyError("action_not_implemented", f"allowlisted action {name!r} has no Blender handler")
        self._revision += 1
        self._publish("event:resource-changed", target_id, {"action": name})
        result["revision"] = str(self._revision)
        return result

    def _set_visibility(self, target: Any, visible: bool) -> None:
        if hasattr(target, "hide_viewport"):
            target.hide_viewport = not visible
        if hasattr(target, "hide_render"):
            target.hide_render = not visible
        if hasattr(target, "visible"):
            target.visible = visible
        if not any(hasattr(target, field) for field in ("hide_viewport", "hide_render", "visible")):
            raise CymonkeyError("invalid_target", "target does not expose visibility")

    def _render_frame(self, target: Any, input_value: dict[str, Any]) -> dict[str, Any]:
        if self.bpy is None or not hasattr(self.bpy, "ops"):
            raise CymonkeyError("renderer_unavailable", "render.frame requires Blender bpy")
        scene = target if hasattr(target, "render") else getattr(self.bpy.context, "scene", None)
        if scene is None:
            raise CymonkeyError("invalid_target", "render.frame requires a Blender scene")
        frame = input_value.get("frame")
        if frame is not None:
            scene.frame_set(int(_number(frame, "frame")))
        output_path = input_value.get("outputPath")
        if output_path is not None:
            if not isinstance(output_path, str) or not output_path.strip() or os.path.isabs(output_path) is False:
                raise CymonkeyError("invalid_input", "outputPath must be an absolute path")
            scene.render.filepath = output_path
        self.bpy.ops.render.render(write_still=bool(output_path))
        return {"ok": True, "frame": int(scene.frame_current), "outputPath": getattr(scene.render, "filepath", "")}

    def _describe_resource(self, registration: dict[str, Any]) -> dict[str, Any]:
        target = registration["target"]
        properties: dict[str, Any] = {}
        if hasattr(target, "hide_viewport"):
            properties["visible"] = not bool(target.hide_viewport)
        elif hasattr(target, "visible"):
            properties["visible"] = bool(target.visible)
        for field, source in (("position", "location"), ("rotation", "rotation_euler"), ("scale", "scale")):
            vector = _read_vector(getattr(target, source, None))
            if vector is not None:
                properties[field] = vector
        if hasattr(target, "diffuse_color"):
            properties["color"] = "#" + "".join(f"{round(float(component) * 255):02X}" for component in target.diffuse_color)
        if hasattr(target, "data") and hasattr(target.data, "lens"):
            properties["lens"] = float(target.data.lens)
        if hasattr(target, "frame_current"):
            properties["frame"] = int(target.frame_current)
        return {
            "id": registration["id"],
            "domain": DOMAIN_RENDER,
            "runtime": RUNTIME_BLENDER,
            "kind": registration["kind"],
            "label": registration["label"],
            "properties": properties,
            "actions": registration["actions"],
        }

    def _publish(self, event_type: str, source_id: str, data: dict[str, Any]) -> None:
        self._event_sequence += 1
        self._events.append({
            "id": str(self._event_sequence),
            "type": event_type,
            "domain": DOMAIN_RENDER,
            "runtime": RUNTIME_BLENDER,
            "driver": DRIVER_WEBSOCKET,
            "sourceId": source_id,
            "occurredAt": _now(),
            "data": data,
        })
        if len(self._events) > 256:
            del self._events[:-256]


def _frame(payload: bytes, opcode: int = 1) -> bytes:
    first = 0x80 | (opcode & 0x0F)
    size = len(payload)
    if size < 126:
        header = bytes((first, size))
    elif size <= 0xFFFF:
        header = bytes((first, 126)) + struct.pack("!H", size)
    else:
        header = bytes((first, 127)) + struct.pack("!Q", size)
    return header + payload


class _WebSocketConnection:
    def __init__(self, sock: socket.socket):
        self.sock = sock
        self.lock = threading.Lock()

    def send_json(self, value: dict[str, Any]) -> None:
        payload = json.dumps(value, separators=(",", ":")).encode("utf-8")
        with self.lock:
            self.sock.sendall(_frame(payload))

    def close(self) -> None:
        try:
            with self.lock:
                self.sock.sendall(_frame(b"", 8))
        except OSError:
            pass
        try:
            self.sock.close()
        except OSError:
            pass


@dataclass
class _PendingRequest:
    connection: _WebSocketConnection
    request: dict[str, Any]


class CymonkeyWebSocketHost:
    """Authenticated, single-client WebSocket host for Blender."""

    def __init__(self, registry: CymonkeyRegistry, token: str | None = None, host: str = "127.0.0.1", port: int = 9321):
        self.registry = registry
        self.token = token or os.environ.get("JANGOLOVA_CYMONKEY_TOKEN", "")
        self.host = host
        self.port = int(port)
        self._pending: queue.Queue[_PendingRequest] = queue.Queue()
        self._server: socket.socket | None = None
        self._thread: threading.Thread | None = None
        self._stopped = threading.Event()

    def start(self) -> None:
        if not self.token:
            raise ValueError("JANGOLOVA_CYMONKEY_TOKEN is required")
        self._server = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        self._server.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        self._server.bind((self.host, self.port))
        self._server.listen(1)
        self.port = int(self._server.getsockname()[1])
        self._thread = threading.Thread(target=self._accept_loop, name="cymonkey-blender-ws", daemon=True)
        self._thread.start()

    def stop(self) -> None:
        self._stopped.set()
        if self._server is not None:
            try:
                self._server.close()
            except OSError:
                pass

    def poll(self, _unused: Any = None) -> float | None:
        """Dispatch queued requests on Blender's main thread.

        Register this function with `bpy.app.timers.register` for a live host.
        """
        while True:
            try:
                pending = self._pending.get_nowait()
            except queue.Empty:
                break
            request = pending.request
            request_id = request.get("id")
            try:
                result = self.registry.dispatch(str(request.get("method", "")), request.get("params"))
                pending.connection.send_json({"id": request_id, "result": result})
            except CymonkeyError as error:
                pending.connection.send_json({"id": request_id, "error": {"code": error.code, "message": error.message}})
            except Exception as error:  # pragma: no cover - defensive runtime boundary
                pending.connection.send_json({"id": request_id, "error": {"code": "internal_error", "message": str(error)}})
        return 0.05 if not self._stopped.is_set() else None

    def _accept_loop(self) -> None:
        assert self._server is not None
        while not self._stopped.is_set():
            try:
                sock, _address = self._server.accept()
            except OSError:
                break
            threading.Thread(target=self._client_loop, args=(sock,), name="cymonkey-blender-client", daemon=True).start()

    def _client_loop(self, sock: socket.socket) -> None:
        connection = _WebSocketConnection(sock)
        try:
            headers = self._handshake(sock)
            authorized = headers.get("authorization", "") == f"Bearer {self.token}"
            connection.send_json({"type": "cymonkey.authenticated"} if authorized else {"type": "cymonkey.authorization_required"})
            while not authorized and not self._stopped.is_set():
                message = self._read_message(sock)
                if message is None:
                    return
                try:
                    auth = json.loads(message)
                except json.JSONDecodeError:
                    connection.send_json({"type": "cymonkey.authorization_required"})
                    continue
                authorized = auth.get("type") == "auth" and auth.get("token") == self.token
                connection.send_json({"type": "cymonkey.authenticated"} if authorized else {"type": "cymonkey.authorization_required"})
            if not authorized:
                return
            while not self._stopped.is_set():
                message = self._read_message(sock)
                if message is None:
                    return
                request = json.loads(message)
                if isinstance(request, dict):
                    self._pending.put(_PendingRequest(connection, request))
        except (OSError, ValueError, json.JSONDecodeError):
            return
        finally:
            connection.close()

    @staticmethod
    def _handshake(sock: socket.socket) -> dict[str, str]:
        data = b""
        while b"\r\n\r\n" not in data and len(data) < 64 * 1024:
            chunk = sock.recv(4096)
            if not chunk:
                raise OSError("WebSocket handshake closed")
            data += chunk
        lines = data.decode("latin1").split("\r\n")
        if not lines or not lines[0].startswith("GET "):
            raise ValueError("invalid WebSocket handshake")
        headers: dict[str, str] = {}
        for line in lines[1:]:
            if ":" in line:
                key, value = line.split(":", 1)
                headers[key.lower().strip()] = value.strip()
        key = headers.get("sec-websocket-key")
        if not key:
            raise ValueError("missing Sec-WebSocket-Key")
        accept = base64.b64encode(hashlib.sha1((key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11").encode()).digest()).decode()
        response = "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " + accept + "\r\n\r\n"
        sock.sendall(response.encode("latin1"))
        return headers

    @staticmethod
    def _read_message(sock: socket.socket) -> str | None:
        header = CymonkeyWebSocketHost._read_exact(sock, 2)
        if not header:
            return None
        first, second = header
        opcode = first & 0x0F
        if opcode == 8:
            return None
        masked = bool(second & 0x80)
        size = second & 0x7F
        if size == 126:
            size = struct.unpack("!H", CymonkeyWebSocketHost._read_exact(sock, 2))[0]
        elif size == 127:
            size = struct.unpack("!Q", CymonkeyWebSocketHost._read_exact(sock, 8))[0]
        if size > 4 * 1024 * 1024:
            raise ValueError("Cymonkey message exceeds 4 MiB")
        mask = CymonkeyWebSocketHost._read_exact(sock, 4) if masked else b""
        payload = bytearray(CymonkeyWebSocketHost._read_exact(sock, size))
        if masked:
            for index in range(size):
                payload[index] ^= mask[index % 4]
        return bytes(payload).decode("utf-8")

    @staticmethod
    def _read_exact(sock: socket.socket, size: int) -> bytes:
        data = b""
        while len(data) < size:
            chunk = sock.recv(size - len(data))
            if not chunk:
                raise OSError("WebSocket closed")
            data += chunk
        return data


def register_blender_timer(host: CymonkeyWebSocketHost, bpy_api: Any) -> None:
    """Start a host and attach its main-thread polling loop to Blender."""
    host.start()
    bpy_api.app.timers.register(host.poll, first_interval=0.05, persistent=True)
