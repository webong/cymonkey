import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

const root = new URL('../pkg/macos-cymonkey-helper/', import.meta.url);
const source = async (path) => readFile(new URL(path, root), 'utf8');

test('macOS helper is caller-owned and consumes Cymonkey v1alpha1', async () => {
  const [readme, models, main, client, signing] = await Promise.all([
    source('README.md'),
    source('Sources/CymonkeyMacOSRuntime/Models.swift'),
    source('Sources/CymonkeyMacOSHelper/main.swift'),
    source('Sources/CymonkeyMacOSRuntime/ControlClient.swift'),
    source('scripts/build-and-sign.sh'),
  ]);
  assert.match(readme, /Jangolova never launches or terminates it/);
  assert.match(models, /cymonkey\/v1alpha1/);
  assert.match(main, /JANGOLOVA_CYMONKEY_CONTROL_URL/);
  assert.match(main, /JANGOLOVA_CYMONKEY_CONTROL_TOKEN/);
  assert.match(client, /Authorization/);
  assert.match(client, /plaintext Cymonkey control endpoints must use loopback/);
  assert.match(signing, /CODESIGN_IDENTITY/);
  assert.match(signing, /ad-hoc signing is not accepted/);
  assert.match(signing, /codesign --verify --strict/);
});

test('native mappings are bounded and expose no raw scripting action', async () => {
  const [runtime, events, accessibility] = await Promise.all([
    source('Sources/CymonkeyMacOSRuntime/Runtime.swift'),
    source('Sources/CymonkeyMacOSRuntime/AppleEvents.swift'),
    source('Sources/CymonkeyMacOSRuntime/Accessibility.swift'),
  ]);
  for (const capability of ['app.command.invoke', 'ui.query', 'ui.action.invoke', 'ui.attribute.set']) {
    assert.match(runtime, new RegExp(capability.replaceAll('.', '\\.')));
  }
  assert.doesNotMatch(runtime, /applescript\.execute|raw-apple-event/);
  assert.match(events, /AppleEventCommand/);
  assert.match(events, /fourCharacterCode/);
  assert.match(accessibility, /AXIsProcessTrusted/);
  assert.match(accessibility, /maxDepth/);
  assert.match(accessibility, /maxResults/);
  assert.match(accessibility, /staleReference/);
});
