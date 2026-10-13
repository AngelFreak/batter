import { test, beforeEach } from 'node:test';
import assert from 'node:assert/strict';
import { FakeWebSocket, installBrowserEnv, fakeCanvas } from './test-browser-env';

installBrowserEnv();
// eslint-disable-next-line @typescript-eslint/no-require-imports
const { navMessages } = require('./device-nav') as typeof import('./device-nav');
// eslint-disable-next-line @typescript-eslint/no-require-imports
const { DeviceInputHandler } = require('./device-input') as typeof import('./device-input');
// eslint-disable-next-line @typescript-eslint/no-require-imports
const { MultiplexerInputHandler } = require('./device-input-multiplexer') as typeof import('./device-input-multiplexer');

beforeEach(() => {
  FakeWebSocket.instances = [];
});

const sent = (ws: FakeWebSocket) => ws.sent.map((s) => JSON.parse(s));

test('Home, Recents and All apps send Android key down+up', () => {
  const cases = { home: 3, recents: 187, all_apps: 284 } as const;
  for (const [action, keycode] of Object.entries(cases)) {
    assert.deepEqual(
      navMessages(action as keyof typeof cases).map((m) => [m.type, m.action, m.keycode]),
      [['key', 0, keycode], ['key', 1, keycode]],
      action,
    );
  }
});

test("Back uses scrcpy's back-or-screen-on (down+up), like scrcpy's right-click", () => {
  assert.deepEqual(
    navMessages('back').map((m) => [m.type, m.action]),
    [['back', 0], ['back', 1]],
  );
});

test('device viewer: nav() sends the button press over the control socket', () => {
  const h = new DeviceInputHandler(fakeCanvas());
  h.connect('A');
  const ws = FakeWebSocket.instances[0];
  ws.open();
  h.nav('recents');
  assert.deepEqual(sent(ws), navMessages('recents'));
  h.disconnect();
});

test('device viewer: right-click on the screen sends Back', () => {
  const canvas = fakeCanvas();
  const h = new DeviceInputHandler(canvas);
  h.connect('A');
  const ws = FakeWebSocket.instances[0];
  ws.open();
  canvas.fire('contextmenu');
  assert.deepEqual(sent(ws), navMessages('back'));
  h.disconnect();
});

test('multiplexer: nav() broadcasts to every connected device', () => {
  const h = new MultiplexerInputHandler();
  h.setDevices(['A', 'B']);
  for (const ws of FakeWebSocket.instances) ws.open();
  h.nav('home');
  for (const ws of FakeWebSocket.instances) {
    assert.deepEqual(sent(ws), navMessages('home'), ws.url);
  }
});

test('multiplexer: right-click on the primary screen sends Back to all', () => {
  const canvas = fakeCanvas();
  const h = new MultiplexerInputHandler();
  h.attachCanvas(canvas);
  h.setDevices(['A', 'B']);
  for (const ws of FakeWebSocket.instances) ws.open();
  canvas.fire('contextmenu');
  for (const ws of FakeWebSocket.instances) {
    assert.deepEqual(sent(ws), navMessages('back'), ws.url);
  }
  h.disconnect();
});

// A browser right-click fires mousedown, contextmenu and mouseup, all with button 2.
const rightClick = (canvas: ReturnType<typeof fakeCanvas>) => {
  canvas.fire('mousedown', { button: 2, buttons: 2, clientX: 200, clientY: 400 });
  canvas.fire('contextmenu', { button: 2, clientX: 200, clientY: 400 });
  canvas.fire('mouseup', { button: 2, buttons: 0, clientX: 200, clientY: 400 });
};
const leftClick = (canvas: ReturnType<typeof fakeCanvas>) => {
  canvas.fire('mousedown', { button: 0, buttons: 1, clientX: 200, clientY: 400 });
  canvas.fire('mouseup', { button: 0, buttons: 0, clientX: 200, clientY: 400 });
};

test('device viewer: a full right-click sends only Back, not a tap', () => {
  const canvas = fakeCanvas();
  const h = new DeviceInputHandler(canvas);
  h.connect('A');
  const ws = FakeWebSocket.instances[0];
  ws.open();
  rightClick(canvas);
  assert.deepEqual(sent(ws), navMessages('back'));
  h.disconnect();
});

test('device viewer: a left click still taps', () => {
  const canvas = fakeCanvas();
  const h = new DeviceInputHandler(canvas);
  h.connect('A');
  const ws = FakeWebSocket.instances[0];
  ws.open();
  leftClick(canvas);
  assert.deepEqual(sent(ws).map((m) => [m.type, m.action, m.x, m.y]), [['touch', 0, 0.5, 0.5], ['touch', 1, 0.5, 0.5]]);
  h.disconnect();
});

test('multiplexer: a full right-click sends only Back, not a tap', () => {
  const canvas = fakeCanvas();
  const h = new MultiplexerInputHandler();
  h.attachCanvas(canvas);
  h.setDevices(['A', 'B']);
  for (const ws of FakeWebSocket.instances) ws.open();
  rightClick(canvas);
  for (const ws of FakeWebSocket.instances) assert.deepEqual(sent(ws), navMessages('back'), ws.url);
  h.disconnect();
});

test('multiplexer: a left click still taps every device', () => {
  const canvas = fakeCanvas();
  const h = new MultiplexerInputHandler();
  h.attachCanvas(canvas);
  h.setDevices(['A', 'B']);
  for (const ws of FakeWebSocket.instances) ws.open();
  leftClick(canvas);
  for (const ws of FakeWebSocket.instances) {
    assert.deepEqual(sent(ws).map((m) => [m.type, m.action]), [['touch', 0], ['touch', 1]], ws.url);
  }
  h.disconnect();
});
