import assert from 'node:assert/strict';
import test from 'node:test';

import { GatewayWebSocketClient } from './wsClient.js';

class FakeWebSocket {
  static OPEN = 1;

  static instances = [];

  constructor(url) {
    this.url = url;
    this.readyState = 0;
    this.listeners = new Map();
    this.sent = [];
    FakeWebSocket.instances.push(this);
  }

  addEventListener(type, listener) {
    const listeners = this.listeners.get(type) || [];
    listeners.push(listener);
    this.listeners.set(type, listeners);
  }

  close() {
    this.readyState = 3;
  }

  send(raw) {
    this.sent.push(JSON.parse(raw));
  }

  emit(type, value = {}) {
    for (const listener of this.listeners.get(type) || []) {
      listener(value);
    }
  }

  open() {
    this.readyState = FakeWebSocket.OPEN;
    this.emit('open');
  }

  receive(message) {
    this.emit('message', { data: JSON.stringify(message) });
  }

  disconnect() {
    this.readyState = 3;
    this.emit('close');
  }
}

function respondToLatest(socket, payload = {}) {
  const request = socket.sent.at(-1);
  socket.receive({
    id: request.id,
    type: 'response',
    ok: true,
    payload,
  });
  return request;
}

test('reconnect resumes each run from the highest observed root sequence', async () => {
  const previousWebSocket = globalThis.WebSocket;
  const previousWindow = globalThis.window;
  globalThis.WebSocket = FakeWebSocket;
  globalThis.window = { location: { origin: 'http://127.0.0.1:5173' } };
  FakeWebSocket.instances = [];

  const statuses = [];
  const client = new GatewayWebSocketClient({
    baseUrl: 'http://127.0.0.1:17888',
    getResumeCursors: () => ({ run_1: 2 }),
    onStatus: (status) => statuses.push(status),
    reconnectBaseDelay: 1,
    reconnectMaxDelay: 1,
  });

  try {
    client.connect();
    const first = FakeWebSocket.instances[0];
    first.open();
    assert.equal(respondToLatest(first).type, 'auth');
    await Promise.resolve();

    const initialResume = first.sent.at(-1);
    assert.equal(initialResume.method, 'run.resume');
    assert.deepEqual(initialResume.payload.last_seen, { run_1: 2 });
    respondToLatest(first);

    first.receive({
      type: 'event',
      method: 'run.event',
      payload: { root_run_id: 'run_1', root_seq: 7, type: 'message_delta' },
    });
    first.disconnect();

    await new Promise((resolve) => setTimeout(resolve, 10));
    const second = FakeWebSocket.instances[1];
    assert.ok(second);
    second.open();
    assert.equal(respondToLatest(second).type, 'auth');
    await Promise.resolve();

    const resumed = second.sent.at(-1);
    assert.equal(resumed.method, 'run.resume');
    assert.deepEqual(resumed.payload.last_seen, { run_1: 7 });
    assert.ok(statuses.includes('reconnecting'));
    assert.equal(statuses.at(-1), 'connected');
  } finally {
    client.close();
    globalThis.WebSocket = previousWebSocket;
    globalThis.window = previousWindow;
  }
});
