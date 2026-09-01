import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

const root = new URL('../', import.meta.url);
const source = (path) => readFile(new URL(path, root), 'utf8');

test('userscripts use one bounded versioned contract', async () => {
  const [schema, augmentationSchema, docs, service, runtime] = await Promise.all([
    source('src/cymonkey/protocol/userscript/v1alpha1/userscript.schema.json'),
    source('src/cymonkey/protocol/v1alpha2/augmentation.schema.json'),
    source('docs/userscripts.md'),
    source('pkg/browser-ext/src/services/userscripts.ts'),
    source('pkg/userscript-runtime/src/validate.ts'),
  ]);
  assert.match(schema, /cymonkey\.userscript\/v1alpha1/);
  assert.match(augmentationSchema, /userscript\/v1alpha1\/userscript\.schema\.json/);
  assert.match(docs, /MVP accepts `@grant none` only/);
  assert.match(service, /authorizeUserscriptMutation/);
  assert.match(service, /approval-required/);
  assert.match(service, /userscript\.prepare/);
  assert.doesNotMatch(service, /approvedPermissionIncrease|approveMainWorld|input\.approved/);
  assert.match(service, /browser\.storage\.local/);
  assert.match(runtime, /supports @grant none only/);
  assert.doesNotMatch(service, /\beval\s*\(|new Function|Function\s*\(/);
});

test('browser builds probe native userscripts and reduce Safari permissions', async () => {
  const [config, service, background, engine, capabilities, extensionRuntime, pageBridge] = await Promise.all([
    source('pkg/browser-ext/wxt.config.ts'),
    source('pkg/browser-ext/src/services/userscripts.ts'),
    source('pkg/browser-ext/entrypoints/background.ts'),
    source('pkg/browser-ext/src/engine.ts'),
    source('pkg/browser-ext/src/capabilities.ts'),
    source('pkg/browser-ext/src/runtime.ts'),
    source('pkg/browser-ext/entrypoints/cymonkey-page.content.ts'),
  ]);
  assert.match(config, /browser === 'safari'/);
  assert.match(config, /'management', 'userScripts'/);
  assert.match(service, /await api\.getScripts\(\)/);
  assert.match(service, /sendNativeMessage\('dev\.jangolova\.Jangolova'/);
  assert.match(service, /userscripts\.catalog\.replace/);
  assert.match(background, /reconcileUserscripts/);
  assert.match(engine, /name\.startsWith\('userscript\.'\).*dispatchUserscript/s);
  assert.match(engine, /userscripts: await describeUserscriptManager\(\)/);
  assert.match(engine, /runtime\.status === 'available'/);
  assert.match(capabilities, /userscript\.install/);
  assert.match(capabilities, /userscript\.prepare/);
  assert.doesNotMatch(pageBridge, /userscript\.install/);
  assert.doesNotMatch(extensionRuntime, /method\.startsWith\('userscript\.'\)/);
  assert.doesNotMatch(extensionRuntime, /subsystems: \['cymonkey', 'render', 'userscripts'\]/);
});

test('macOS containing app imports the distinct helper and carries Safari', async () => {
  const [manifest, core, menu, handler, project, build] = await Promise.all([
    source('pkg/macos-ext/Package.swift'),
    source('pkg/macos-ext/Sources/JangolovaMacCore/ManagedRuntime.swift'),
    source('pkg/macos-ext/Safari/Jangolova/Jangolova/AppDelegate.swift'),
    source('pkg/macos-ext/Safari/Jangolova/Jangolova Extension/SafariWebExtensionHandler.swift'),
    source('pkg/macos-ext/Safari/Jangolova/Jangolova.xcodeproj/project.pbxproj'),
    source('pkg/macos-ext/scripts/build-safari.sh'),
  ]);
  assert.match(manifest, /\.package\(path: "\.\.\/macos-cymonkey-helper"\)/);
  assert.match(core, /import CymonkeyMacOSRuntime/);
  assert.match(menu, /Start Managed Cymonkey/);
  assert.match(menu, /Cymonkey for macOS/);
  assert.match(menu, /Quit Cymonkey/);
  assert.match(menu, /Safari Extension Preferences/);
  assert.match(handler, /group\.dev\.jangolova\.shared/);
  assert.match(handler, /userscripts\.catalog\.replace/);
  assert.doesNotMatch(handler, /\["(?:source|code)"\]/);
  assert.match(project, /JangolovaMacCore/);
  assert.match(build, /rsync -a --delete/);
});
