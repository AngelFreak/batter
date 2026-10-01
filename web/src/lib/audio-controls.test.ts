import { test, beforeEach, afterEach, mock } from 'node:test';
import assert from 'node:assert/strict';
import { FakeWebSocket, installBrowserEnv } from './test-browser-env';
import { installAudioEnv, fakeAudioDeps } from './test-audio-env';

installBrowserEnv();
installAudioEnv();
// eslint-disable-next-line @typescript-eslint/no-require-imports
const { DeviceAudioPlayer } = require('./device-audio') as typeof import('./device-audio');
// eslint-disable-next-line @typescript-eslint/no-require-imports
const { AudioControls } = require('./audio-controls') as typeof import('./audio-controls');
type AudioView = import('./audio-controls').AudioView;

function memoryStorage(initial?: object) {
  const data = new Map<string, string>();
  if (initial) data.set('batter.audio', JSON.stringify(initial));
  return {
    data,
    getItem: (k: string) => data.get(k) ?? null,
    setItem: (k: string, v: string) => void data.set(k, v),
  };
}

// Real player + controls; only the browser APIs are faked.
function setup(opts: { stored?: object; allowResume?: boolean } = {}) {
  const fakes = fakeAudioDeps({ allowResume: opts.allowResume });
  const storage = memoryStorage(opts.stored);
  const views: AudioView[] = [];
  const controls = new AudioControls(new DeviceAudioPlayer(fakes.deps), (v) => views.push(v), storage);
  controls.start('A');
  const ws = FakeWebSocket.instances[FakeWebSocket.instances.length - 1];
  ws.open();
  ws.message(JSON.stringify({ available: true, codec: 'opus' }));
  const view = () => views[views.length - 1];
  const stored = () => JSON.parse(storage.data.get('batter.audio') ?? 'null');
  return { controls, view, stored, ...fakes };
}

const flush = () => new Promise<void>((r) => setImmediate(r));

beforeEach(() => {
  FakeWebSocket.instances = [];
  mock.timers.enable({ apis: ['setTimeout'] });
});
afterEach(() => mock.timers.reset());

test('a new browser starts muted, without touching Web Audio, until the user clicks', async () => {
  const { controls, view, contexts, stored } = setup();
  await flush();
  assert.equal(view().muted, true);
  assert.equal(view().state, 'available');
  assert.equal(contexts.length, 0, 'AudioContext created before any click');

  await controls.toggleMute();
  assert.equal(view().muted, false);
  assert.equal(contexts[0].state, 'running');
  assert.deepEqual(stored(), { muted: false, volume: 1 });

  await controls.toggleMute();
  assert.equal(view().muted, true);
  assert.deepEqual(stored(), { muted: true, volume: 1 });
});

test('remembered unmute resumes playback when the browser allows it', async () => {
  const { view } = setup({ stored: { muted: false, volume: 0.5 } });
  await flush();
  assert.equal(view().muted, false);
  assert.equal(view().volume, 0.5);
});

test('remembered unmute blocked by autoplay shows muted but keeps the preference', async () => {
  const { view, stored } = setup({ stored: { muted: false, volume: 0.5 }, allowResume: false });
  await flush();
  assert.equal(view().muted, true);
  assert.equal(view().state, 'blocked');
  assert.deepEqual(stored(), { muted: false, volume: 0.5 });
});

test('the volume slider sets the gain and is remembered', async () => {
  const { controls, contexts, stored } = setup();
  await controls.toggleMute();
  controls.setVolume(0.25);
  assert.equal(contexts[0].gains[0].gain.value, 0.25);
  assert.equal(stored().volume, 0.25);
});

test('stop() closes the audio socket for good', async () => {
  const { controls } = setup();
  const ws = FakeWebSocket.instances[0];
  controls.stop();
  ws.drop();
  mock.timers.tick(60_000);
  await flush();
  assert.equal(FakeWebSocket.instances.length, 1);
});
