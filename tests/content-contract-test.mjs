import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

const observation = JSON.parse(await readFile(new URL("../lib/jangolova/contentkit/protocol/v1alpha1/observation.schema.json", import.meta.url), "utf8"));
const content = JSON.parse(await readFile(new URL("../lib/jangolova/contentkit/protocol/v1alpha1/content-request.schema.json", import.meta.url), "utf8"));
const providerSchema = JSON.parse(await readFile(new URL("../lib/jangolova/contentkit/protocol/v1alpha1/provider-adapter.schema.json", import.meta.url), "utf8"));
const onnx = await readFile(new URL("../pkg/content-onnx/main.go", import.meta.url), "utf8");
const ultralytics = await readFile(new URL("../pkg/content-ultralytics/python/provider.py", import.meta.url), "utf8");
const webllm = await readFile(new URL("../pkg/content-webllm/main.go", import.meta.url), "utf8");
const endpointServer = await readFile(new URL("../pkg/content-endpoints/internal/providerexec/server.go", import.meta.url), "utf8");

assert.equal(observation.properties.apiVersion.const, "content.observation/v1");
assert.ok(content.properties.parts.items.oneOf.some((part) => part.properties.kind.const === "video"));
assert.equal(providerSchema.$defs.modelInspectRequest.properties.settings.type, "object");
for (const source of [onnx, webllm]) {
  assert.match(source, /contentContract\s*=\s*"content\.provider"/);
  assert.match(source, /case "content\.provider\.observe":/);
  assert.match(source, /packagekit\.LoadDescriptor/);
  assert.doesNotMatch(source, /wirebridge|cymonkey\.content|"blockade\.(provider|start|observe|interpret|capabilities|health|close)"/);
}
assert.match(onnx, /contentkit\.ProviderAdapterAPIVersion/);
assert.match(ultralytics, /CONTRACT = "content\.provider"/);
assert.match(ultralytics, /method == "content\.provider\.observe"/);
assert.match(ultralytics, /PLUGIN_MANIFEST/);
assert.doesNotMatch(ultralytics, /cymonkey\.content|"blockade\.(provider|start|observe)"/);
assert.match(endpointServer, /"content\.provider\.interpret"/);
assert.doesNotMatch(endpointServer, /wirebridge|cymonkey\.content/);
assert.match(endpointServer, /RetryAfterMilliseconds/);
console.log("Cymonkey content contract checks passed");
