"""Real Blender only. The caller owns this process; the bridge only detaches."""
import bpy
import json
import os
import sys
import time
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from fixture import build_blender_registry
from blender_cymonkey import CymonkeyWebSocketHost

bpy.ops.object.select_all(action="SELECT")
bpy.ops.object.delete(use_global=False)
registry = build_blender_registry()
scene = bpy.context.scene
scene.render.engine = "CYCLES"
scene.cycles.device = "CPU"
scene.cycles.samples = 24
scene.cycles.use_denoising = False
scene.render.resolution_x = 384
scene.render.resolution_y = 384
scene.render.image_settings.file_format = "PNG"
scene.world.color = (0.15, 0.15, 0.15)

# This floor belongs to the fixture and is deliberately not registered.
bpy.ops.mesh.primitive_plane_add(size=200, location=(0, 0, -1.01))
floor = bpy.context.object
floor.name = "UnregisteredFloor"

host = CymonkeyWebSocketHost(registry, host="0.0.0.0", port=9321)
host.start()
Path("/workspace/artifacts/runtime.json").write_text(json.dumps({
    "engine": "blender", "version": bpy.app.version_string,
    "renderer": scene.render.engine, "device": scene.cycles.device,
    "realEngine": True,
}) + "\n")
print("Jangolova real Blender fixture ready", flush=True)
try:
    while True:
        host.poll()  # bpy operations execute on this main thread.
        time.sleep(0.01)
finally:
    host.stop()
