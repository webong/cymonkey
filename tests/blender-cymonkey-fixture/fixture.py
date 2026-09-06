"""Headless Blender Cymonkey fixture.

The script is intentionally usable with Blender's Python interpreter. It also
contains a tiny fake-object mode so the contract can be exercised without
installing Blender on the development machine.
"""

from __future__ import annotations

import json
import os
import sys
from pathlib import Path

PACKAGE_ROOT = Path(os.environ.get("JANGOLOVA_BLENDER_MODULE_DIR", str(Path(__file__).resolve().parents[2] / "pkg" / "blender")))
if str(PACKAGE_ROOT) not in sys.path:
    sys.path.insert(0, str(PACKAGE_ROOT))

from blender_cymonkey import CymonkeyRegistry  # noqa: E402

try:
    import bpy  # type: ignore
except ImportError:  # Running the source-only contract outside Blender.
    bpy = None


class Vector:
    def __init__(self, x=0.0, y=0.0, z=0.0):
        self.x, self.y, self.z = x, y, z


class FakeObject:
    def __init__(self, name):
        self.name = name
        self.hide_viewport = False
        self.hide_render = False
        self.location = Vector()
        self.rotation_euler = Vector()
        self.scale = Vector(1.0, 1.0, 1.0)


class FakeMaterial:
    def __init__(self, name):
        self.name = name
        self.diffuse_color = (0.7, 0.7, 0.7, 1.0)


def build_registry():
    if bpy is not None:
        return build_blender_registry()
    registry = CymonkeyRegistry()
    house = FakeObject("House")
    roof = FakeObject("Roof")
    material = FakeMaterial("RoofMaterial")
    registry.register(id="scene:fixture", kind="scene", target=house, actions=["resource.describe", "render.frame"], label="Fixture scene")
    registry.register(id="object:house", kind="object", target=house, actions=["resource.describe", "object.visibility.set", "object.transform.set"], label="House")
    registry.register(id="object:roof", kind="object", target=roof, actions=["resource.describe", "object.transform.set"], label="Roof")
    registry.register(id="material:roof", kind="material", target=material, actions=["resource.describe", "material.color.set"], label="Roof material")
    return registry


def build_blender_registry():
    """Create a small real Blender scene when launched by Blender itself."""
    import math
    from mathutils import Vector as BlenderVector

    scene = bpy.context.scene
    scene.render.engine = "BLENDER_WORKBENCH"
    scene.render.resolution_x = 256
    scene.render.resolution_y = 256
    scene.render.resolution_percentage = 100

    bpy.ops.mesh.primitive_cube_add(location=(0, 0, 0))
    house = bpy.context.object
    house.name = "House"
    house.scale = (1.5, 1.5, 1.0)
    bpy.ops.object.transform_apply(location=False, rotation=False, scale=True)

    bpy.ops.mesh.primitive_cone_add(vertices=4, radius1=2.2, radius2=0, depth=1.5, location=(0, 0, 1.8), rotation=(0, 0, math.radians(45)))
    roof = bpy.context.object
    roof.name = "Roof"

    material = bpy.data.materials.new("RoofMaterial")
    material.diffuse_color = (0.7, 0.15, 0.1, 1.0)
    roof.data.materials.append(material)

    bpy.ops.object.camera_add(location=(6, -6, 5))
    camera = bpy.context.object
    camera.name = "CameraMain"
    camera.rotation_euler = (BlenderVector((0, 0, 0)) - camera.location).to_track_quat("-Z", "Y").to_euler()
    scene.camera = camera

    bpy.ops.object.light_add(type="AREA", location=(4, -3, 6))
    bpy.context.object.data.energy = 900

    registry = CymonkeyRegistry(bpy)
    registry.register(id="scene:fixture", kind="scene", target=scene, actions=["resource.describe", "render.frame"], label="Fixture scene")
    registry.register(id="object:house", kind="object", target=house, actions=["resource.describe", "object.visibility.set", "object.transform.set"], label="House")
    registry.register(id="object:roof", kind="object", target=roof, actions=["resource.describe", "object.transform.set"], label="Roof")
    registry.register(id="material:roof", kind="material", target=material, actions=["resource.describe", "material.color.set"], label="Roof material")
    registry.register(id="camera:main", kind="camera", target=camera, actions=["resource.describe", "camera.transform.set"], label="Main camera")
    return registry


def run_contract(registry):
    assert registry.hello()["protocolVersion"] == "cymonkey/v1alpha1"
    assert registry.hello()["runtimes"] == ["blender"]
    assert registry.hello()["domains"] == ["render"]
    described = registry.dispatch("describe")
    expected = {"scene:fixture", "object:house", "object:roof", "material:roof"}
    if registry.bpy is not None:
        expected.add("camera:main")
    assert {surface["id"] for surface in described["surfaces"]} == expected
    registry.dispatch("health")
    registry.dispatch("act", {"name": "object.visibility.set", "input": {"targetId": "object:house", "visible": False}})
    registry.dispatch("act", {"name": "object.transform.set", "input": {"targetId": "object:house", "position": {"x": 1, "y": 2, "z": 3}}})
    registry.dispatch("act", {"name": "material.color.set", "input": {"targetId": "material:roof", "color": "#D95F59"}})
    if registry.bpy is not None:
        output_path = os.environ.get("JANGOLOVA_BLENDER_RENDER_PATH", "/tmp/jangolova-blender-fixture.png")
        registry.dispatch("act", {"name": "render.frame", "input": {"targetId": "scene:fixture", "outputPath": output_path}})
    events = registry.dispatch("events", {"after": "0"})
    assert any(event["type"] == "event:resource-changed" for event in events["events"])
    return {"ok": True, "revision": described["revision"], "eventCursor": events["cursor"]}


if __name__ == "__main__":
    result = run_contract(build_registry())
    capture_path = os.environ.get("JANGOLOVA_CAPTURE_PATH")
    if capture_path:
        Path(capture_path).write_text(json.dumps(result, indent=2) + "\n", encoding="utf-8")
    print("Blender Cymonkey headless fixture passed.")
