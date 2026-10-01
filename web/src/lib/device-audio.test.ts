import { test, beforeEach, afterEach, mock } from 'node:test';
import assert from 'node:assert/strict';
import { FakeWebSocket, installBrowserEnv } from './test-browser-env';

installBrowserEnv();
class FakeEncodedAudioChunk {
  constructor(public init: { type: string; timestamp: number; data: Uint8Array }) {}
}
(globalThis as Record<string, unknown>).EncodedAudioChunk = FakeEncodedAudioChunk;
// eslint-disable-next-line @typescript-eslint/no-require-imports
const { DeviceAudioPlayer, JitterScheduler } = require('./device-audio') as typeof import('./device-audio');

class FakeAudioContext {
  state = 'suspended';
  currentTime = 10;
  destination = {};
  started: number[] = [];
  gains: { gain: { value: number } }[] = [];
  constructor(public allowResume: boolean) {}
  async resume() {
    if (this.allowResume) this.state = 'running';
  }
  async suspend() {
    this.state = 'suspended';
  }
  async close() {
    this.state = 'closed';
  }
  createGain() {
    const g = { gain: { value: 1 }, connect() {} };
    this.gains.push(g);
    return g;
  }
  createBuffer() {
    return { copyToChannel() {} };
  }
  createBufferSource() {
    return {
      buffer: null,
      onended: null,
      connect() {},
      stop() {},
      start: (t: number) => this.started.push(t),
    };
  }
}

class FakeAudioDecoder {
  state = 'unconfigured';
  config: { codec: string; description?: Uint8Array } | null = null;
  chunks: FakeEncodedAudioChunk[] = [];
  constructor(public init: { output: (d: unknown) => void; error: (e: Error) => void }) {}
  configure(config: { codec: string; description?: Uint8Array }) {
    this.config = config;
    this.state = 'configured';
  }
  decode(chunk: FakeEncodedAudioChunk) {
    this.chunks.push(chunk);
  }
  close() {
    this.state = 'closed';
  }
}

function setup(opts: { allowResume?: boolean; supported?: boolean } = {}) {
  const contexts: FakeAudioContext[] = [];
  const decoders: FakeAudioDecoder[] = [];
  const player = new DeviceAudioPlayer({
    supported: () => opts.supported ?? true,
    createContext: () => {
      const c = new FakeAudioContext(opts.allowResume ?? true);
      contexts.push(c);
      return c as unknown as AudioContext;
    },
    createDecoder: (init) => {
      const d = new FakeAudioDecoder(init as never);
      decoders.push(d);
      return d as unknown as AudioDecoder;
    },
  });
  const states: string[] = [];
  player.setOnStateChange((s) => states.push(s));
  return { player, contexts, decoders, states };
}

const OPUS_HEAD = new TextEncoder().encode('OpusHead\x01\x02\x38\x01\x80\xbb\x00\x00\x00\x00\x00');

function packet(payload: Uint8Array, opts: { config?: boolean; pts?: number } = {}): ArrayBuffer {
  const buf = new ArrayBuffer(12 + payload.length);
  const view = new DataView(buf);
  if (opts.config) view.setUint32(0, 0x80000000);
  else view.setUint32(4, opts.pts ?? 0);
  view.setUint32(8, payload.length);
  new Uint8Array(buf, 12).set(payload);
  return buf;
}

const last = () => FakeWebSocket.instances[FakeWebSocket.instances.length - 1];
const flush = () => new Promise<void>((r) => setImmediate(r));
const status = (s: object) => JSON.stringify(s);

beforeEach(() => {
  FakeWebSocket.instances = [];
  mock.timers.enable({ apis: ['setTimeout'] });
});
afterEach(() => mock.timers.reset());

test('connects to the audio endpoint and reports available audio', () => {
  const { player, states } = setup();
  player.connect('A');
  assert.match(last().url, /\/ws\/device\/A\/audio\?token=tok$/);
  last().open();
  last().message(status({ available: true, codec: 'opus' }));
  assert.equal(states.at(-1), 'available');
});

