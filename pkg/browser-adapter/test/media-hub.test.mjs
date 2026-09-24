import assert from 'node:assert/strict';
import test from 'node:test';
import {createMediaBrokerController, XalletSpookClient} from '../dist/index.js';

test('media broker scopes camera sessions to a tab and closes an idle host', async () => {
  let exists = false;
  let creates = 0;
  let closes = 0;
  const messages = [];
  const broker = createMediaBrokerController({
    exists: async () => exists,
    create: async () => { creates++; exists = true; },
    close: async () => { closes++; exists = false; },
    send: async (message) => { messages.push(message); return message.method === 'camera.open'
      ? {ok: true, answer: {type: 'answer', sdp: 'answer-sdp'}}
      : {ok: true, closed: true, activeSessions: 0}; },
  });
  assert.deepEqual(await broker.open(9, 'camera-one', {type: 'offer', sdp: 'offer-sdp'}), {answer: {type: 'answer', sdp: 'answer-sdp'}});
  assert.equal(creates, 1);
  assert.equal(messages[0].sessionKey, '9:camera-one');
  assert.deepEqual(await broker.close(9, 'camera-one'), {closed: true});
  assert.equal(closes, 1);
  await assert.rejects(broker.open(9, '../other', {type: 'offer', sdp: 'x'}), /invalid/);
});

test('optional Xallet bridge accepts only the discovered enabled hub sender', async () => {
  const sent = [];
  const client = new XalletSpookClient('Reader', {
    status: 'ready', xalletSpook: 'discovering', browser: 'chrome', capabilities: [], extensionId: 'reader-id',
  }, 'popup.html', () => {}, {
    management: {getAll: async () => [{id: 'xallet-id', name: 'Xallet', enabled: true}]},
    runtime: {sendMessage: async (id, message) => { sent.push({id, message}); }},
  });
  await client.probe();
  assert.equal(client.acceptsExternalSender('xallet-id'), true);
  assert.equal(client.acceptsExternalSender('other'), false);
  assert.equal(sent[0].message.type, 'REGISTER_SPOKE');
  client.stop();
});
