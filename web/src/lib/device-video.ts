import { getToken } from './auth';
import { Reconnector } from './reconnect';

const WS_BASE_URL = process.env.NEXT_PUBLIC_WS_URL || "";

export class DeviceVideoPlayer {
  private canvas: HTMLCanvasElement;
  private ctx: CanvasRenderingContext2D;
  private ws: WebSocket | null = null;
  private decoder: VideoDecoder | null = null;
  private sps: Uint8Array | null = null;
  private pps: Uint8Array | null = null;
  private configured = false;
  private frameCount = 0;
  private lastFpsTime = 0;
  private _fps = 0;
  private onFpsUpdate: ((fps: number) => void) | null = null;
  private onStatusChange: ((status: string) => void) | null = null;
  private serial = "";
  private stopped = true;
  private beforeReconnect: (() => Promise<unknown>) | null = null;
  private reconnector = new Reconnector(() => void this.reopen());

  constructor(canvas: HTMLCanvasElement) {
    this.canvas = canvas;
    const ctx = canvas.getContext("2d");
    if (!ctx) throw new Error("Failed to get 2d context");
    this.ctx = ctx;
  }

  get fps(): number {
    return this._fps;
  }

  setOnFpsUpdate(cb: (fps: number) => void) {
    this.onFpsUpdate = cb;
  }

  setOnStatusChange(cb: (status: string) => void) {
    this.onStatusChange = cb;
  }

  /**
   * Hook awaited before each reconnect attempt, e.g. to restart the device
   * session after a backend restart (the video endpoint 404s without one).
   * Errors are ignored; the attempt proceeds and backs off if it fails.
   */
  setBeforeReconnect(fn: () => Promise<unknown>) {
    this.beforeReconnect = fn;
  }

  /** Stream video for serial, reconnecting with backoff until disconnect(). */
  connect(serial: string) {
    this.serial = serial;
    this.stopped = false;
    this.reconnector.reset();
    this.open();
  }

  private async reopen() {
    if (this.beforeReconnect) {
      try {
        await this.beforeReconnect();
      } catch {
        // proceed; a failed attempt just backs off further
      }
    }
    if (!this.stopped) this.open();
  }

  private open() {
    const token = getToken();
    const base =
      WS_BASE_URL ||
      `${window.location.protocol === "https:" ? "wss:" : "ws:"}//${window.location.host}`;
    const url = `${base}/ws/device/${encodeURIComponent(this.serial)}/video?token=${encodeURIComponent(token || '')}`;

    this.onStatusChange?.("connecting");

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
      this.handleVideoMessage(event.data as ArrayBuffer);
    };

