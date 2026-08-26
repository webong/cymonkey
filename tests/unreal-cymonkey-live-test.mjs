import assert from "node:assert/strict";

const endpoint = process.env.JANGOLOVA_CYMONKEY_ENDPOINT ?? "ws://127.0.0.1:8090";
const token = process.env.JANGOLOVA_CYMONKEY_TOKEN;
if (!token) throw new Error("JANGOLOVA_CYMONKEY_TOKEN is required");

const socket = new WebSocket(endpoint);
let nextId = 1;
const pending = new Map();
let authenticated = false;
const ready = new Promise((resolve, reject) => {
  const timeout = setTimeout(() => reject(new Error("Unreal Cymonkey live test timed out")), 20_000);
  socket.addEventListener("open", () => socket.send(JSON.stringify({ type: "auth", token })));
  socket.addEventListener("message", (event) => {
    const message = JSON.parse(event.data);
    if (message.type === "cymonkey.authenticated") {
      authenticated = true;
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
  socket.addEventListener("error", () => reject(new Error("Unreal Cymonkey WebSocket failed")));
});

await ready;
assert.equal(authenticated, true);
function call(method, params = {}) {
  return new Promise((resolve, reject) => {
    const id = nextId++;
    pending.set(id, { resolve, reject });
    socket.send(JSON.stringify({ id, method, params }));
  });
}

const hello = await call("hello");
assert.equal(hello.implementation.engine, "unreal");
assert.equal(hello.protocolVersion, "jangolova.pacman/v1alpha1");
const capabilities = await call("capabilities");
assert.ok(capabilities.some((capability) => capability.name === "object.visibility.set"));
const description = await call("describe");
assert.ok(description.resources.some((resource) => resource.id === "object:fixture"));
assert.equal((await call("health")).status, "ready");
const action = await call("act", { name: "object.visibility.set", targetId: "object:fixture", input: { visible: false } });
assert.equal(action.ok, true);
const events = await call("events", { after: "0", limit: 10 });
assert.ok(events.events.some((event) => event.sourceId === "object:fixture"));
socket.close(1000, "live test complete");
console.log("Unreal Cymonkey live WebSocket conformance passed.");
