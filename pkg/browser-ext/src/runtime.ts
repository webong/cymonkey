import { privilegedCapabilityNames } from './capabilities';
import { dispatchCymonkey } from './engine';
import { readEvents } from './services/events';
import { callCymonkeyEngine } from './services/cymonkey-engine';
import { isRecord } from './types';

let xalletSpook: 'discovering' | 'unavailable' | 'connected' = 'discovering';
let outboundControl: 'disabled' | 'connecting' | 'authenticating' | 'connected' | 'unavailable' = 'disabled';

export function setXalletSpookStatus(status: typeof xalletSpook) {
  xalletSpook = status;
}

export function setOutboundControlStatus(status: typeof outboundControl) {
  outboundControl = status;
}

export async function dispatchJangolova(method: string, params: Record<string, unknown> = {}) {
  if (method === 'hello') return hello();
  if (method === 'capabilities') return capabilities();
  if (method === 'describe') return describe();
  if (method === 'events') return readEvents(params);
  if (method === 'cymonkey.call') {
    return dispatchCymonkey(String(params.method || ''), isRecord(params.params) ? params.params : {});
  }
  if (method === 'cymonkey-engine.call') return callCymonkeyEngine(params.request, params.target);
  throw new Error(`unsupported Jangolova extension method ${JSON.stringify(method)}`);
}

function hello() {
  return {
    protocolVersion: 'jangolova.browser-extension/v1alpha1',
    implementation: { name: 'jangolova-browser-extension', version: browser.runtime.getManifest().version },
    subsystems: ['cymonkey', 'cymonkey-engine'],
    backend: 'webextension',
  };
}

function capabilities() {
  return {
    platform: [
      'events.read', 'audit.events', 'injection.packaged', 'network.rules',
      'storage.scoped', 'policy.fine-grained', 'control.websocket.outbound',
    ],
    cymonkey: privilegedCapabilityNames,
    cymonkeyEngine: ['cymonkey-engine.call'],
  };
}

async function describe() {
  return {
    product: 'Jangolova Browser Extension',
    extensionId: browser.runtime.id,
    distribution: 'single-build',
    subsystems: { cymonkey: true, cymonkeyEngine: true },
    integrations: { xalletSpook: { status: xalletSpook }, outboundControl: { status: outboundControl } },
  };
}
