import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

const manifest = JSON.parse(await readFile(new URL("../integrations/jangolova/cymonkey/fixtures/engine-runtimes.json", import.meta.url), "utf8"));
assert.equal(manifest.protocolVersion, "cymonkey/v1alpha1");
assert.deepEqual(manifest.runtimes.map((runtime) => runtime.engine), ["godot", "unity", "unreal"]);
for (const runtime of manifest.runtimes) {
  for (const field of ["engine", "version", "image", "pluginVersion", "protocolVersion", "supportedActions", "supportedPlatforms", "requirements"]) {
    assert.ok(runtime[field] !== undefined, `${runtime.engine} runtime missing ${field}`);
  }
  assert.equal(runtime.protocolVersion, manifest.protocolVersion);
  assert.ok(runtime.supportedActions.length > 0);
  assert.ok(runtime.supportedPlatforms.length > 0);
  assert.ok(runtime.requirements.headless);
}
console.log("Cymonkey engine runtime manifest is valid.");
