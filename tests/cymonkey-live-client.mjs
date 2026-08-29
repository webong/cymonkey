#!/usr/bin/env node
import assert from "node:assert/strict";
import process from "node:process";

const provider = requiredArgument("--provider").replace(/\/$/, "");
const instance = requiredArgument("--instance");
const expectedBackend = requiredArgument("--expect-backend");
const token = requiredEnvironment("JANGOLOVA_PROVIDER_TOKEN");
const augmentationId = `live-${expectedBackend}-${process.pid}`;
const overlayId = `${augmentationId}-overlay`;
const ruleId = 700000 + (process.pid % 100000);
let installed = false;
let overlayMounted = false;

try {
  const hello = await call("hello", {});
  assert.equal(hello.protocolVersion, "jangolova.cymonkey/v1alpha2");
  assert.ok(hello.domains.includes("computer"), JSON.stringify(hello));
  assert.ok(hello.runtimes.includes("browser-dom"), JSON.stringify(hello));
  assert.ok(hello.drivers.includes(expectedBackend), JSON.stringify(hello));

  const capabilities = await call("capabilities", {});
  const byName = new Map(capabilities.map((value) => [value.name, value]));
  for (const name of [
    "augmentation.install", "augmentation.list", "augmentation.disable",
    "augmentation.enable", "augmentation.uninstall", "dom.query",
    "overlay.mount", "overlay.unmount", "storage.get", "storage.set",
  ]) {
    assert.ok(byName.has(name), `${expectedBackend} did not advertise ${name}`);
  }
  for (const capability of capabilities) {
    for (const field of ["domain", "runtime", "driver", "support", "lifetime", "persistence", "effect", "inputSchema"]) {
      assert.ok(capability[field], `${capability.name} omitted ${field}`);
    }
  }

  const manifest = {
    apiVersion: "jangolova.cymonkey/v1alpha2",
    kind: "Augmentation",
    metadata: { id: augmentationId, revision: "live-conformance-1" },
    spec: {
	  targets: [{ domain: "computer", runtime: "browser-dom", match: { urlPatterns: ["file://*/*"] } }],
      permissions: ["script.register", "style.insert"],
      computer: {
        scripts: [{ id: "preload", source: "globalThis.__cymonkeyLivePreload = true;", world: "ISOLATED", runAt: "document_start" }],
        styles: [{ id: "fixture-style", css: ":root { --cymonkey-live: 1; }" }],
      },
    },
  };
  const installedResult = await act("augmentation.install", { manifest });
  installed = true;
  assert.equal(installedResult.id, augmentationId);
  assert.equal(installedResult.enabled, true);

  const listed = await act("augmentation.list", {});
  assert.ok(listed.augmentations.some((value) => value.id === augmentationId));

  const queried = await act("dom.query", { selector: "title", limit: 1 });
  assert.equal(queried.matches[0]?.text, "Jangolova Fixture");

  await act("overlay.mount", { id: overlayId, html: "<strong>Cymonkey live</strong>" });
  overlayMounted = true;
  const overlay = await act("dom.query", { selector: `[data-jangolova-cymonkey-overlay="${overlayId}"]`, limit: 1 });
  assert.equal(overlay.matches.length, 1);
  await act("overlay.unmount", { id: overlayId });
  overlayMounted = false;

  await act("storage.set", { augmentationId, values: { proof: expectedBackend } });
  const stored = await act("storage.get", { augmentationId, keys: ["proof"] });
  assert.equal(stored.values.proof, expectedBackend);

  if (byName.has("network.rules.install") && byName.get("network.rules.install").driver === expectedBackend) {
    await act("network.rules.install", {
      augmentationId,
      rules: [{ id: ruleId, priority: 1, action: { type: "block" }, condition: { urlFilter: "cymonkey-live.invalid/never" } }],
    });
    await act("network.rules.remove", { augmentationId, ruleIds: [ruleId] });
  }

  assert.equal((await act("augmentation.disable", { augmentationId })).enabled, false);
  assert.equal((await act("augmentation.enable", { augmentationId })).enabled, true);
  await act("augmentation.uninstall", { augmentationId });
  installed = false;

  const batch = await call("events", { limit: 100 });
  assert.ok(batch.cursor);
  assert.ok(batch.events.some((event) => event.type === "cymonkey.action"));
  process.stdout.write(`Cymonkey ${expectedBackend} live conformance passed\n`);
} finally {
  if (overlayMounted) await act("overlay.unmount", { id: overlayId }).catch(() => {});
  if (installed) await act("augmentation.uninstall", { augmentationId }).catch(() => {});
}

async function act(name, input) {
  return call("act", { name, input });
}

async function call(method, params) {
  const response = await fetch(`${provider}/v1/instances/${encodeURIComponent(instance)}/call`, {
    method: "POST",
    headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" },
    body: JSON.stringify({ method, params }),
  });
  const body = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(`${method} returned HTTP ${response.status}: ${JSON.stringify(body)}`);
  if (body.error) throw new Error(`${method} failed: ${JSON.stringify(body.error)}`);
  return body.result;
}

function requiredArgument(name) {
  const index = process.argv.indexOf(name);
  const value = index >= 0 ? process.argv[index + 1] : "";
  if (!value) throw new Error(`${name} is required`);
  return value;
}

function requiredEnvironment(name) {
  const value = process.env[name];
  if (!value) throw new Error(`${name} is required`);
  return value;
}
