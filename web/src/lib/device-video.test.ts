import { test, beforeEach, afterEach, mock } from 'node:test';
import assert from 'node:assert/strict';
import { FakeWebSocket, installBrowserEnv, fakeCanvas, videoPacket } from './test-browser-env';

installBrowserEnv();
// eslint-disable-next-line @typescript-eslint/no-require-imports
const { DeviceVideoPlayer } = require('./device-video') as typeof import('./device-video');
// eslint-disable-next-line @typescript-eslint/no-require-imports
const { DeviceThumbnailPlayer } = require('./device-video-thumbnail') as typeof import('./device-video-thumbnail');

beforeEach(() => {
  FakeWebSocket.instances = [];
  mock.timers.enable({ apis: ['setTimeout'] });
});
afterEach(() => mock.timers.reset());

const sockets = () => FakeWebSocket.instances.length;
const last = () => FakeWebSocket.instances[FakeWebSocket.instances.length - 1];
const flush = () => new Promise<void>((r) => setImmediate(r));

for (const [name, Player] of [
  ['DeviceVideoPlayer', DeviceVideoPlayer],
  ['DeviceThumbnailPlayer', DeviceThumbnailPlayer],
] as const) {
  test(`${name} reconnects after the stream drops`, async () => {
    const p = new Player(fakeCanvas());
    const statuses: string[] = [];
    p.setOnStatusChange((s) => statuses.push(s));
    p.connect('A');
    last().open();
    last().drop();

    assert.ok(statuses.includes('reconnecting'), `statuses: ${statuses}`);
    assert.equal(sockets(), 1, 'reconnected without waiting');
    mock.timers.tick(30_000);
    await flush();
    assert.equal(sockets(), 2, 'did not reconnect');
    assert.match(last().url, /\/ws\/device\/A\/video\?token=tok$/);
  });

  test(`${name} does not reconnect after disconnect()`, async () => {
    const p = new Player(fakeCanvas());
    p.connect('A');
    const ws = last();
    ws.open();
    p.disconnect();
    ws.drop(); // the close event arrives after disconnect()
    mock.timers.tick(60_000);
    await flush();
    assert.equal(sockets(), 1);
  });

  test(`${name} backs off while the server is down, and resets once video flows`, async () => {
    const p = new Player(fakeCanvas());
    p.connect('A');
    // Failed attempt i must wait at least 0.5s * 2^i (backoff grows).
    for (let i = 0; i < 4; i++) {
      last().drop();
      const before = sockets();
      const minDelay = 500 * 2 ** i;
      mock.timers.tick(minDelay - 1);
      await flush();
      assert.equal(sockets(), before, `attempt ${i + 1} retried before ${minDelay}ms`);
      mock.timers.tick(30_000);
      await flush();
    }
    // Recovered: data flows, then a later drop retries quickly again.
    last().open();
    last().message(videoPacket());
    last().drop();
    const before = sockets();
    mock.timers.tick(1000);
    await flush();
    assert.equal(sockets(), before + 1, 'backoff not reset after recovery');
  });
}

test('DeviceVideoPlayer runs beforeReconnect (e.g. restart the session) before reopening', async () => {
  const order: string[] = [];
  const p = new DeviceVideoPlayer(fakeCanvas());
  p.setBeforeReconnect(async () => {
    order.push('before');
  });
  p.connect('A');
  last().open();
  last().drop();
  const origCount = sockets();
  mock.timers.tick(30_000);
  await flush();
  order.push(sockets() > origCount ? 'reopened' : 'not-reopened');
  assert.deepEqual(order, ['before', 'reopened']);
});

test('a stale socket closing does not trigger an extra reconnect', async () => {
  const p = new DeviceVideoPlayer(fakeCanvas());
  p.connect('A');
  const first = last();
  first.open();
  first.drop();
  mock.timers.tick(30_000);
  await flush();
  last().open();
  first.drop(); // late duplicate close from the old socket
  mock.timers.tick(60_000);
  await flush();
  assert.equal(sockets(), 2);
});

for (const [secure, want] of [[false, 'needs-https'], [true, 'unsupported']] as const) {
  test(`without WebCodecs on a ${secure ? 'secure' : 'plain-HTTP'} page the player reports ${want}`, () => {
    const w = (globalThis as unknown as { window: { isSecureContext?: boolean } }).window;
    w.isSecureContext = secure;
    try {
      for (const Player of [DeviceVideoPlayer, DeviceThumbnailPlayer]) {
        const statuses: string[] = [];
        const p = new Player(fakeCanvas());
        p.setOnStatusChange((s) => statuses.push(s));
        p.connect('A');
        last().open();
        assert.ok(statuses.includes(want), `${Player.name}: ${statuses}`);
        p.disconnect();
      }
    } finally {
      delete w.isSecureContext;
    }
  });
}
