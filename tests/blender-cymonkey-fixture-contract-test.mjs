import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

const packageSource = await readFile(new URL("../pkg/blender/blender_cymonkey.py", import.meta.url), "utf8");
const packageReadme = await readFile(new URL("../pkg/blender/README.md", import.meta.url), "utf8");
const fixture = await readFile(new URL("blender-cymonkey-fixture/fixture.py", import.meta.url), "utf8");
const manifest = JSON.parse(await readFile(new URL("../lib/jangolova/registry/index.json", import.meta.url), "utf8"));

assert.match(packageSource, /PROTOCOL_VERSION = "cymonkey\/v1alpha1"/);
assert.match(packageSource, /RUNTIME_BLENDER = "blender"/);
assert.match(packageSource, /DRIVER_WEBSOCKET = "websocket"/);
for (const method of ["hello", "capabilities", "describe", "act", "events", "health"]) assert.match(packageSource, new RegExp(`method == "${method}"`));
for (const action of ["resource.describe", "object.visibility.set", "object.transform.set", "material.color.set", "camera.transform.set", "render.frame"]) assert.match(packageSource, new RegExp(action.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")));
assert.match(packageSource, /explicit resource registry/i);
assert.match(packageSource, /JANGOLOVA_CYMONKEY_TOKEN/);
assert.match(packageSource, /Sec-WebSocket-Key/);
assert.match(packageSource, /authorization/);
assert.doesNotMatch(packageSource, /bpy\.data\.[A-Za-z_]+\.get\(/);
assert.match(fixture, /scene:fixture/);
assert.match(fixture, /object:house/);
assert.match(fixture, /material:roof/);
assert.match(fixture, /JANGOLOVA_CAPTURE_PATH/);
assert.match(packageReadme, /render\/blender/);
assert.match(packageReadme, /--background/);
const module = manifest.modules.find((entry) => entry.id === "render/blender");
assert.equal(module?.status, "available");
assert.equal(module?.runtime, "blender");
console.log("Blender Cymonkey fixture environment contract is valid.");
