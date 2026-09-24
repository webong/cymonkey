import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

const root = new URL(
  "../pkg/unity/com.jangolova.bridge/",
  import.meta.url,
);
const manifest = JSON.parse(await readFile(new URL("package.json", root)));

assert.equal(manifest.name, "com.jangolova.bridge");
assert.match(manifest.version, /^0\.\d+\.\d+$/);
assert.equal(manifest.unity, "2022.3");
assert.equal(
  manifest.dependencies["com.unity.nuget.newtonsoft-json"],
  "3.2.2",
);

const protocol = await readFile(
  new URL("Runtime/BridgeProtocol.cs", root),
  "utf8",
);
const client = await readFile(
  new URL("Runtime/JangolovaBridgeClient.cs", root),
  "utf8",
);
const scene = await readFile(
  new URL("Runtime/JangolovaSceneBridge.cs", root),
  "utf8",
);
const goProtocol = await readFile(
  new URL("../src/internal/bridge/protocol.go", import.meta.url),
  "utf8",
);

assert.match(protocol, /jangolova\.bridge\/v1alpha1/);
assert.match(goProtocol, /jangolova\.bridge\/v1alpha1/);
const unityProtocol = protocol.match(
  /Version = "([^"]+)"/,
)?.[1];
const nativeProtocol = goProtocol.match(
  /ProtocolVersion = "([^"]+)"/,
)?.[1];
assert.equal(unityProtocol, nativeProtocol);
for (const variable of [
  "JANGOLOVA_BRIDGE_URL",
  "JANGOLOVA_BRIDGE_TOKEN",
  "JANGOLOVA_BRIDGE_PROTOCOL",
]) {
  assert.ok(protocol.includes(variable), `missing ${variable}`);
}
assert.match(client, /ClientWebSocket/);
assert.match(client, /Authorization/);
assert.match(client, /IPAddress\.IsLoopback/);
assert.match(client, /SynchronizationContext/);
for (const method of [
  '"hello"',
  '"capabilities"',
  '"describe"',
  '"act"',
  '"events"',
]) {
  assert.ok(client.includes(method), `missing bridge method ${method}`);
}
for (const capability of [
  "scene.describe",
  "object.create",
  "object.update",
  "object.remove",
  "camera.update",
]) {
  assert.ok(scene.includes(capability), `missing capability ${capability}`);
}

console.log("Unity package contract is valid.");
