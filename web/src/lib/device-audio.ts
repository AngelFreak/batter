import { getToken } from './auth';
import { Reconnector } from './reconnect';

const WS_BASE_URL = process.env.NEXT_PUBLIC_WS_URL || "";

/**
 * connecting/reconnecting: no answer from the server yet.
 * available: the device streams audio (whether or not we are muted).
 * unavailable: the device can't stream audio (reason says why).
 * unsupported/needs-https: this browser can't decode it.
 * blocked: the browser refused to start playback without a user gesture.
 */
export type AudioState =
  | "connecting"
  | "reconnecting"
  | "available"
  | "unavailable"
  | "unsupported"
  | "needs-https"
  | "blocked";

export interface AudioDeps {
  supported: () => boolean;
  createContext: () => AudioContext;
  createDecoder: (init: AudioDecoderInit) => AudioDecoder;
}

const browserDeps: AudioDeps = {
  supported: () => typeof AudioDecoder !== "undefined" && typeof AudioContext !== "undefined",
  createContext: () => new AudioContext({ latencyHint: "interactive", sampleRate: 48000 }),
  createDecoder: (init) => new AudioDecoder(init),
};

/**
 * Picks when each decoded chunk plays: back to back, a little ahead of now so
 * network jitter doesn't cause gaps, and never so far ahead that audio lags
 * the video (chunks beyond maxAhead are dropped).
 */
export class JitterScheduler {
  private nextTime = 0;

  constructor(
    private readonly target = 0.06,
    private readonly maxAhead = 0.25,
  ) {}

  /** Start time (AudioContext seconds) for a chunk, or null to drop it. */
  next(now: number, duration: number): number | null {
    if (this.nextTime <= now) {
      this.nextTime = now + this.target; // underrun (or first chunk): rebuffer
    } else if (this.nextTime > now + this.maxAhead) {
      return null;
    }
    const start = this.nextTime;
    this.nextTime += duration;
    return start;
  }

  reset() {
    this.nextTime = 0;
  }
}

/**
 * Plays a device's audio from /ws/device/:serial/audio: a JSON status, then
 * scrcpy packets (12-byte header + Opus) decoded with WebCodecs and played
 * through a gain node. Starts muted; nothing is decoded while muted.
 */
export class DeviceAudioPlayer {
  private ws: WebSocket | null = null;
  private serial = "";
  private stopped = true;
  private state: AudioState = "connecting";
  private onStateChange: ((state: AudioState, reason?: string) => void) | null = null;
  private reconnector = new Reconnector(() => {
    if (!this.stopped) this.open();
  });

  private muted = true;
  private volume = 1;
  private ctx: AudioContext | null = null;
  private gain: GainNode | null = null;
  private decoder: AudioDecoder | null = null;
  private description: Uint8Array | null = null; // OpusHead from the config packet
  private scheduler = new JitterScheduler();
  private sources = new Set<AudioBufferSourceNode>();

  constructor(private readonly deps: AudioDeps = browserDeps) {}

  setOnStateChange(cb: (state: AudioState, reason?: string) => void) {
    this.onStateChange = cb;
  }

  /** Listen to serial's audio, reconnecting with backoff until disconnect(). */
  connect(serial: string) {
    this.serial = serial;
    this.stopped = false;
    this.reconnector.reset();
    this.open();
  }

  disconnect() {
    this.stopped = true;
    this.reconnector.cancel();
    const ws = this.ws;
    this.ws = null;
    ws?.close();
    this.stopPlayback();
    void this.ctx?.close().catch(() => {});
    this.ctx = null;
    this.gain = null;
  }

  /**
   * Mutes or unmutes. Unmuting must run from a user gesture the first time;
   * resolves false (state "blocked") if the browser still refuses to play.
   */
  async setMuted(muted: boolean): Promise<boolean> {
    if (muted) {
      this.muted = true;
      this.stopPlayback();
      void this.ctx?.suspend().catch(() => {});
      return true;
    }
    if (!this.deps.supported()) {
      this.setState(window.isSecureContext === false ? "needs-https" : "unsupported");
      return false;
    }
    if (!this.ctx) {
      this.ctx = this.deps.createContext();
      this.gain = this.ctx.createGain();
      this.gain.gain.value = this.volume;
      this.gain.connect(this.ctx.destination);
    }
    try {
      await this.ctx.resume();
    } catch {
      // treated as blocked below
    }
    if (this.ctx.state !== "running") {
      this.setState("blocked");
      return false;
    }
    this.muted = false;
    if (this.state === "blocked") this.setState("available");
    return true;
  }

