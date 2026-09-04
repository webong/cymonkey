import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

const manifest = JSON.parse(await readFile(new URL("../pkg/jangolova-render-modules.json", import.meta.url), "utf8"));
assert.equal(manifest.owner, "jangolova");
assert.equal(manifest.coordinator, "cymonkey");
assert.equal(manifest.protocolVersion, "cymonkey/v1alpha1");

const modules = new Map(manifest.modules.map((module) => [module.id, module]));
for (const id of ["render/browser-dom", "render/threejs", "render/godot", "render/unity", "render/unreal", "render/snapchat-camera-kit"]) {
  assert.ok(modules.has(id), `missing implemented Jangolova module ${id}`);
  assert.equal(modules.get(id).status, "implemented");
}
assert.equal(modules.get("render/blender")?.status, "implemented");
for (const module of manifest.modules.filter((entry) => entry.status === "implemented")) {
  assert.ok(module.distributionPath, `${module.id} needs a distribution path`);
}
console.log("Jangolova render module ownership manifest is valid.");
