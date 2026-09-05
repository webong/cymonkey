import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

const manifest = JSON.parse(await readFile(new URL("../src/jangolova/registry/index.json", import.meta.url), "utf8"));
assert.equal(manifest.schemaVersion, "jangolova.registry/v1alpha1");
assert.equal(manifest.registryId, "jangolova-reference");

const modules = new Map(manifest.modules.map((module) => [module.id, module]));
for (const id of ["render/browser-dom", "render/threejs", "render/godot", "render/unity", "render/unreal", "render/snapchat-camera-kit"]) {
  assert.ok(modules.has(id), `missing implemented Jangolova module ${id}`);
  assert.equal(modules.get(id).status, "available");
}
assert.equal(modules.get("render/blender")?.status, "available");
for (const module of manifest.modules.filter((entry) => entry.status === "available")) {
  assert.ok(module.version, `${module.id} needs a version`);
  assert.ok(module.protocolVersion, `${module.id} needs a protocol version`);
  assert.ok(Array.isArray(module.platforms), `${module.id} needs platform metadata`);
}
console.log("Jangolova module registry snapshot is valid.");
