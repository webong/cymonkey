import {installEphemeralWebStorage} from '../../src/sandbox-storage.js';

type SandboxPackage = {
  id: string;
  connect(context: {augmentationId: string; configuration: Record<string, unknown>}, port: MessagePort): void | Promise<void>;
};

declare global {
  interface Window { jangolovaSandboxPackage?: SandboxPackage }
}

installEphemeralWebStorage(window);

const params = new URL(location.href).searchParams;
const packageID = identifier(params.get('package'), 'package');
const sandboxID = identifier(params.get('id'), 'id');
const nonce = identifier(params.get('nonce'), 'nonce');

void loadPackage().then(
  () => window.parent.postMessage({channel: 'jangolova.cymonkey.sandbox.ready', id: sandboxID, nonce, status: 'ready'}, '*'),
  (error) => window.parent.postMessage({channel: 'jangolova.cymonkey.sandbox.ready', id: sandboxID, nonce, status: 'failed', error: message(error)}, '*'),
);

window.addEventListener('message', (event) => {
  if (event.source !== window.parent || !event.data || event.data.channel !== 'jangolova.cymonkey.sandbox.connect') return;
  if (event.data.nonce !== nonce || event.ports.length !== 1) return;
  const context = event.data.context;
  const [port] = event.ports;
  if (!port || !record(context) || context.augmentationId !== undefined && typeof context.augmentationId !== 'string') return;
  const registration = window.jangolovaSandboxPackage;
  if (!registration) return reportConnection('failed', 'sandbox package is not registered');
  void Promise.resolve(registration.connect({
      augmentationId: String(context.augmentationId || ''),
      configuration: record(context.configuration) ? context.configuration : {},
    }, port)).then(
      () => reportConnection('connected'),
      (error) => reportConnection('failed', message(error)),
    );
});

function reportConnection(status: 'connected' | 'failed', error?: string) {
  window.parent.postMessage({channel: 'jangolova.cymonkey.sandbox.connected', id: sandboxID, nonce, status, ...(error ? {error} : {})}, '*');
}

async function loadPackage() {
  await new Promise<void>((resolve, reject) => {
    const script = document.createElement('script');
    script.type = 'module';
    script.src = `augmentations/${packageID}/sandbox.js`;
    script.onload = () => resolve();
    script.onerror = () => reject(new Error(`could not load sandbox package ${JSON.stringify(packageID)}`));
    document.head.append(script);
  });
  if (!window.jangolovaSandboxPackage || window.jangolovaSandboxPackage.id !== packageID) {
    throw new Error(`sandbox package ${JSON.stringify(packageID)} did not register`);
  }
}

function identifier(value: string | null, name: string) {
  if (!value || !/^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/.test(value)) throw new Error(`sandbox ${name} is invalid`);
  return value;
}

function record(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function message(error: unknown) { return error instanceof Error ? error.message : String(error); }