test('unmuted audio is decoded with the OpusHead and scheduled through the gain', async () => {
  const { player, contexts, decoders } = setup();
  player.connect('A');
  last().open();
  last().message(status({ available: true, codec: 'opus' }));
  last().message(packet(OPUS_HEAD, { config: true }));
  assert.equal(await player.setMuted(false), true);

  last().message(packet(new Uint8Array([0xfc, 0xff, 0xfe]), { pts: 20000 }));
  const dec = decoders[0];
  assert.ok(dec, 'no decoder created');
  assert.equal(dec.config?.codec, 'opus');
  assert.deepEqual(Array.from(dec.config?.description ?? []), Array.from(OPUS_HEAD));
  assert.equal(dec.chunks.length, 1);
  assert.equal(dec.chunks[0].init.timestamp, 20000);
  assert.deepEqual(Array.from(dec.chunks[0].init.data), [0xfc, 0xff, 0xfe]);

  // 20ms of decoded audio is played a small jitter buffer ahead of now.
  let closed = false;
  dec.init.output({
    numberOfFrames: 960,
    sampleRate: 48000,
    numberOfChannels: 2,
    copyTo() {},
    close: () => (closed = true),
  });
  const ctx = contexts[0];
  assert.equal(ctx.started.length, 1);
  assert.ok(ctx.started[0] > ctx.currentTime && ctx.started[0] < ctx.currentTime + 0.25);
  assert.ok(closed, 'AudioData not released');
});

test('muted audio is not decoded', () => {
  const { player, decoders } = setup();
  player.connect('A');
  last().open();
  last().message(status({ available: true, codec: 'opus' }));
  last().message(packet(OPUS_HEAD, { config: true }));
  last().message(packet(new Uint8Array([1, 2, 3]), { pts: 20000 }));
  assert.equal(decoders.length, 0);
});

test('unmuting when the browser blocks autoplay reports blocked and stays muted', async () => {
  const { player, decoders, states } = setup({ allowResume: false });
  player.connect('A');
  last().open();
  last().message(status({ available: true, codec: 'opus' }));
  assert.equal(await player.setMuted(false), false);
  assert.equal(states.at(-1), 'blocked');
  last().message(packet(OPUS_HEAD, { config: true }));
  last().message(packet(new Uint8Array([1]), { pts: 1 }));
  assert.equal(decoders.length, 0);
});

test('volume is applied to the gain node, including before unmuting', async () => {
  const { player, contexts } = setup();
  player.setVolume(0.3);
  await player.setMuted(false);
  const gain = contexts[0].gains[0].gain;
  assert.equal(gain.value, 0.3);
  player.setVolume(0.8);
  assert.equal(gain.value, 0.8);
});

test('unavailable audio is reported with its reason and retried with backoff', async () => {
  const { player, states } = setup();
  const reasons: (string | undefined)[] = [];
  player.setOnStateChange((s, reason) => {
    states.push(s);
    reasons.push(reason);
  });
  player.connect('A');
  last().open();
  last().message(status({ available: false, reason: 'needs Android 11' }));
  last().drop(); // the server closes after the status
  assert.equal(states.at(-1), 'unavailable');
  assert.equal(reasons.at(-1), 'needs Android 11');
  assert.equal(FakeWebSocket.instances.length, 1);
  mock.timers.tick(30_000);
  await flush();
  assert.equal(FakeWebSocket.instances.length, 2, 'never retried (session may gain audio after a restart)');
});

test('a dropped stream reconnects; disconnect() stops for good', async () => {
  const { player, states } = setup();
  player.connect('A');
  last().open();
  last().drop();
  assert.equal(states.at(-1), 'reconnecting');
  mock.timers.tick(30_000);
  await flush();
  assert.equal(FakeWebSocket.instances.length, 2);

  player.disconnect();
  last().drop();
  mock.timers.tick(60_000);
  await flush();
  assert.equal(FakeWebSocket.instances.length, 2);
});

test('browsers without WebCodecs audio report unsupported instead of failing', () => {
  const { player, states } = setup({ supported: false });
  player.connect('A');
  last().open();
  last().message(status({ available: true, codec: 'opus' }));
  assert.equal(states.at(-1), 'unsupported');
});

test('jitter scheduler buffers ahead, rebuffers after underrun, drops when too far behind', () => {
  const s = new JitterScheduler(0.06, 0.25);
  assert.equal(s.next(10, 0.02), 10.06);
  assert.equal(s.next(10, 0.02), 10.08);
  // Underrun: playback caught up with the queue, so buffer ahead again.
  assert.equal(s.next(11, 0.02), 11.06);
  // Burst after a stall: latency is capped by dropping chunks.
  let dropped = 0;
  for (let i = 0; i < 30; i++) if (s.next(11, 0.02) === null) dropped++;
  assert.ok(dropped > 0, 'never dropped');
  const t = s.next(11, 0.02);
  assert.ok(t === null || t <= 11.25 + 0.02);
});
