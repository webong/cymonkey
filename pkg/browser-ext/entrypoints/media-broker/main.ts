import {errorMessage, isRecord} from '../../src/types';

type BrokerSession = {peer: RTCPeerConnection; stream: MediaStream};

const sessions = new Map<string, BrokerSession>();

browser.runtime.onMessage.addListener((message) => {
  if (!isRecord(message) || message.target !== 'cymonkey.jangolova.media-broker.offscreen') return undefined;
  return dispatch(String(message.method || ''), isRecord(message.params) ? message.params : {});
});

window.addEventListener('unload', () => {
  for (const key of [...sessions.keys()]) closeSession(key);
});

async function dispatch(method: string, params: Record<string, unknown>) {
  try {
    const sessionKey = identifier(params.sessionKey);
    if (method === 'camera.open') return {ok: true, answer: await openCamera(sessionKey, params.offer)};
    if (method === 'camera.close') {
      const closed = closeSession(sessionKey);
      return {ok: true, closed, activeSessions: sessions.size};
    }
    throw new Error(`unsupported media broker method ${JSON.stringify(method)}`);
  } catch (error) {
    return {ok: false, error: errorMessage(error)};
  }
}

async function openCamera(sessionKey: string, offerValue: unknown) {
  closeSession(sessionKey);
  const offer = description(offerValue, 'offer');
  const stream = await navigator.mediaDevices.getUserMedia({video: true, audio: false});
  const peer = new RTCPeerConnection({iceServers: []});
  const session = {peer, stream};
  sessions.set(sessionKey, session);
  peer.addEventListener('connectionstatechange', () => {
    if (peer.connectionState === 'failed' || peer.connectionState === 'closed') closeSession(sessionKey, session);
  });
  try {
    await peer.setRemoteDescription(offer);
    for (const track of stream.getTracks()) peer.addTrack(track, stream);
    await peer.setLocalDescription(await peer.createAnswer());
    await waitForIce(peer);
    if (!peer.localDescription) throw new Error('camera broker could not create an answer');
    return {type: 'answer', sdp: peer.localDescription.sdp};
  } catch (error) {
    closeSession(sessionKey, session);
    throw error;
  }
}

function closeSession(sessionKey: string, expected?: BrokerSession) {
  const session = sessions.get(sessionKey);
  if (!session || expected && session !== expected) return false;
  sessions.delete(sessionKey);
  session.stream.getTracks().forEach((track) => track.stop());
  session.peer.close();
  return true;
}

function waitForIce(peer: RTCPeerConnection) {
  if (peer.iceGatheringState === 'complete') return Promise.resolve();
  return new Promise<void>((resolve, reject) => {
    const timer = window.setTimeout(() => finish(new Error('camera broker ICE gathering timed out')), 5_000);
    const listener = () => { if (peer.iceGatheringState === 'complete') finish(); };
    const finish = (error?: Error) => {
      window.clearTimeout(timer);
      peer.removeEventListener('icegatheringstatechange', listener);
      if (error) reject(error); else resolve();
    };
    peer.addEventListener('icegatheringstatechange', listener);
  });
}

function description(value: unknown, expectedType: 'offer') {
  if (!isRecord(value) || value.type !== expectedType || typeof value.sdp !== 'string' || value.sdp.length > 1_000_000) {
    throw new Error('camera broker offer is invalid');
  }
  return {type: expectedType, sdp: value.sdp} as RTCSessionDescriptionInit;
}

function identifier(value: unknown) {
  if (typeof value !== 'string' || !/^\d+:[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/.test(value)) throw new Error('media session key is invalid');
  return value;
}
