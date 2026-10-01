// Fake WebCodecs/Web Audio pieces for testing the audio player under node.
import type { AudioDeps } from './device-audio';

export class FakeEncodedAudioChunk {
  constructor(public init: { type: string; timestamp: number; data: Uint8Array }) {}
}

export function installAudioEnv() {
  (globalThis as Record<string, unknown>).EncodedAudioChunk = FakeEncodedAudioChunk;
}

export class FakeAudioContext {
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

export class FakeAudioDecoder {
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

/** Player deps backed by fakes; created contexts/decoders are collected. */
export function fakeAudioDeps(opts: { allowResume?: boolean; supported?: boolean } = {}) {
  const contexts: FakeAudioContext[] = [];
  const decoders: FakeAudioDecoder[] = [];
  const deps: AudioDeps = {
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
  };
  return { deps, contexts, decoders };
}