    ws.onclose = () => {
      if (this.ws !== ws) return;
      this.ws = null;
      this.resetDecoder();
      // Sessions restart on quality changes and backend restarts, which
      // closes the stream; reconnect rather than leave a frozen frame.
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
      output: (frame) => this.handleFrame(frame),
      error: (err) => {
        console.error("VideoDecoder error:", err);
        this.onStatusChange?.("decoder-error");
      },
    });
  }

  private handleVideoMessage(data: ArrayBuffer) {
    if (data.byteLength < 12) return;

    const view = new DataView(data);

    // Parse 12-byte header
    const ptsHigh = view.getUint32(0);
    const ptsLow = view.getUint32(4);

    // Flags in PTS MSBs
    const isConfig = (ptsHigh >>> 31) & 1;
    const isKeyFrame = (ptsHigh >>> 30) & 1;

    // Extract PTS (clear flag bits)
    const pts = ((ptsHigh & 0x3fffffff) * 0x100000000 + ptsLow);

    // NALU data starts at offset 12
    const naluData = new Uint8Array(data, 12);

    if (isConfig) {
      this.parseConfigPacket(naluData);
      return;
    }

    if (!this.decoder || this.decoder.state === "closed") return;

    if (!this.configured && this.sps && this.pps) {
      this.configureDecoder();
    }

    if (!this.configured) return;

    try {
      const avccData = this.annexBToAVCC(naluData);
      const chunk = new EncodedVideoChunk({
        type: isKeyFrame ? "key" : "delta",
        timestamp: pts,
        data: avccData,
      });
      this.decoder.decode(chunk);
    } catch (err) {
      console.debug("decode error:", err);
    }

    // FPS counter
    this.frameCount++;
    const now = performance.now();
    if (now - this.lastFpsTime >= 1000) {
      this._fps = this.frameCount;
      this.frameCount = 0;
      this.lastFpsTime = now;
      this.onFpsUpdate?.(this._fps);
    }
  }

  private parseConfigPacket(data: Uint8Array) {
    const nalus = this.findNALUs(data);

    for (const nalu of nalus) {
      const naluType = nalu[0] & 0x1f;
      if (naluType === 7) {
        this.sps = nalu;
      } else if (naluType === 8) {
        this.pps = nalu;
      }
    }
  }

  private findNALUs(data: Uint8Array): Uint8Array[] {
    const nalus: Uint8Array[] = [];
    let i = 0;

    while (i < data.length) {
      let startCodeLen = 0;
      if (
        i + 3 < data.length &&
        data[i] === 0 &&
        data[i + 1] === 0 &&
        data[i + 2] === 0 &&
        data[i + 3] === 1
      ) {
        startCodeLen = 4;
      } else if (
        i + 2 < data.length &&
        data[i] === 0 &&
        data[i + 1] === 0 &&
        data[i + 2] === 1
      ) {
        startCodeLen = 3;
      }

      if (startCodeLen > 0) {
        const naluStart = i + startCodeLen;
        let naluEnd = data.length;
        for (let j = naluStart + 1; j < data.length - 2; j++) {
          if (
            data[j] === 0 &&
            data[j + 1] === 0 &&
            (data[j + 2] === 1 || (j + 3 < data.length && data[j + 2] === 0 && data[j + 3] === 1))
          ) {
            naluEnd = j;
            break;
          }
        }
        nalus.push(data.slice(naluStart, naluEnd));
        i = naluEnd;
      } else {
        i++;
      }
    }

    if (nalus.length === 0 && data.length > 0) {
      nalus.push(data);
    }

    return nalus;
  }

  private annexBToAVCC(data: Uint8Array): Uint8Array {
    const nalus = this.findNALUs(data);
    if (nalus.length === 0) return data;

    let totalSize = 0;
    for (const nalu of nalus) {
      totalSize += 4 + nalu.length;
    }

    const result = new Uint8Array(totalSize);
    let offset = 0;
    for (const nalu of nalus) {
      result[offset] = (nalu.length >> 24) & 0xff;
      result[offset + 1] = (nalu.length >> 16) & 0xff;
      result[offset + 2] = (nalu.length >> 8) & 0xff;
      result[offset + 3] = nalu.length & 0xff;
      offset += 4;
      result.set(nalu, offset);
      offset += nalu.length;
    }
    return result;
  }

  private configureDecoder() {
    if (!this.decoder || !this.sps || !this.pps) return;

    const description = this.buildAVCCDescription(this.sps, this.pps);

    // Derive codec string from SPS: avc1.PPCCLL
    const profile = this.sps[1].toString(16).padStart(2, '0');
    const compat = this.sps[2].toString(16).padStart(2, '0');
    const level = this.sps[3].toString(16).padStart(2, '0');
    const codec = `avc1.${profile}${compat}${level}`;

    try {
      this.decoder.configure({
        codec,
        description: description,
        optimizeForLatency: true,
      });
      this.configured = true;
      this.onStatusChange?.("streaming");
    } catch (err) {
      console.error("Failed to configure decoder:", err);
      this.onStatusChange?.("decoder-error");
    }
  }

  private buildAVCCDescription(sps: Uint8Array, pps: Uint8Array): Uint8Array {
    const length = 11 + sps.length + pps.length;
    const buf = new Uint8Array(length);
    let offset = 0;

    buf[offset++] = 1;
    buf[offset++] = sps[1];
    buf[offset++] = sps[2];
    buf[offset++] = sps[3];
    buf[offset++] = 0xff;

    buf[offset++] = 0xe1;
    buf[offset++] = (sps.length >> 8) & 0xff;
    buf[offset++] = sps.length & 0xff;
    buf.set(sps, offset);
    offset += sps.length;

    buf[offset++] = 1;
    buf[offset++] = (pps.length >> 8) & 0xff;
    buf[offset++] = pps.length & 0xff;
    buf.set(pps, offset);

    return buf;
  }

  private handleFrame(frame: VideoFrame) {
    if (this.canvas.width !== frame.displayWidth || this.canvas.height !== frame.displayHeight) {
      this.canvas.width = frame.displayWidth;
      this.canvas.height = frame.displayHeight;
    }
    this.ctx.drawImage(frame, 0, 0);
    frame.close();
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
      try {
        this.decoder.close();
      } catch {
        // Ignore close errors
      }
    }
    this.decoder = null;
    this.configured = false;
    this.sps = null;
    this.pps = null;
  }
}
