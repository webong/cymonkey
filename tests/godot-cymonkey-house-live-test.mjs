import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

const endpoint = process.env.JANGOLOVA_CYMONKEY_ENDPOINT ?? "ws://127.0.0.1:8090";
const token = process.env.JANGOLOVA_CYMONKEY_TOKEN;
if (!token) throw new Error("JANGOLOVA_CYMONKEY_TOKEN is required");
const plan = JSON.parse(await readFile(new URL("godot-cymonkey-fixture/house.scene-plan.json", import.meta.url), "utf8"));
assert.equal(plan.apiVersion, "jangolova.cymonkey.scene/v1alpha1");

const socket = new WebSocket(endpoint);
let requestId = 1;
const pending = new Map();

const reply = new Promise((resolve, reject) => {
  const timeout = setTimeout(() => reject(new Error("Cymonkey house test timed out")), 15_000);
  socket.addEventListener("open", () => socket.send(JSON.stringify({ type: "auth", token })));
  socket.addEventListener("message", (event) => {
    const message = JSON.parse(event.data);
    if (message.type === "cymonkey.authenticated") {
      clearTimeout(timeout);
      resolve();
      return;
    }
    const request = pending.get(message.id);
    if (!request) return;
    pending.delete(message.id);
    if (message.error) request.reject(new Error(`${message.error.code}: ${message.error.message}`));
    else request.resolve(message.result);
  });
  socket.addEventListener("error", () => reject(new Error("Cymonkey WebSocket failed")));
  socket.addEventListener("close", (event) => {
    if (event.code !== 1000) reject(new Error(`Cymonkey WebSocket closed: ${event.code}`));
  });
});

await reply;

function call(method, params = {}) {
  return new Promise((resolve, reject) => {
    const id = requestId++;
    pending.set(id, { resolve, reject });
    socket.send(JSON.stringify({ id, method, params }));
  });
}

function act(name, targetId, input) {
  return call("act", { name, input: { targetId, ...input } });
}

const hello = await call("hello");
assert.equal(hello.protocolVersion, "jangolova.cymonkey/v1alpha2");
assert.ok(hello.domains.includes("render"));
assert.ok(hello.runtimes.includes("godot"));
assert.ok(hello.drivers.includes("cymonkey-ws"));
const capabilities = await call("capabilities");
assert.ok(capabilities.some((capability) => capability.name === "camera.transform.set"));
assert.ok(capabilities.some((capability) => capability.name === "ui.text.set"));

const description = await call("describe");
const resourceIds = description.surfaces.map((surface) => surface.id);
for (const id of ["object:house", "object:door", "object:hero", "material:interior-light", "camera:main", "ui:status"]) {
  assert.ok(resourceIds.includes(id), `missing explicit house resource ${id}`);
}

for (const action of plan.actions) {
  const result = await act(action.name, action.targetId, action.input);
  assert.equal(result.ok, true);
}

const events = await call("events", { after: "0", limit: 20 });
assert.deepEqual(events.events.map((event) => event.sourceId), plan.actions.map((action) => action.targetId));
assert.equal((await call("health")).status, "ready");
socket.close(1000, "house test complete");
console.log(`Godot Cymonkey house choreography passed (${events.events.length} resource events).`);
