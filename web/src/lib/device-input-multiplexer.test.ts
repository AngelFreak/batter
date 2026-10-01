// Run with `npm test` (compiles with tsc, runs with node's built-in runner).
import { test, beforeEach } from 'node:test';
import assert from 'node:assert/strict';

// Minimal browser globals the handler touches.
class FakeWebSocket {
  static OPEN = 1;
  static instances: FakeWebSocket[] = [];
  readyState = 0;
  sent: string[] = [];
  onopen: (() => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  onmessage: ((e: { data: string }) => void) | null = null;
  constructor(public url: string) {
    FakeWebSocket.instances.push(this);
  }
  open() {
    this.readyState = FakeWebSocket.OPEN;
    this.onopen?.();
  }
  send(data: string) {
    this.sent.push(data);
  }
  // Like a browser, close() doesn't fire onclose synchronously.
  close() {
    this.readyState = 3;
  }
  fireClose() {
    this.onclose?.();
  }
}
const g = globalThis as Record<string, unknown>;
g.WebSocket = FakeWebSocket;
g.window = { location: { protocol: 'http:', host: 'test' } };
g.localStorage = { getItem: () => 'tok' };

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
  old.fireClose();

  h.sendText('hi');
  assert.equal(fresh.sent.length, 1, 'replacement connection was dropped by stale close');
});
