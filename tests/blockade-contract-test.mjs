import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

const observation = JSON.parse(await readFile(new URL("../lib/blockade/protocol/v1alpha1/observation.schema.json", import.meta.url), "utf8"));
const content = JSON.parse(await readFile(new URL("../lib/blockade/protocol/v1alpha1/content-request.schema.json", import.meta.url), "utf8"));
const providerSchema = JSON.parse(await readFile(new URL("../lib/blockade/protocol/v1alpha1/provider-adapter.schema.json", import.meta.url), "utf8"));
const onnx = await readFile(new URL("../providers/blockade-onnx/main.go", import.meta.url), "utf8");
const ultralytics = await readFile(new URL("../providers/blockade-ultralytics/python/provider.py", import.meta.url), "utf8");
const webllm = await readFile(new URL("../providers/blockade-webllm/main.go", import.meta.url), "utf8");
const endpointServer = await readFile(new URL("../providers/blockade-endpoints/internal/providerexec/server.go", import.meta.url), "utf8");

assert.equal(observation.properties.apiVersion.const, "blockade.observation/v1alpha1");
assert.ok(content.properties.parts.items.oneOf.some((part) => part.properties.kind.const === "video"));
assert.equal(providerSchema.$defs.modelInspectRequest.properties.settings.type, "object");
for (const source of [onnx, webllm]) {
  assert.match(source, /blockadeContract\s*=\s*"blockade\.provider"/);
  assert.match(source, /providerplugin\.LoadManifest/);
}
assert.match(onnx, /blockade\.model\.inspect/);
assert.match(ultralytics, /blockade\.model\.inspect/);
assert.match(ultralytics, /BLOCKADE_PLUGIN_MANIFEST/);
assert.match(endpointServer, /blockade\.interpret/);
assert.match(endpointServer, /retryAfterMilliseconds/);
console.log("Blockade contract checks passed");
