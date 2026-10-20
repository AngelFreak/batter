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

// Node 20's mock clock doesn't run a timeout scheduled during the same
// tick(), so time is advanced in small steps.
const elapse = (ms: number) => {
  for (let t = 0; t < ms; t += 5) mock.timers.tick(5);
};

const touches = (ws: FakeWebSocket) =>
  ws.sent.map((s) => JSON.parse(s)).filter((m: { type: string }) => m.type === 'touch') as {
    action: number;
    x: number;
    y: number;
  }[];

test('a pad swipe is played out over time like a finger, then lifted', () => {
  const h = new DeviceInputHandler(fakeCanvas());
  h.connect('A');
  last().open();
  h.swipe('up');
  assert.deepEqual(touches(last()).map((t) => t.action), [0], 'only the finger down at once');
  elapse(1_000);
  const sent = touches(last());
  assert.equal(sent[sent.length - 1].action, 1);
  assert.ok(sent.slice(1, -1).every((t) => t.action === 2));
  assert.ok(sent[0].y > sent[sent.length - 1].y, 'not upward');
  h.disconnect();
});

test('pressing the pad again mid-swipe does not start a second finger', () => {
  const h = new DeviceInputHandler(fakeCanvas());
  h.connect('A');
  last().open();
  h.swipe('up');
  h.swipe('left');
  elapse(1_000);
  assert.equal(touches(last()).filter((t) => t.action === 0).length, 1);
  h.swipe('left'); // the first one is over, so this one goes
  elapse(1_000);
  assert.equal(touches(last()).filter((t) => t.action === 0).length, 2);
  h.disconnect();
});

test('a swipe stops when the viewer disconnects', () => {
  const h = new DeviceInputHandler(fakeCanvas());
  h.connect('A');
  const ws = last();
  ws.open();
  h.swipe('down');
  h.disconnect();
  const before = ws.sent.length;
  elapse(1_000);
  assert.equal(ws.sent.length, before);
});
