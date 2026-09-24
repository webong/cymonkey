import assert from 'node:assert/strict';
import test from 'node:test';
import {OutboundControlClient, validateOutboundConfiguration} from '../dist/index.js';

test('outbound control authenticates before dispatch and never describes its token', async () => {
  const stored = {};
  const storage = {
    async get(key) { return {[key]: stored[key]}; },
    async set(value) { Object.assign(stored, value); },
    async remove(key) { delete stored[key]; },
  };
  const sockets = [];
  const calls = [];
  const statuses = [];
  const client = new OutboundControlClient(
    storage,
    async (message, source) => {
      calls.push({message, source});
      return {ok: true, result: {product: 'Cymonkey'}};
    },
    (status) => statuses.push(status),
    (endpoint) => {
      const socket = new FakeSocket(endpoint);
      sockets.push(socket);
      return socket;
    },
  );
  const token = 'short-lived-token-value';
  const expiresAt = new Date(Date.now() + 60_000).toISOString();
  await client.configure({endpoint: 'wss://control.example/ws', token, expiresAt});
  const socket = sockets[0];
  assert.equal(statuses.at(-1), 'connecting');
  assert.doesNotMatch(JSON.stringify(client.describe()), new RegExp(token));

  socket.emit('open');
  assert.equal(JSON.parse(socket.sent[0]).type, 'CYMONKEY_EXTENSION_AUTH');
  assert.equal(JSON.parse(socket.sent[0]).token, token);
  socket.emit('message', JSON.stringify({type: 'CYMONKEY_EXTENSION_CALL', id: 'before-auth', method: 'describe'}));
  await tick();
  assert.equal(calls.length, 0);

  socket.emit('message', JSON.stringify({type: 'CYMONKEY_EXTENSION_AUTHENTICATED'}));
  socket.emit('message', JSON.stringify({type: 'CYMONKEY_EXTENSION_CALL', id: 'call-1', method: 'describe'}));
  await tick();
  assert.equal(statuses.at(-1), 'connected');
  assert.equal(calls[0].source, 'authenticated-websocket');
  const response = JSON.parse(socket.sent.at(-1));
  assert.equal(response.type, 'CYMONKEY_EXTENSION_RESPONSE');
  assert.equal(response.id, 'call-1');
  assert.equal(response.ok, true);

  await client.disable();
  assert.equal(client.describe().status, 'disabled');
});

test('outbound control rejects remote plaintext and long-lived tokens', async () => {
  assert.throws(() => validateOutboundConfiguration({
    endpoint: 'ws://control.example/ws', token: 'long-enough-token-value', expiresAt: new Date(Date.now() + 60_000).toISOString(),
  }), /loopback/);
  assert.throws(() => validateOutboundConfiguration({
    endpoint: 'wss://control.example/ws', token: 'long-enough-token-value', expiresAt: new Date(Date.now() + 25 * 60 * 60 * 1000).toISOString(),
  }), /within 24 hours/);
});

class FakeSocket {
  readyState = 1;
  sent = [];
  listeners = new Map();
  constructor(endpoint) { this.endpoint = endpoint; }
  send(value) { this.sent.push(value); }
  close() { this.readyState = 3; }
  addEventListener(type, listener) { this.listeners.set(type, [...(this.listeners.get(type) ?? []), listener]); }
  emit(type, data) {
    for (const listener of this.listeners.get(type) ?? []) listener(type === 'message' ? {data} : {});
  }
}

const tick = () => new Promise((resolve) => setTimeout(resolve, 0));
