import type { CymonkeyRequest } from './index.js';

type Context = {augmentationId: string; configuration: Record<string, unknown>};
type SandboxPackage = {id: string; connect(context: Context, port: MessagePort): Promise<void>};

declare global {
  interface Window { jangolovaSandboxPackage?: SandboxPackage }
}

if (window.jangolovaSandboxPackage) throw new Error('a Jangolova sandbox package is already installed');

window.jangolovaSandboxPackage = {
  id: 'snapchat-camera-kit',
  async connect(context, port) {
    const apiToken = string(context.configuration.apiToken, 'Camera Kit apiToken');
    // Publish the lightweight package registration before evaluating Camera
    // Kit. The SDK may fail to initialize in a particular browser, but that is
    // a connection error—not a missing-package error.
    const {CameraKitCymonkey} = await import('./index.js');
    const runtime = new CameraKitCymonkey({
      augmentationId: context.augmentationId,
      apiToken,
      title: typeof context.configuration.title === 'string' ? context.configuration.title : undefined,
    });
    port.onmessage = async (event) => {
      const value = event.data;
      if (!record(value) || value.channel !== 'jangolova.cymonkey.sandbox.request' || typeof value.id !== 'string') return;
      const request = record(value.request) ? value.request as CymonkeyRequest : {method: ''};
      try {
        port.postMessage({channel: 'jangolova.cymonkey.sandbox.response', id: value.id, result: await runtime.dispatch(request)});
      } catch (error) {
        port.postMessage({channel: 'jangolova.cymonkey.sandbox.response', id: value.id, error: errorMessage(error)});
      }
    };
    port.start();
  },
};

function record(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function string(value: unknown, name: string) {
  if (typeof value !== 'string' || !value.trim()) throw new Error(`${name} is required`);
  return value;
}

function errorMessage(error: unknown) { return error instanceof Error ? error.message : String(error); }
