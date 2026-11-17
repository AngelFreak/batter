import { CanvasKeys } from "./canvas-keys";
import { getToken } from "./auth";
import { WheelGestures } from "./wheel-gesture";
import { NavAction, navMessages } from "./device-nav";

const WS_BASE_URL = process.env.NEXT_PUBLIC_WS_URL || "";

interface ControlMessage {
  type: string;
  action?: number;
  x?: number;
  y?: number;
  pointer_id?: number;
  pressure?: number;
  keycode?: number;
  repeat?: number;
  metastate?: number;
  text?: string;
  scroll_h?: number;
  scroll_v?: number;
}

interface DeviceConnection {
  serial: string;
  ws: WebSocket;
  hasControl: boolean;
}

export class MultiplexerInputHandler {
  private canvas: HTMLCanvasElement | null = null;
  private connections = new Map<string, DeviceConnection>();
  private onStatusChange: ((serial: string, status: string) => void) | null = null;

  setOnStatusChange(cb: (serial: string, status: string) => void) {
    this.onStatusChange = cb;
  }

  attachCanvas(canvas: HTMLCanvasElement) {
    this.detachCanvas();
    this.canvas = canvas;
    this.attachListeners();
  }

  detachCanvas() {
    if (this.canvas) {
      this.detachListeners();
      this.canvas = null;
    }
  }

  addDevice(serial: string) {
    if (this.connections.has(serial)) return;

    const token = getToken();
    const base =
      WS_BASE_URL ||
      `${window.location.protocol === "https:" ? "wss:" : "ws:"}//${window.location.host}`;
    const url = `${base}/ws/device/${encodeURIComponent(serial)}/control?token=${encodeURIComponent(token || '')}`;

    const ws = new WebSocket(url);
    const conn: DeviceConnection = { serial, ws, hasControl: false };
    this.connections.set(serial, conn);

    ws.onopen = () => {
      conn.hasControl = true;
      this.onStatusChange?.(serial, "connected");
    };

    ws.onclose = () => {
      conn.hasControl = false;
      this.onStatusChange?.(serial, "disconnected");
      // close events land asynchronously; if this device was removed and
      // re-added meanwhile, don't drop the newer connection.
      if (this.connections.get(serial) === conn) {
        this.connections.delete(serial);
      }
    };

    ws.onerror = () => {};

    ws.onmessage = (event) => {
      try {
        const msg = JSON.parse(event.data);
        if (msg.error) {
          conn.hasControl = false;
          this.onStatusChange?.(serial, "denied");
        }
      } catch {}
    };
  }

  removeDevice(serial: string) {
    const conn = this.connections.get(serial);
    if (conn) {
      conn.ws.close();
      this.connections.delete(serial);
    }
  }

  /**
   * Make the broadcast set exactly `serials`: connect new devices, disconnect
   * ones that left. Callers pass the devices currently shown on screen, so
   * input never reaches a device the user can't see.
   */
  setDevices(serials: Iterable<string>) {
    const wanted = new Set(serials);
    for (const serial of Array.from(this.connections.keys())) {
      if (!wanted.has(serial)) this.removeDevice(serial);
    }
    wanted.forEach((serial) => this.addDevice(serial));
  }

  private broadcast(msg: ControlMessage) {
    const data = JSON.stringify(msg);
    for (const conn of this.connections.values()) {
      if (conn.ws.readyState === WebSocket.OPEN && conn.hasControl) {
        conn.ws.send(data);
      }
    }
  }

  private getNormalizedCoords(e: MouseEvent): { x: number; y: number } {
    if (!this.canvas) return { x: 0, y: 0 };
    const rect = this.canvas.getBoundingClientRect();
    return {
      x: (e.clientX - rect.left) / rect.width,
      y: (e.clientY - rect.top) / rect.height,
    };
  }

  private handleMouseDown = (e: MouseEvent) => {
    // preventDefault below stops the browser focusing the screen, so do it here:
    // a press on the screen means typing goes to the phones.
    this.canvas?.focus();
    if (e.button !== 0) return;
    e.preventDefault();
    const { x, y } = this.getNormalizedCoords(e);
    this.broadcast({
      type: "touch",
      action: 0,
      x, y,
      pointer_id: 0,
      pressure: 1.0,
    });
  };

  private handleMouseMove = (e: MouseEvent) => {
    if (e.buttons === 0) return;
    e.preventDefault();
    const { x, y } = this.getNormalizedCoords(e);
    this.broadcast({
      type: "touch",
      action: 2,
      x, y,
      pointer_id: 0,
      pressure: 1.0,
    });
  };

  private handleMouseUp = (e: MouseEvent) => {
    if (e.button !== 0) return;
    e.preventDefault();
    const { x, y } = this.getNormalizedCoords(e);
    this.broadcast({
      type: "touch",
      action: 1,
      x, y,
      pointer_id: 0,
      pressure: 0,
    });
  };

  // Two-finger swipes: vertical scrolls, horizontal drags (see WheelGestures).
  private wheelGestures = new WheelGestures((msg) => this.broadcast(msg));

  private handleWheel = (e: WheelEvent) => {
    e.preventDefault();
    if (!this.canvas) return;
    this.wheelGestures.wheel(e, this.getNormalizedCoords(e), this.canvas.getBoundingClientRect());
  };

  // No clipboard here: Ctrl+C and Ctrl+V go to every phone as keys.
  private keys = new CanvasKeys({ send: (msg) => this.broadcast(msg) });

  // Right-click is Back, as in scrcpy.
  private handleContextMenu = (e: Event) => {
    e.preventDefault();
    this.nav("back");
  };

  private attachListeners() {
    if (!this.canvas) return;
    this.canvas.addEventListener("mousedown", this.handleMouseDown);
    this.canvas.addEventListener("mousemove", this.handleMouseMove);
    this.canvas.addEventListener("mouseup", this.handleMouseUp);
    this.canvas.addEventListener("wheel", this.handleWheel, { passive: false });
    this.canvas.addEventListener("contextmenu", this.handleContextMenu);
    this.keys.attach(this.canvas);
  }

  private detachListeners() {
    if (!this.canvas) return;
    this.wheelGestures.cancel();
    this.canvas.removeEventListener("mousedown", this.handleMouseDown);
    this.canvas.removeEventListener("mousemove", this.handleMouseMove);
    this.canvas.removeEventListener("mouseup", this.handleMouseUp);
    this.canvas.removeEventListener("wheel", this.handleWheel);
    this.canvas.removeEventListener("contextmenu", this.handleContextMenu);
    this.keys.detach();
  }

  /** Press an on-screen navigation button on every device. */
  nav(action: NavAction) {
    for (const msg of navMessages(action)) this.broadcast(msg);
  }

  sendText(text: string) {
    if (text) {
      this.broadcast({ type: "text", text });
    }
  }

  disconnect() {
    this.detachCanvas();
    for (const conn of this.connections.values()) {
      conn.ws.close();
    }
    this.connections.clear();
  }

  get connectedSerials(): string[] {
    return Array.from(this.connections.keys());
  }
}
