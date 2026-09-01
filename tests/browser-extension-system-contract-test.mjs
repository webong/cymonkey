import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const root = new URL("../", import.meta.url);
const source = (path) => readFile(new URL(path, root), "utf8");

test("browser-ext is the canonical WXT product", async () => {
  const pkg = JSON.parse(await source("pkg/browser-ext/package.json"));
  const config = await source("pkg/browser-ext/wxt.config.ts");
  assert.equal(pkg.name, "@cymonkey/browser-extension");
  assert.match(pkg.scripts.build, /chrome.*edge.*firefox/);
  assert.doesNotMatch(JSON.stringify(pkg.scripts), /build:spoke|mode spoke/);
  assert.match(config, /Cymonkey Browser Extension/);
  assert.match(config, /browser-cymonkey@cymonkey\.dev/);
  assert.match(config, /runtime-sandbox\.html/);
  assert.match(config, /resources: \['runtime-sandbox\.html', 'augmentations\/\*'\]/);
});

test("Cymonkey owns extension platform services", async () => {
  const engine = await source("pkg/browser-ext/src/engine.ts");
  const runtime = await source("pkg/browser-ext/src/runtime.ts");
  for (const service of ["events", "injection", "network", "storage", "tabs", "policy", "cymonkey-engine", "userscripts"]) {
    await source(`pkg/browser-ext/src/services/${service}.ts`);
  }
  assert.match(engine, /services\/injection/);
  assert.match(engine, /services\/network/);
  assert.match(engine, /services\/storage/);
  assert.match(engine, /cymonkey\/v1alpha1/);
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
  const packages = await source("pkg/browser-ext/src/services/packages.ts");
  const approvals = await source("pkg/browser-ext/src/services/approvals.ts");
  const popup = await source("pkg/browser-ext/entrypoints/popup/main.ts");
  const tabs = await source("pkg/browser-ext/src/services/tabs.ts");
  const mediaBroker = await source("pkg/browser-ext/src/services/media-broker.ts");
  const offscreen = await source("pkg/browser-ext/entrypoints/media-broker/main.ts");
  const config = await source("pkg/browser-ext/wxt.config.ts");

  assert.match(injection, /augmentations\/\$\{augmentationId\}\//);
  assert.match(runtime, /cymonkey\.jangolova\.augmentation-runtime/);
  assert.match(runtime, /augmentationId/);
  assert.match(policy, /method === 'cymonkey-engine\.call'/);
  assert.match(policy, /augmentationId: params\.augmentationId/);
  assert.match(capabilities, /augmentation\.mount/);
  assert.match(sandbox, /cymonkey\.jangolova\.browser-package\.factories/);
  assert.match(sandbox, /mountAugmentationRuntime/);
  assert.match(runtime, /delivery === 'sandbox'/);
  assert.match(sandbox, /new MessageChannel\(\)/);
  assert.match(sandbox, /cymonkey\.jangolova\.sandbox\.connected/);
  assert.match(sandbox, /sandboxPermissions\(input\.permissions\)/);
  assert.match(packages, /browser package registry is invalid/);
  assert.match(packages, /did not declare permission/);
  assert.match(approvals, /approval\.package\.requested/);
  assert.match(approvals, /package mount was denied by the user/);
  assert.match(popup, /Allow once/);
  assert.match(popup, /approval\.resolve/);
  assert.match(tabs, /content-scripts\/cymonkey-page\.js/);
  assert.match(tabs, /content-scripts\/cymonkey\.js/);
  assert.match(tabs, /ordinary HTTP\(S\) pages/);
  assert.match(config, /\['offscreen'\]/);
  assert.match(mediaBroker, /USER_MEDIA/);
  assert.match(mediaBroker, /WEB_RTC/);
  assert.match(offscreen, /getUserMedia/);
  assert.match(offscreen, /RTCPeerConnection/);
  assert.match(sandbox, /cymonkey\.jangolova\.media-broker/);
  assert.doesNotMatch(packageManifest, /threejs-cymonkey|\"three\"/);
  assert.doesNotMatch(packageManifest, /camera-kit|@snap\//);
  assert.doesNotMatch(capabilities, /threejs\./);
});

test("public page bridge is Cymonkey-rooted and page-safe", async () => {
  const page = await source("pkg/browser-ext/entrypoints/cymonkey-page.content.ts");
  const content = await source("pkg/browser-ext/entrypoints/cymonkey.content.ts");
  assert.match(page, /window\.cymonkey/);
  assert.match(page, /root\.jangolova/);
  assert.doesNotMatch(page, /window\.jangolova/);
  assert.match(page, /world: 'MAIN'/);
  assert.doesNotMatch(page, /root\.render/);
  assert.doesNotMatch(page, /chrome\.|browser\./);
  assert.doesNotMatch(content, /injectScript/);
  assert.match(content, /cannot invoke privileged action/);
});

test("private control plane uses one Cymonkey control envelope", async () => {
  const background = await source("pkg/browser-ext/entrypoints/background.ts");
  const control = await source("pkg/browser-ext/entrypoints/control/main.ts");
  const policy = await source("pkg/browser-ext/src/services/policy.ts");
  assert.match(policy, /CYMONKEY_EXTENSION_CALL/);
	assert.doesNotMatch(policy, /CYMONKEY_CALL/);
  assert.match(background, /acceptsExternalSender\(sender\.id\)/);
  assert.match(control, /cymonkeyExtensionDispatch/);
  assert.match(control, /cymonkeyDispatch/);
});

test("popup reports standalone state and optional Xallet Spook state", async () => {
  const popup = await source("pkg/browser-ext/entrypoints/popup/main.ts");
  const runtime = await source("pkg/browser-ext/src/runtime.ts");
  assert.match(popup, /cymonkey\.extension\.control/);
  assert.match(popup, /xalletSpook/);
  assert.match(popup, /Mount on active tab/);
  assert.match(popup, /packages\.list/);
  assert.match(runtime, /integrations: \{ xalletSpook: \{ status: xalletSpook \}, outboundControl: \{ status: outboundControl \} \}/);
});
