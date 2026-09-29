import type { CymonkeyRequest } from './index.js';

type Context = {augmentationId: string; configuration: Record<string, unknown>};
type SandboxPackage = {id: string; connect(context: Context, port: MessagePort): Promise<void>};
type PendingMediaRequest = {resolve(value: Record<string, unknown>): void; reject(error: Error): void; timer: number};

declare global {
  interface Window { cymonkeySandboxPackage?: SandboxPackage }
}

if (window.cymonkeySandboxPackage) throw new Error('a Cymonkey sandbox package is already installed');

window.cymonkeySandboxPackage = {
  id: 'snapchat-camera-kit',
  async connect(context, port) {
    const apiToken = string(context.configuration.apiToken, 'Camera Kit apiToken');
    // Publish the lightweight package registration before evaluating Camera
    // Kit. The SDK may fail to initialize in a particular browser, but that is
    // a connection error—not a missing-package error.
    const {CameraKitCymonkey} = await import('./index.js');
    const media = createCameraMediaProvider(port);
    const runtime = new CameraKitCymonkey({
      augmentationId: context.augmentationId,
      apiToken,
      title: typeof context.configuration.title === 'string' ? context.configuration.title : undefined,
      mediaProvider: media.provider,
    });
    port.onmessage = async (event) => {
      const value = event.data;
      if (record(value) && value.channel === 'cymonkey.jangolova.media.response') {
        media.receive(value);
        return;
      }
      if (!record(value) || value.channel !== 'cymonkey.jangolova.sandbox.request' || typeof value.id !== 'string') return;
      const request = record(value.request) ? value.request as CymonkeyRequest : {method: ''};
      try {
        port.postMessage({channel: 'cymonkey.jangolova.sandbox.response', id: value.id, result: await runtime.dispatch(request)});
      } catch (error) {
        port.postMessage({channel: 'cymonkey.jangolova.sandbox.response', id: value.id, error: errorMessage(error)});
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

function createCameraMediaProvider(port: MessagePort) {
  const pending = new Map<string, PendingMediaRequest>();
  let sequence = 0;
  let active: {sessionId: string; peer: RTCPeerConnection; stream: MediaStream} | null = null;

  const request = (method: 'camera.open' | 'camera.close', params: Record<string, unknown>) => {
    sequence += 1;
    const id = `media-${sequence}`;
    return new Promise<Record<string, unknown>>((resolve, reject) => {
      const timer = window.setTimeout(() => {
        pending.delete(id);
        reject(new Error('Cymonkey media broker timed out'));
      }, 15_000);
      pending.set(id, {resolve, reject, timer});
      port.postMessage({channel: 'cymonkey.jangolova.media.request', id, method, params});
    });
  };

  return {
    provider: {
      async openCamera() {
        if (active) throw new Error('camera media session is already open');
        const sessionId = randomIdentifier();
        const peer = new RTCPeerConnection({iceServers: []});
        const track = waitForTrack(peer);
        peer.addTransceiver('video', {direction: 'recvonly'});
        try {
          await peer.setLocalDescription(await peer.createOffer());
          await waitForIce(peer);
          if (!peer.localDescription) throw new Error('camera receiver could not create an offer');
          const result = await request('camera.open', {
            sessionId,
            offer: {type: 'offer', sdp: peer.localDescription.sdp},
          });
          const answer = result.answer;
          if (!record(answer) || answer.type !== 'answer' || typeof answer.sdp !== 'string') throw new Error('Cymonkey media broker returned an invalid answer');
          await peer.setRemoteDescription({type: 'answer', sdp: answer.sdp});
          const stream = await track.promise;
          active = {sessionId, peer, stream};
          return stream;
        } catch (error) {
          track.cancel();
          peer.close();
          await request('camera.close', {sessionId}).catch(() => undefined);
          throw error;
        }
      },
      async closeCamera() {
        const session = active;
        active = null;
        if (!session) return;
        session.stream.getTracks().forEach((track) => track.stop());
        session.peer.close();
        await request('camera.close', {sessionId: session.sessionId});
      },
    },
    receive(value: Record<string, unknown>) {
      if (typeof value.id !== 'string') return;
      const item = pending.get(value.id);
      if (!item) return;
      pending.delete(value.id);
      window.clearTimeout(item.timer);
      if (value.error) item.reject(new Error(String(value.error)));
      else if (record(value.result)) item.resolve(value.result);
      else item.reject(new Error('Cymonkey media broker returned an invalid response'));
    },
  };
}

function waitForTrack(peer: RTCPeerConnection) {
  let rejectTrack!: (error: Error) => void;
  const listener = (event: RTCTrackEvent) => {
    cleanup();
    resolveTrack(event.streams[0] ?? new MediaStream([event.track]));
  };
  let resolveTrack!: (stream: MediaStream) => void;
  const timer = window.setTimeout(() => {
    cleanup();
    rejectTrack(new Error('Cymonkey camera track timed out'));
  }, 10_000);
  const cleanup = () => {
    window.clearTimeout(timer);
    peer.removeEventListener('track', listener);
  };
  const promise = new Promise<MediaStream>((resolve, reject) => {
    resolveTrack = resolve;
    rejectTrack = reject;
  });
  peer.addEventListener('track', listener);
  return {promise, cancel: cleanup};
}

function waitForIce(peer: RTCPeerConnection) {
  if (peer.iceGatheringState === 'complete') return Promise.resolve();
  return new Promise<void>((resolve, reject) => {
    const timer = window.setTimeout(() => finish(new Error('camera receiver ICE gathering timed out')), 5_000);
    const listener = () => { if (peer.iceGatheringState === 'complete') finish(); };
    const finish = (error?: Error) => {
      window.clearTimeout(timer);
      peer.removeEventListener('icegatheringstatechange', listener);
      if (error) reject(error); else resolve();
    };
    peer.addEventListener('icegatheringstatechange', listener);
  });
}

function randomIdentifier() {
  const values = crypto.getRandomValues(new Uint32Array(4));
  return `camera-${[...values].map((value) => value.toString(16)).join('')}`;
}
