// Keys reach the phone only while its screen (the canvas) has focus. These run
// against the real handlers, with listeners attached as in the browser.
import { test, beforeEach } from 'node:test';
import assert from 'node:assert/strict';
import { FakeWebSocket, installBrowserEnv, fakeCanvas, fakeDocument, FakeCanvas } from './test-browser-env';

installBrowserEnv();
// eslint-disable-next-line @typescript-eslint/no-require-imports
const { DeviceInputHandler } = require('./device-input') as typeof import('./device-input');
// eslint-disable-next-line @typescript-eslint/no-require-imports
const { MultiplexerInputHandler } = require('./device-input-multiplexer') as typeof import('./device-input-multiplexer');

type Msg = { type: string; action?: number; keycode?: number; text?: string };

let clipboardReads = 0;
beforeEach(() => {
  FakeWebSocket.instances = [];
  clipboardReads = 0;
  Object.defineProperty(globalThis, 'navigator', { configurable: true, value: {
    clipboard: {
      readText: () => {
        clipboardReads++;
        return Promise.resolve('');
      },
      writeText: () => Promise.resolve(),
    },
  } });
});

const msgs = (ws: FakeWebSocket): Msg[] => ws.sent.map((s) => JSON.parse(s));
const keyA = { code: 'KeyA', key: 'a' };

// The viewer and the multiplexer must behave the same, so each case runs on both.
const rigs: { name: string; setup(): { canvas: FakeCanvas; ws: FakeWebSocket[] } }[] = [
  {
    name: 'viewer',
    setup() {
      const canvas = fakeCanvas();
      const h = new DeviceInputHandler(canvas);
      h.connect('A');
      FakeWebSocket.instances[0].open();
      return { canvas, ws: [FakeWebSocket.instances[0]] };
    },
  },
  {
    name: 'multiplexer',
    setup() {
      const canvas = fakeCanvas();
      const h = new MultiplexerInputHandler();
      h.attachCanvas(canvas);
      h.setDevices(['A', 'B']);
      FakeWebSocket.instances.forEach((w) => w.open());
      return { canvas, ws: FakeWebSocket.instances.slice() };
    },
  },
];

for (const rig of rigs) {
  test(`${rig.name}: a key pressed on the focused screen goes to the phone`, () => {
    const { canvas, ws } = rig.setup();
    const down = canvas.fire('keydown', keyA);
    canvas.fire('keyup', keyA);
    assert.ok(down.defaultPrevented, 'browser default not suppressed on the screen');
    for (const w of ws) {
      assert.deepEqual(
        msgs(w).map((m) => [m.type, m.action, m.keycode]),
        [['key', 0, 29], ['key', 1, 29]],
      );
    }
  });

  test(`${rig.name}: keys typed with another control focused stay in the page`, () => {
    const { ws } = rig.setup();
    for (const k of [keyA, { code: 'Tab', key: 'Tab' }, { code: 'Enter', key: 'Enter' }, { code: 'Space', key: ' ' }]) {
      const down = fakeDocument.fire('keydown', k);
      const up = fakeDocument.fire('keyup', k);
      assert.equal(down.defaultPrevented, false, `${k.code} keydown swallowed`);
      assert.equal(up.defaultPrevented, false, `${k.code} keyup swallowed`);
    }
    for (const w of ws) assert.deepEqual(msgs(w), []);
  });

  test(`${rig.name}: a key held on the screen is released when focus moves away`, () => {
    const { canvas, ws } = rig.setup();
    canvas.fire('keydown', keyA);
    canvas.fire('blur');
    // Its keyup now lands on whatever took focus.
    fakeDocument.fire('keyup', keyA);
    for (const w of ws) {
      assert.deepEqual(
        msgs(w).map((m) => [m.type, m.action, m.keycode]),
        [['key', 0, 29], ['key', 1, 29]],
        'held key not released exactly once',
      );
    }
  });

  test(`${rig.name}: a keyup for a key pressed elsewhere sends nothing`, () => {
    const { canvas, ws } = rig.setup();
    fakeDocument.fire('keydown', keyA);
    canvas.fire('keyup', keyA);
    canvas.fire('blur');
    for (const w of ws) assert.deepEqual(msgs(w), []);
  });

  test(`${rig.name}: IME composition is never intercepted`, () => {
    const { canvas, ws } = rig.setup();
    const e = canvas.fire('keydown', { code: 'KeyA', key: 'a', isComposing: true });
    assert.equal(e.defaultPrevented, false);
    for (const w of ws) assert.deepEqual(msgs(w), []);
  });

  test(`${rig.name}: Shift+Esc hands focus back to the page instead of reaching the phone`, () => {
    const { canvas, ws } = rig.setup();
    const blurs = canvas.blurCalls;
    const e = canvas.fire('keydown', { code: 'Escape', key: 'Escape', shiftKey: true });
    assert.equal(canvas.blurCalls, blurs + 1, 'screen kept focus: keyboard users are trapped');
    assert.ok(e.defaultPrevented);
    for (const w of ws) assert.deepEqual(msgs(w), []);
    // Plain Esc is still Back on the phone (keycode 111).
    canvas.fire('keydown', { code: 'Escape', key: 'Escape' });
    for (const w of ws) assert.deepEqual(msgs(w).map((m) => m.keycode), [111]);
  });

  test(`${rig.name}: pressing the screen gives it keyboard focus`, () => {
    const { canvas } = rig.setup();
    canvas.fire('mousedown', { button: 0, clientX: 10, clientY: 10 });
    assert.ok(canvas.focusCalls > 0, 'screen not focused on press');
  });
}

test('viewer: Ctrl+V and Cmd+V paste to the phone only with the screen focused', () => {
  const { canvas } = rigs[0].setup();
  const outside = fakeDocument.fire('keydown', { code: 'KeyV', key: 'v', ctrlKey: true });
  assert.equal(outside.defaultPrevented, false, 'Ctrl+V hijacked outside the screen');
  assert.equal(clipboardReads, 0);
  canvas.fire('keydown', { code: 'KeyV', key: 'v', ctrlKey: true });
  canvas.fire('keydown', { code: 'KeyV', key: 'v', metaKey: true });
  assert.equal(clipboardReads, 2, 'paste not sent from the screen');
});

test('viewer: Ctrl+C and Cmd+C copy from the phone only with the screen focused', () => {
  const { canvas, ws } = rigs[0].setup();
  const outside = fakeDocument.fire('keydown', { code: 'KeyC', key: 'c', ctrlKey: true });
  assert.equal(outside.defaultPrevented, false, 'Ctrl+C hijacked outside the screen');
  assert.deepEqual(msgs(ws[0]), []);
  canvas.fire('keydown', { code: 'KeyC', key: 'c', ctrlKey: true });
  canvas.fire('keydown', { code: 'KeyC', key: 'c', metaKey: true });
  assert.deepEqual(msgs(ws[0]).map((m) => m.type), ['get_clipboard', 'get_clipboard']);
});
