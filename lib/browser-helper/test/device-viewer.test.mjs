import assert from 'node:assert/strict';
import test from 'node:test';

// Minimal same-origin browser globals; WebRTC is never reached in these checks.
const listeners = {window: new Map(), document: new Map()};
const track = (map) => ({
  addEventListener: (type, fn) => map.set(type, fn),
  removeEventListener: (type, fn) => { if (map.get(type) === fn) map.delete(type); },
});
globalThis.location = new URL('https://viewer.test/');
globalThis.window = track(listeners.window);
globalThis.document = {hidden: false, ...track(listeners.document)};
const sent = [];
globalThis.fetch = async (url, init) => { sent.push({url, body: JSON.parse(init.body), keepalive: init.keepalive}); return {ok: true, json: async () => ({output: []})}; };

const {DeviceViewer} = await import('../dist/index.js');
const {DeviceViewer: subpathViewer} = await import('../dist/device-viewer.js');
const video = () => ({srcObject: 'stream'});

test('device viewer is exported from the root and its own entry point', () => {
  assert.equal(DeviceViewer, subpathViewer);
});

test('device viewer accepts only a same-origin session endpoint', () => {
  assert.throws(() => new DeviceViewer({url: 'https://other.test/v1/device-viewer/ab12', token: 't', video: video()}), /same-origin/);
  for (const url of ['/v1/device-sessions/ab12', '/v1/device-viewer/AB12', '/v1/device-viewer/ab12/x']) {
    assert.throws(() => new DeviceViewer({url, token: 't', video: video()}), /Invalid viewer endpoint/);
  }
  const viewer = new DeviceViewer({url: '/v1/device-viewer/ab12', token: 't', video: video(), relayOnly: true});
  assert.equal(viewer.url, 'https://viewer.test/v1/device-viewer/ab12');
  assert.equal(viewer.configuration.iceTransportPolicy, 'relay');
  viewer.close();
});

test('device viewer refuses input without a connected control channel', () => {
  const viewer = new DeviceViewer({url: '/v1/device-viewer/ab12', token: 't', video: video()});
  assert.throws(() => viewer.type('hi'), /not connected/);
  assert.throws(() => viewer.press('Enter'), /not connected/);
  assert.throws(() => viewer.click(0.5, 0.5), /not connected/);
  assert.doesNotThrow(() => viewer.up(0, 0));
  viewer.close();
});

test('closing sends a best-effort bye, clears the credential and listeners, and is idempotent', async () => {
  sent.length = 0;
  const element = video();
  const viewer = new DeviceViewer({url: '/v1/device-viewer/ab12', token: 'secret', video: element});
  const states = [];
  viewer.addEventListener('state', (event) => states.push(event.detail));
  assert.ok(listeners.window.has('blur') && listeners.document.has('visibilitychange'));
  viewer.close();
  viewer.close();
  assert.deepEqual(sent.map(({body, keepalive}) => ({body, keepalive})), [{body: {capability: 'webrtc.signal', input: {type: 'bye'}}, keepalive: true}]);
  assert.equal(viewer.token, '');
  assert.equal(element.srcObject, null);
  assert.deepEqual(states, ['closed']);
  assert.ok(!listeners.window.has('blur') && !listeners.document.has('visibilitychange'));
  await assert.rejects(viewer.connect(), /new viewer/);
});
