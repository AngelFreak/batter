import { test, beforeEach, afterEach, mock } from 'node:test';
import assert from 'node:assert/strict';
import { FakeWebSocket, installBrowserEnv, fakeCanvas } from './test-browser-env';
import { WheelGestures, GestureMessage } from './wheel-gesture';

installBrowserEnv();
// eslint-disable-next-line @typescript-eslint/no-require-imports
const { DeviceInputHandler } = require('./device-input') as typeof import('./device-input');
// eslint-disable-next-line @typescript-eslint/no-require-imports
const { MultiplexerInputHandler } = require('./device-input-multiplexer') as typeof import('./device-input-multiplexer');

beforeEach(() => {
  FakeWebSocket.instances = [];
  mock.timers.enable({ apis: ['setTimeout'] });
});
afterEach(() => mock.timers.reset());

const round = (n: number | undefined) => (n === undefined ? n : Math.round(n * 1000) / 1000);
const summary = (msgs: GestureMessage[]) =>
  msgs.map((m) => (m.type === 'touch' ? `touch ${m.action} ${round(m.x)}` : `scroll h${m.scroll_h} v${m.scroll_v}`));

// Canvas 400px wide; wheel deltas in pixels (deltaMode 0) like a trackpad.
const wheel = (g: WheelGestures, deltaX: number, deltaY: number, x = 0.5, y = 0.5) =>
  g.wheel({ deltaX, deltaY, deltaMode: 0 }, { x, y }, { width: 400, height: 800 });

test('a horizontal two-finger swipe becomes a finger drag, released when the trackpad goes quiet', () => {
  const out: GestureMessage[] = [];
  const g = new WheelGestures((m) => out.push(m));
  wheel(g, 40, 2);
  wheel(g, 40, -1);
  wheel(g, 40, 0);
  assert.deepEqual(summary(out), ['touch 0 0.5', 'touch 2 0.4', 'touch 2 0.3', 'touch 2 0.2']);
  mock.timers.tick(200);
  assert.deepEqual(summary(out).at(-1), 'touch 1 0.2');
});

test('vertical two-finger scrolling stays a scroll event (feeds, lists)', () => {
  const out: GestureMessage[] = [];
  const g = new WheelGestures((m) => out.push(m));
  wheel(g, 1, 30);
  wheel(g, -2, -30);
  assert.deepEqual(summary(out), ['scroll h0 v-1', 'scroll h0 v1']);
});

test('the axis is locked per gesture, and a pause starts a new gesture', () => {
  const out: GestureMessage[] = [];
  const g = new WheelGestures((m) => out.push(m));
  wheel(g, 40, 0);
  wheel(g, 5, 30); // vertical-ish wobble mid-swipe: still a drag
  assert.deepEqual(summary(out), ['touch 0 0.5', 'touch 2 0.4', 'touch 2 0.388']);
  mock.timers.tick(200); // released
  out.length = 0;
  wheel(g, 0, 30); // new gesture, vertical
  assert.deepEqual(summary(out), ['scroll h0 v-1']);
});

test('the drag stays on the screen', () => {
  const out: GestureMessage[] = [];
  const g = new WheelGestures((m) => out.push(m));
  wheel(g, -300, 0, 0.9);
  wheel(g, -300, 0, 0.9);
  assert.deepEqual(summary(out), ['touch 0 0.9', 'touch 2 1', 'touch 2 1']);
});

test('line-based wheel deltas (Firefox mouse wheels) are scaled to pixels', () => {
  const out: GestureMessage[] = [];
  const g = new WheelGestures((m) => out.push(m));
  g.wheel({ deltaX: 2.5, deltaY: 0, deltaMode: 1 }, { x: 0.5, y: 0.5 }, { width: 400, height: 800 });
  assert.deepEqual(summary(out), ['touch 0 0.5', 'touch 2 0.4']); // 2.5 lines * 16px = 40px
});

test('detaching mid-swipe lifts the finger', () => {
  const out: GestureMessage[] = [];
  const g = new WheelGestures((m) => out.push(m));
  wheel(g, 40, 0);
  g.cancel();
  assert.deepEqual(summary(out).at(-1), 'touch 1 0.4');
});

const sent = (ws: FakeWebSocket) => ws.sent.map((s) => JSON.parse(s));

test('device viewer: a horizontal trackpad swipe reaches the phone as touch', () => {
  const canvas = fakeCanvas();
  const h = new DeviceInputHandler(canvas);
  h.connect('A');
  const ws = FakeWebSocket.instances[0];
  ws.open();
  canvas.fire('wheel', { deltaX: 40, deltaY: 0, deltaMode: 0, clientX: 200, clientY: 400 });
  assert.deepEqual(sent(ws).map((m) => [m.type, m.action]), [['touch', 0], ['touch', 2]]);
  h.disconnect();
});

test('multiplexer: a horizontal trackpad swipe is broadcast as touch', () => {
  const canvas = fakeCanvas();
  const h = new MultiplexerInputHandler();
  h.attachCanvas(canvas);
  h.setDevices(['A', 'B']);
  for (const ws of FakeWebSocket.instances) ws.open();
  canvas.fire('wheel', { deltaX: 40, deltaY: 0, deltaMode: 0, clientX: 200, clientY: 400 });
  for (const ws of FakeWebSocket.instances) {
    assert.deepEqual(sent(ws).map((m) => [m.type, m.action]), [['touch', 0], ['touch', 2]], ws.url);
  }
  h.disconnect();
});
