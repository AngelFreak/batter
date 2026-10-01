import { getToken } from './auth';
import { Reconnector } from './reconnect';

const WS_BASE_URL = process.env.NEXT_PUBLIC_WS_URL || "";

/**
 * Lightweight video player for grid thumbnails.
 * Same H.264 decoding pipeline as DeviceVideoPlayer but optimized for
 * low overhead: no FPS tracking, minimal state.
 */
export class DeviceThumbnailPlayer {
  private canvas: HTMLCanvasElement;
  private ctx: CanvasRenderingContext2D;
  private ws: WebSocket | null = null;
  private decoder: VideoDecoder | null = null;
  private sps: Uint8Array | null = null;
  private pps: Uint8Array | null = null;
  private configured = false;
  private onStatusChange: ((status: string) => void) | null = null;
  private serial = "";
  private stopped = true;
  private reconnector = new Reconnector(() => { if (!this.stopped) this.open(); });

  constructor(canvas: HTMLCanvasElement) {
    this.canvas = canvas;
    const ctx = canvas.getContext("2d");
    if (!ctx) throw new Error("Failed to get 2d context");
    this.ctx = ctx;
  }

  setOnStatusChange(cb: (status: string) => void) {
    this.onStatusChange = cb;
  }

  /** Stream video for serial, reconnecting with backoff until disconnect(). */
  connect(serial: string) {
    this.serial = serial;
    this.stopped = false;
    this.reconnector.reset();
    this.open();
  }

  private open() {
    const token = getToken();
    const base =
      WS_BASE_URL ||
      `${window.location.protocol === "https:" ? "wss:" : "ws:"}//${window.location.host}`;
    const url = `${base}/ws/device/${encodeURIComponent(this.serial)}/video?token=${encodeURIComponent(token || '')}`;

    const ws = new WebSocket(url);
    this.ws = ws;
    ws.binaryType = "arraybuffer";

    // Handlers ignore events from sockets that have since been replaced.
    ws.onopen = () => {
      if (this.ws !== ws) return;
      this.onStatusChange?.("connected");
      this.initDecoder();
    };

    ws.onmessage = (event) => {
      if (this.ws !== ws) return;
      this.reconnector.reset();
      this.handleMessage(event.data as ArrayBuffer);
    };

    ws.onclose = () => {
      if (this.ws !== ws) return;
      this.ws = null;
      this.resetDecoder();
      // Another viewer upgrading/downgrading the session, or a backend
      // restart, ends the stream; reconnect rather than freeze the tile.
      this.onStatusChange?.("reconnecting");
      this.reconnector.schedule();
    };

    ws.onerror = () => {
      if (this.ws !== ws) return;
      this.onStatusChange?.("error");
    };
  }

  private initDecoder() {
    if (!("VideoDecoder" in window)) {
      this.onStatusChange?.("unsupported");
      return;
    }

    this.decoder = new VideoDecoder({
      output: (frame) => {
        if (this.canvas.width !== frame.displayWidth || this.canvas.height !== frame.displayHeight) {
          this.canvas.width = frame.displayWidth;
          this.canvas.height = frame.displayHeight;
        }
        this.ctx.drawImage(frame, 0, 0);
        frame.close();
      },
      error: () => {
        // Decoder hit a fatal error — replace it and reconfigure from the
        // stored SPS/PPS on the next packet
        this.configured = false;
        try { this.decoder?.close(); } catch { /* already closed */ }
        this.initDecoder();
      },
    });
  }

  private handleMessage(data: ArrayBuffer) {
    if (data.byteLength < 12) return;

    const view = new DataView(data);
    const ptsHigh = view.getUint32(0);
    const ptsLow = view.getUint32(4);
    const isConfig = (ptsHigh >>> 31) & 1;
    const isKeyFrame = (ptsHigh >>> 30) & 1;
    const pts = ((ptsHigh & 0x3fffffff) * 0x100000000 + ptsLow);
    const naluData = new Uint8Array(data, 12);

    if (isConfig) {
      this.parseConfig(naluData);
      return;
    }

    if (!this.decoder || this.decoder.state === "closed") return;

    if (!this.configured && this.sps && this.pps) {
      this.configure();
    }

    if (!this.configured) return;

    try {
      const avcc = this.toAVCC(naluData);
      this.decoder.decode(new EncodedVideoChunk({
        type: isKeyFrame ? "key" : "delta",
        timestamp: pts,
        data: avcc,
      }));
    } catch {
      // silently skip decode errors for thumbnails
    }
  }

