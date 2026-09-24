import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { readFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import test from "node:test";

const root = new URL("../", import.meta.url);

async function source(path) {
  return readFile(new URL(path, root), "utf8");
}

test("Cymonkey worker provides CDP, BiDi, and Playwright drivers with integrated browser automation", async () => {
  const worker = await source("scripts/cymonkey-worker.mjs");
  const syntax = spawnSync(process.execPath, ["--check", fileURLToPath(new URL("scripts/cymonkey-worker.mjs", root))], { encoding: "utf8" });
  assert.equal(syntax.status, 0, syntax.stderr);
  assert.match(worker, /puppeteer\.connect/);
  assert.match(worker, /playwright-core/);
  assert.match(worker, /webDriverBiDi/);
	for (const capability of ["window.navigate", "window.click", "window.fill", "window.press", "window.evaluate", "window.screenshot"]) {
		assert.match(worker, new RegExp(capability.replaceAll(".", "\\.")));
	}
	assert.match(worker, /cymonkey\/v1alpha1/);
	assert.doesNotMatch(worker, /jangolova\.cymonkey/);
  assert.match(worker, /first-party Cymonkey browser extension backend was retired/);
  assert.match(worker, /baseCapabilities\(targetProtocol\)/);
  assert.match(worker, /chrome-extension:\/\//);
  assert.doesNotMatch(worker, /puppeteer\.launch/);
  assert.doesNotMatch(worker, /chromium\.launch/);
});

test("one reversible live client is shared by CDP and BiDi fixtures", async () => {
  const clientPath = new URL("tests/cymonkey-live-client.mjs", root);
  const client = await source("tests/cymonkey-live-client.mjs");
  const cdpFixture = await source("tests/docker/browser-interaction-smoke-test.sh");
  const bidiFixture = await source("tests/docker/firefox-bidi-smoke-test.sh");
  const syntax = spawnSync(process.execPath, ["--check", fileURLToPath(clientPath)], { encoding: "utf8" });
  assert.equal(syntax.status, 0, syntax.stderr);
  assert.match(client, /augmentation\.install/);
  assert.match(client, /augmentation\.uninstall/);
  assert.match(client, /finally/);
  assert.match(cdpFixture, /cymonkey-live-client\.mjs[\s\S]*--expect-backend cdp/);
  assert.match(bidiFixture, /cymonkey-live-client\.mjs[\s\S]*--expect-backend bidi/);
});

test("v1alpha1 defines runtime-agnostic viewer, render, and player domains", async () => {
  const protocol = JSON.parse(await source("src/protocol/cymonkey/v1alpha2/protocol.schema.json"));
  const augmentation = JSON.parse(await source("src/protocol/cymonkey/v1alpha2/augmentation.schema.json"));
  assert.equal(protocol.$defs.hello.properties.protocolVersion.const, "cymonkey/v1alpha1");
  assert.deepEqual(protocol.$defs.domain.enum, ["viewer", "render", "player"]);
  for (const field of ["domain", "runtime", "driver"]) {
    assert.ok(protocol.$defs.capability.required.includes(field), `capability schema missing ${field}`);
  }
  assert.equal(protocol.$defs.driver.type, "string");
  assert.match("example-driver", new RegExp(protocol.$defs.driver.pattern));
  assert.equal(augmentation.properties.apiVersion.const, "cymonkey/v1alpha1");
  assert.deepEqual(augmentation.$defs.target.properties.domain.enum, ["viewer", "render", "player"]);
  assert.ok(augmentation.$defs.target.required.includes("runtime"));
	assert.deepEqual(protocol.$defs.action.dependentRequired, {domain: ["runtime"], runtime: ["domain"]});
  assert.doesNotMatch(JSON.stringify(augmentation), /applescript\.execute|raw-apple-event/);
});

test("transport mappings expose semantics without raw protocol passthrough", async () => {
  const worker = await source("scripts/cymonkey-worker.mjs");
  const safari = await source("lib/jangolova/safari_backend.go");
  for (const capability of ["augmentation.install", "script.execute", "script.register", "document.query", "document.observe", "document.patch", "network.observe", "storage.get"]) {
    assert.match(worker, new RegExp(capability.replaceAll(".", "\\.")), `worker missing ${capability}`);
  }
  assert.match(safari, /window\.evaluate/);
  assert.match(safari, /strings\.Contains\(lower, "preload"\)/);
  assert.match(safari, /network\.observe/);
  assert.doesNotMatch(worker, /cdp\.call|bidi\.call|browser\.api|chrome\.evaluate/);
});

test("CDP interception rules are augmentation-owned and cleaned up before the worker releases its connection", async () => {
  const worker = await source("scripts/cymonkey-worker.mjs");
  assert.match(worker, /existing\.augmentationId !== augmentationId/);
  assert.match(worker, /network rule \$\{id\} is not owned by augmentation/);
  assert.match(worker, /async function disconnect\(\) \{\s*await disableInterception\(\);[\s\S]*?browser = null;/);
  assert.doesNotMatch(worker, /async function disconnect\(\)[\s\S]*?browser\.disconnect\(/);
  assert.match(worker, /page\.setRequestInterception\(false\)/);
  assert.match(worker, /protocol === "cdp"/);
});
