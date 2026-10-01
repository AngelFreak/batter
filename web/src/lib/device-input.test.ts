import { test, beforeEach, afterEach, mock } from 'node:test';
import assert from 'node:assert/strict';
import { FakeWebSocket, installBrowserEnv, fakeCanvas } from './test-browser-env';

installBrowserEnv();
// eslint-disable-next-line @typescript-eslint/no-require-imports
const { DeviceInputHandler } = require('./device-input') as typeof import('./device-input');

beforeEach(() => {
  FakeWebSocket.instances = [];
  mock.timers.enable({ apis: ['setTimeout'] });
});
afterEach(() => mock.timers.reset());

const last = () => FakeWebSocket.instances[FakeWebSocket.instances.length - 1];

test('control socket backs off instead of retrying every 2s forever', () => {
  const h = new DeviceInputHandler(fakeCanvas());
  h.connect('A');
  // Let the backoff grow over several failed attempts.
  for (let i = 0; i < 4; i++) {
    last().drop();
    mock.timers.tick(30_000);
  }
  // By now the next retry must wait well beyond the old fixed 2s.
  last().drop();
  const before = FakeWebSocket.instances.length;
  mock.timers.tick(2_000);
  assert.equal(FakeWebSocket.instances.length, before, 'retried within 2s after repeated failures');
  mock.timers.tick(30_000);
  assert.equal(FakeWebSocket.instances.length, before + 1, 'never retried');
  h.disconnect();
});

test('control socket stops retrying after disconnect()', () => {
  const h = new DeviceInputHandler(fakeCanvas());
  h.connect('A');
  last().open();
  h.disconnect();
  last().drop();
  mock.timers.tick(60_000);
  assert.equal(FakeWebSocket.instances.length, 1);
});