  private parseConfig(data: Uint8Array) {
    let i = 0;
    while (i < data.length) {
      let scLen = 0;
      if (i + 3 < data.length && data[i] === 0 && data[i+1] === 0 && data[i+2] === 0 && data[i+3] === 1) scLen = 4;
      else if (i + 2 < data.length && data[i] === 0 && data[i+1] === 0 && data[i+2] === 1) scLen = 3;

      if (scLen > 0) {
        const start = i + scLen;
        let end = data.length;
        for (let j = start + 1; j < data.length - 2; j++) {
          if (data[j] === 0 && data[j+1] === 0 && (data[j+2] === 1 || (j+3 < data.length && data[j+2] === 0 && data[j+3] === 1))) {
            end = j; break;
          }
        }
        const nalu = data.slice(start, end);
        const type = nalu[0] & 0x1f;
        if (type === 7) this.sps = nalu;
        else if (type === 8) this.pps = nalu;
        i = end;
      } else {
        i++;
      }
    }
  }

  private configure() {
    if (!this.decoder || !this.sps || !this.pps) return;
    const desc = new Uint8Array(11 + this.sps.length + this.pps.length);
    let o = 0;
    desc[o++] = 1; desc[o++] = this.sps[1]; desc[o++] = this.sps[2]; desc[o++] = this.sps[3]; desc[o++] = 0xff;
    desc[o++] = 0xe1; desc[o++] = (this.sps.length >> 8) & 0xff; desc[o++] = this.sps.length & 0xff;
    desc.set(this.sps, o); o += this.sps.length;
    desc[o++] = 1; desc[o++] = (this.pps.length >> 8) & 0xff; desc[o++] = this.pps.length & 0xff;
    desc.set(this.pps, o);

    // Derive codec string from SPS: avc1.PPCCLL
    const profile = this.sps[1].toString(16).padStart(2, '0');
    const compat = this.sps[2].toString(16).padStart(2, '0');
    const level = this.sps[3].toString(16).padStart(2, '0');
    const codec = `avc1.${profile}${compat}${level}`;

    try {
      this.decoder.configure({ codec, description: desc, optimizeForLatency: true });
      this.configured = true;
      this.onStatusChange?.("streaming");
    } catch {
      this.onStatusChange?.("decoder-error");
    }
  }

  private toAVCC(data: Uint8Array): Uint8Array {
    const nalus: Uint8Array[] = [];
    let i = 0;
    while (i < data.length) {
      let scLen = 0;
      if (i + 3 < data.length && data[i] === 0 && data[i+1] === 0 && data[i+2] === 0 && data[i+3] === 1) scLen = 4;
      else if (i + 2 < data.length && data[i] === 0 && data[i+1] === 0 && data[i+2] === 1) scLen = 3;
      if (scLen > 0) {
        const start = i + scLen;
        let end = data.length;
        for (let j = start + 1; j < data.length - 2; j++) {
          if (data[j] === 0 && data[j+1] === 0 && (data[j+2] === 1 || (j+3 < data.length && data[j+2] === 0 && data[j+3] === 1))) {
            end = j; break;
          }
        }
        nalus.push(data.slice(start, end));
        i = end;
      } else {
        i++;
      }
    }
    if (nalus.length === 0) return data;
    let total = 0;
    for (const n of nalus) total += 4 + n.length;
    const result = new Uint8Array(total);
    let off = 0;
    for (const n of nalus) {
      result[off] = (n.length >> 24) & 0xff;
      result[off+1] = (n.length >> 16) & 0xff;
      result[off+2] = (n.length >> 8) & 0xff;
      result[off+3] = n.length & 0xff;
      off += 4;
      result.set(n, off);
      off += n.length;
    }
    return result;
  }

  disconnect() {
    this.stopped = true;
    this.reconnector.cancel();
    const ws = this.ws;
    this.ws = null;
    ws?.close();
    this.resetDecoder();
  }

  private resetDecoder() {
    if (this.decoder && this.decoder.state !== "closed") {
      try { this.decoder.close(); } catch { /* ignore */ }
    }
    this.decoder = null;
    this.configured = false;
    this.sps = null;
    this.pps = null;
  }
}
