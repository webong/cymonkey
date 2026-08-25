import assert from "node:assert/strict";
import test from "node:test";
import { CYMONKEY_PROTOCOL_VERSION, CYMONKEY_RUNTIME_SYMBOL, ThreeJSCymonkey } from "../pkg/threejs-pacman/dist/index.js";

test("Three.js Cymonkey exposes only explicitly registered resources", async () => {
  const runtime = new ThreeJSCymonkey();
  const hidden = { name: "hidden", visible: true };
  const hero = { name: "hero", visible: true, position: vector(), rotation: vector(), scale: vector(1, 1, 1) };
  runtime.register({ id: "object:hero", kind: "object", target: hero, actions: ["object.visibility.set", "object.transform.set"] });
  const description = runtime.describe();
  assert.deepEqual(description.surfaces.map((surface) => surface.id), ["object:hero"]);
  assert.equal(description.surfaces.some((surface) => surface.properties?.name === hidden.name), false);
  const hello = runtime.hello();
  assert.equal(hello.protocolVersion, CYMONKEY_PROTOCOL_VERSION);
  assert.ok(hello.profiles.includes("engine"));
  assert.deepEqual(hello.backends, ["engine-threejs"]);
});

test("Three.js Cymonkey enforces target and action allowlists", async () => {
  const runtime = new ThreeJSCymonkey();
  const hero = { visible: true, position: vector(), rotation: vector(), scale: vector(1, 1, 1) };
  runtime.register({ id: "object:hero", kind: "object", target: hero, actions: ["object.visibility.set"] });
  const accepted = await runtime.dispatch({ id: 1, method: "act", params: { name: "object.visibility.set", input: { targetId: "object:hero", visible: false } } });
  assert.equal(accepted.error, undefined);
  assert.equal(hero.visible, false);
  const denied = await runtime.dispatch({ id: 2, method: "act", params: { name: "object.transform.set", input: { targetId: "object:hero" } } });
  assert.equal(denied.error.code, "action_not_allowlisted");
  const stale = await runtime.dispatch({ id: 3, method: "act", params: { name: "object.visibility.set", input: { targetId: "object:hero", visible: true, expectedRevision: "0" } } });
  assert.equal(stale.error.code, "stale_revision");
});

test("Three.js Cymonkey installs a private symbol runtime", () => {
  const runtime = new ThreeJSCymonkey();
  const target = {};
  const uninstall = runtime.installGlobal(target);
  assert.equal(target[CYMONKEY_RUNTIME_SYMBOL], runtime);
  assert.equal(Object.keys(target).length, 0);
  uninstall();
  assert.equal(target[CYMONKEY_RUNTIME_SYMBOL], undefined);
});

function vector(x = 0, y = 0, z = 0) {
  return { x, y, z, set(nextX, nextY, nextZ) { this.x = nextX; this.y = nextY; this.z = nextZ; } };
}
