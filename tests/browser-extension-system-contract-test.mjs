import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const root = new URL("../", import.meta.url);
const source = (path) => readFile(new URL(path, root), "utf8");

test("browser-ext is the canonical WXT product", async () => {
  const pkg = JSON.parse(await source("pkg/browser-ext/package.json"));
  const config = await source("pkg/browser-ext/wxt.config.ts");
  assert.equal(pkg.name, "@jangolova/browser-extension");
  assert.match(pkg.scripts.build, /chrome.*edge.*firefox/);
  assert.doesNotMatch(JSON.stringify(pkg.scripts), /build:spoke|mode spoke/);
  assert.match(config, /Jangolova Browser Extension/);
  assert.match(config, /browser-jangolova@jangolova\.dev/);
  assert.match(config, /runtime-sandbox\.html/);
});

test("Jangolova owns extension platform services", async () => {
  const engine = await source("pkg/browser-ext/src/engine.ts");
  const runtime = await source("pkg/browser-ext/src/runtime.ts");
  for (const service of ["events", "injection", "network", "storage", "tabs", "policy", "cymonkey-engine", "userscripts"]) {
    await source(`pkg/browser-ext/src/services/${service}.ts`);
  }
  assert.match(engine, /services\/injection/);
  assert.match(engine, /services\/network/);
  assert.match(engine, /services\/storage/);
  assert.match(engine, /jangolova\.cymonkey\/v1alpha2/);
  assert.match(engine, /domains: \['viewer', 'render'\]/);
  assert.match(engine, /runtimes: \['browser-dom'\]/);
  assert.match(runtime, /cymonkey-engine\.call/);
  assert.match(runtime, /cymonkey\.call/);
  assert.doesNotMatch(runtime, /startsWith\('userscript\.'\)/);
  assert.match(engine, /dispatchUserscript/);
});

test("the extension hosts generic augmentation packages without library coupling", async () => {
  const packageManifest = await source("pkg/browser-ext/package.json");
  const capabilities = await source("pkg/browser-ext/src/capabilities.ts");
  const injection = await source("pkg/browser-ext/src/services/injection.ts");
  const runtime = await source("pkg/browser-ext/src/services/cymonkey-engine.ts");
  const policy = await source("pkg/browser-ext/src/services/policy.ts");
  const sandbox = await source("pkg/browser-ext/entrypoints/cymonkey.content.ts");

  assert.match(injection, /augmentations\/\$\{augmentationId\}\//);
  assert.match(runtime, /jangolova\.cymonkey\.augmentation-runtime/);
  assert.match(runtime, /augmentationId/);
  assert.match(policy, /method === 'cymonkey-engine\.call'/);
  assert.match(policy, /augmentationId: params\.augmentationId/);
  assert.match(capabilities, /sandbox\.mount/);
  assert.match(runtime, /delivery === 'sandbox'/);
  assert.match(sandbox, /new MessageChannel\(\)/);
  assert.match(sandbox, /sandboxPermissions\(input\.permissions\)/);
  assert.doesNotMatch(packageManifest, /threejs-cymonkey|\"three\"/);
  assert.doesNotMatch(packageManifest, /camera-kit|@snap\//);
  assert.doesNotMatch(capabilities, /threejs\./);
});

test("public page bridge remains Cymonkey-only and page-safe", async () => {
  const page = await source("pkg/browser-ext/entrypoints/cymonkey-main.ts");
  const content = await source("pkg/browser-ext/entrypoints/cymonkey.content.ts");
  assert.match(page, /root\.cymonkey/);
  assert.doesNotMatch(page, /root\.render/);
  assert.doesNotMatch(page, /chrome\.|browser\./);
  assert.match(content, /cannot invoke privileged action/);
});

test("private control plane uses one Jangolova control envelope", async () => {
  const background = await source("pkg/browser-ext/entrypoints/background.ts");
  const control = await source("pkg/browser-ext/entrypoints/control/main.ts");
  const policy = await source("pkg/browser-ext/src/services/policy.ts");
  assert.match(policy, /JANGOLOVA_EXTENSION_CALL/);
	assert.doesNotMatch(policy, /CYMONKEY_CALL/);
  assert.match(background, /acceptsExternalSender\(sender\.id\)/);
  assert.match(control, /jangolovaExtensionDispatch/);
  assert.match(control, /cymonkeyDispatch/);
});

test("popup reports the single distribution and live Xallet Spook state", async () => {
  const popup = await source("pkg/browser-ext/entrypoints/popup/main.ts");
  const runtime = await source("pkg/browser-ext/src/runtime.ts");
  assert.match(popup, /jangolova\.extension\.control/);
  assert.match(popup, /xalletSpook/);
  assert.match(runtime, /integrations: \{ xalletSpook: \{ status: xalletSpook \}, outboundControl: \{ status: outboundControl \} \}/);
});