  /** Sets the browser-side volume, 0..1. */
  setVolume(volume: number) {
    this.volume = Math.min(1, Math.max(0, volume));
    if (this.gain) this.gain.gain.value = this.volume;
  }

  private setState(state: AudioState, reason?: string) {
    this.state = state;
    this.onStateChange?.(state, reason);
  }

  private open() {
    const token = getToken();
    const base =
      WS_BASE_URL ||
      `${window.location.protocol === "https:" ? "wss:" : "ws:"}//${window.location.host}`;
    const url = `${base}/ws/device/${encodeURIComponent(this.serial)}/audio?token=${encodeURIComponent(token || "")}`;

    const ws = new WebSocket(url);
    this.ws = ws;
    ws.binaryType = "arraybuffer";

    // Handlers ignore events from sockets that have since been replaced.
    ws.onmessage = (event) => {
      if (this.ws !== ws) return;
      if (typeof event.data === "string") {
        this.handleStatus(event.data);
      } else {
        this.reconnector.reset();
        this.handlePacket(event.data as ArrayBuffer);
      }
    };

    ws.onclose = () => {
      if (this.ws !== ws) return;
      this.ws = null;
      this.stopPlayback();
      // Sessions restart on quality changes, and may gain audio then, so
      // keep retrying even when audio was unavailable.
      if (this.state !== "unavailable") this.setState("reconnecting");
      this.reconnector.schedule();
    };
  }

  private handleStatus(text: string) {
    let status: { available?: boolean; reason?: string };
    try {
      status = JSON.parse(text);
    } catch {
      return;
    }
    if (!status.available) {
      this.setState("unavailable", status.reason);
    } else if (!this.deps.supported()) {
      this.setState(window.isSecureContext === false ? "needs-https" : "unsupported");
    } else {
      this.setState("available");
    }
  }

  private handlePacket(data: ArrayBuffer) {
    if (data.byteLength < 12) return;
    const view = new DataView(data);
    const ptsHigh = view.getUint32(0);
    const ptsLow = view.getUint32(4);
    const payload = new Uint8Array(data, 12);

    if (ptsHigh >>> 31) {
      // Config packet: the OpusHead the decoder is configured with.
      this.description = payload.slice();
      this.closeDecoder();
      return;
    }
    if (this.muted || !this.ctx) return;

    const decoder = this.ensureDecoder();
    if (!decoder) return;
    // PTS is in microseconds, below the two flag bits.
    const timestamp = (ptsHigh & 0x3fffffff) * 2 ** 32 + ptsLow;
    decoder.decode(new EncodedAudioChunk({ type: "key", timestamp, data: payload }));
  }

  private ensureDecoder(): AudioDecoder | null {
    if (this.decoder) return this.decoder;
    try {
      const decoder = this.deps.createDecoder({
        output: (audio) => this.play(audio),
        error: (err) => {
          console.error("AudioDecoder error:", err);
          if (this.decoder === decoder) this.decoder = null; // recreated on the next packet
        },
      });
      decoder.configure({
        codec: "opus",
        sampleRate: 48000,
        numberOfChannels: 2,
        ...(this.description ? { description: this.description } : {}),
      });
      this.decoder = decoder;
      return decoder;
    } catch (err) {
      console.error("AudioDecoder setup failed:", err);
      this.setState("unsupported");
      return null;
    }
  }

  private play(audio: AudioData) {
    const ctx = this.ctx;
    const gain = this.gain;
    try {
      if (!ctx || !gain || this.muted) return;
      const start = this.scheduler.next(ctx.currentTime, audio.numberOfFrames / audio.sampleRate);
      if (start === null) return;

      const buffer = ctx.createBuffer(audio.numberOfChannels, audio.numberOfFrames, audio.sampleRate);
      for (let ch = 0; ch < audio.numberOfChannels; ch++) {
        const plane = new Float32Array(audio.numberOfFrames);
        audio.copyTo(plane, { planeIndex: ch, format: "f32-planar" });
        buffer.copyToChannel(plane, ch);
      }
      const source = ctx.createBufferSource();
      source.buffer = buffer;
      source.connect(gain);
      source.onended = () => this.sources.delete(source);
      this.sources.add(source);
      source.start(start);
    } finally {
      audio.close();
    }
  }

  private closeDecoder() {
    if (this.decoder && this.decoder.state !== "closed") {
      try {
        this.decoder.close();
      } catch {
        // already closed
      }
    }
    this.decoder = null;
  }

  /** Drops queued audio and the decoder, e.g. on mute or a lost stream. */
  private stopPlayback() {
    this.closeDecoder();
    for (const source of this.sources) {
      try {
        source.stop();
      } catch {
        // not started yet
      }
    }
    this.sources.clear();
    this.scheduler.reset();
  }
}
