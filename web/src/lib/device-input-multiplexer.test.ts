// Run with `npm test` (compiles with tsc, runs with node's built-in runner).
import { test, beforeEach } from 'node:test';
import assert from 'node:assert/strict';

import { FakeWebSocket, installBrowserEnv } from './test-browser-env';

installBrowserEnv();

// Imported after globals exist.
// eslint-disable-next-line @typescript-eslint/no-require-imports
const { MultiplexerInputHandler } = require('./device-input-multiplexer') as typeof import('./device-input-multiplexer');

function socketFor(serial: string): FakeWebSocket {
  const ws = FakeWebSocket.instances.filter(w => w.url.includes(`/device/${serial}/`)).pop();
  assert.ok(ws, `no socket for ${serial}`);
  return ws;
}

beforeEach(() => {
  FakeWebSocket.instances = [];
});

test('setDevices stops broadcasting to devices no longer in the set', () => {
  const h = new MultiplexerInputHandler();
  h.setDevices(['A', 'B']);
  socketFor('A').open();
  socketFor('B').open();

  h.setDevices(['A']); // B's tile left the screen

  h.sendText('hi');
  assert.equal(socketFor('A').sent.length, 1);
  assert.equal(socketFor('B').sent.length, 0, 'removed device still received input');
  assert.equal(socketFor('B').readyState, 3, 'removed device socket not closed');
  assert.deepEqual(h.connectedSerials, ['A']);
});

test('setDevices connects newly added devices and keeps existing sockets', () => {
  const h = new MultiplexerInputHandler();
  h.setDevices(['A']);
  const a = socketFor('A');
  h.setDevices(['A', 'B']);
  assert.equal(socketFor('A'), a, 'existing connection was replaced');
  assert.deepEqual(h.connectedSerials.sort(), ['A', 'B']);
});

test('a late close from a removed socket does not drop its replacement', () => {
  const h = new MultiplexerInputHandler();
  h.setDevices(['A']);
  const old = socketFor('A');
  old.open();

  h.setDevices([]); // device drops out...
  h.setDevices(['A']); // ...and comes back before the old close event lands
  const fresh = socketFor('A');
  assert.notEqual(fresh, old);
  fresh.open();
  old.drop();

  h.sendText('hi');
  assert.equal(fresh.sent.length, 1, 'replacement connection was dropped by stale close');
});

test('a phone whose control socket is refused with 403 shows as denied', async () => {
  const calls: string[] = [];
  (globalThis as Record<string, unknown>).fetch = (url: string) => {
    calls.push(url);
    return Promise.resolve({ status: url.includes('/device/B/') ? 403 : 404 });
  };
  const h = new MultiplexerInputHandler();
  const status = new Map<string, string>();
  h.setOnStatusChange((serial, s) => status.set(serial, s));
  h.setDevices(['A', 'B']);
  socketFor('A').drop();
  socketFor('B').drop();
  await new Promise<void>((r) => setImmediate(r));
  assert.equal(status.get('B'), 'denied');
  assert.equal(status.get('A'), 'disconnected');
  assert.equal(calls.length, 2);
});
