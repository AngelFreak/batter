import { keycodeMap } from "./device-keymap";
import { getToken } from "./auth";

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
  paste?: boolean;
}

export class DeviceInputHandler {
  private canvas: HTMLCanvasElement;
  private ws: WebSocket | null = null;
  private hasControl = false;
  private onStatusChange: ((status: string) => void) | null = null;
  private onClipboardReceive: ((text: string) => void) | null = null;
  private serial: string = "";
  private stopped = false;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;

  constructor(canvas: HTMLCanvasElement) {
    this.canvas = canvas;
  }

  setOnStatusChange(cb: (status: string) => void) {
    this.onStatusChange = cb;
  }

  setOnClipboardReceive(cb: (text: string) => void) {
    this.onClipboardReceive = cb;
  }

  connect(serial: string) {
    this.serial = serial;
    this.stopped = false;
    this.doConnect();
  }

  private doConnect() {
    if (this.stopped) return;

    const token = getToken();
    const base =
      WS_BASE_URL ||
      `${window.location.protocol === "https:" ? "wss:" : "ws:"}//${window.location.host}`;
    const url = `${base}/ws/device/${encodeURIComponent(this.serial)}/control?token=${encodeURIComponent(token || '')}`;

    this.ws = new WebSocket(url);

    this.ws.onopen = () => {
      this.hasControl = true;
      this.onStatusChange?.("connected");
      this.attachListeners();
    };

    this.ws.onclose = () => {
      this.hasControl = false;
      this.detachListeners();
      if (!this.stopped) {
        this.onStatusChange?.("reconnecting");
        this.reconnectTimer = setTimeout(() => this.doConnect(), 2000);
      } else {
        this.onStatusChange?.("disconnected");
      }
    };

    this.ws.onerror = () => {
      // onclose will fire after this
    };

    this.ws.onmessage = (event) => {
      try {
        const msg = JSON.parse(event.data);
        if (msg.error) {
          this.onStatusChange?.("denied");
          this.hasControl = false;
        } else if (msg.type === "clipboard" && msg.text) {
          navigator.clipboard.writeText(msg.text).catch(() => {});
          this.onClipboardReceive?.(msg.text);
        }
      } catch {
        // Ignore non-JSON messages
      }
    };
  }

  private send(msg: ControlMessage) {
    if (this.ws?.readyState === WebSocket.OPEN && this.hasControl) {
      this.ws.send(JSON.stringify(msg));
    }
  }

  private getNormalizedCoords(e: MouseEvent): { x: number; y: number } {
    const rect = this.canvas.getBoundingClientRect();
    return {
      x: (e.clientX - rect.left) / rect.width,
      y: (e.clientY - rect.top) / rect.height,
    };
  }

  private handleMouseDown = (e: MouseEvent) => {
    e.preventDefault();
    const { x, y } = this.getNormalizedCoords(e);
    this.send({
      type: "touch",
      action: 0,
      x,
      y,
      pointer_id: 0,
      pressure: 1.0,
    });
  };

  private handleMouseMove = (e: MouseEvent) => {
    if (e.buttons === 0) return;
    e.preventDefault();
    const { x, y } = this.getNormalizedCoords(e);
    this.send({
      type: "touch",
      action: 2,
      x,
      y,
      pointer_id: 0,
      pressure: 1.0,
    });
  };

  private handleMouseUp = (e: MouseEvent) => {
    e.preventDefault();
    const { x, y } = this.getNormalizedCoords(e);
    this.send({
      type: "touch",
      action: 1,
      x,
      y,
      pointer_id: 0,
      pressure: 0,
    });
  };

  private handleWheel = (e: WheelEvent) => {
    e.preventDefault();
    const { x, y } = this.getNormalizedCoords(e);
    const scrollH = e.deltaX > 0 ? 1 : e.deltaX < 0 ? -1 : 0;
    const scrollV = e.deltaY > 0 ? -1 : e.deltaY < 0 ? 1 : 0;
    this.send({
      type: "scroll",
      x,
      y,
      scroll_h: scrollH,
      scroll_v: scrollV,
    });
  };

  private handleKeyDown = (e: KeyboardEvent) => {
    // Ctrl+V / Cmd+V: paste host clipboard to device
    if ((e.ctrlKey || e.metaKey) && e.key === 'v') {
      e.preventDefault();
      navigator.clipboard.readText().then(text => {
        if (text) this.sendSetClipboard(text, true);
      }).catch(() => {});
      return;
    }

    // Ctrl+C / Cmd+C: request device clipboard
    if ((e.ctrlKey || e.metaKey) && e.key === 'c') {
      e.preventDefault();
      this.sendGetClipboard();
      return;
    }

    e.preventDefault();

    const keycode = keycodeMap[e.code];
    if (keycode !== undefined) {
      this.send({
        type: "key",
        action: 0,
        keycode,
        repeat: e.repeat ? 1 : 0,
        metastate: this.getMetastate(e),
      });
      return;
    }

    if (e.key.length === 1 && !e.ctrlKey && !e.metaKey) {
      this.send({
        type: "text",
        text: e.key,
      });
    }
  };

  private handleKeyUp = (e: KeyboardEvent) => {
    e.preventDefault();

    const keycode = keycodeMap[e.code];
    if (keycode !== undefined) {
      this.send({
        type: "key",
        action: 1,
        keycode,
        repeat: 0,
        metastate: this.getMetastate(e),
      });
    }
  };

  private handleContextMenu = (e: Event) => {
    e.preventDefault();
  };

  private getMetastate(e: KeyboardEvent): number {
    let meta = 0;
    if (e.shiftKey) meta |= 1;
    if (e.altKey) meta |= 2;
    if (e.ctrlKey) meta |= 0x1000;
    return meta;
  }

  private attachListeners() {
    this.canvas.addEventListener("mousedown", this.handleMouseDown);
    this.canvas.addEventListener("mousemove", this.handleMouseMove);
    this.canvas.addEventListener("mouseup", this.handleMouseUp);
    this.canvas.addEventListener("wheel", this.handleWheel, { passive: false });
    this.canvas.addEventListener("contextmenu", this.handleContextMenu);
    document.addEventListener("keydown", this.handleKeyDown);
    document.addEventListener("keyup", this.handleKeyUp);
  }

  private detachListeners() {
    this.canvas.removeEventListener("mousedown", this.handleMouseDown);
    this.canvas.removeEventListener("mousemove", this.handleMouseMove);
    this.canvas.removeEventListener("mouseup", this.handleMouseUp);
    this.canvas.removeEventListener("wheel", this.handleWheel);
    this.canvas.removeEventListener("contextmenu", this.handleContextMenu);
    document.removeEventListener("keydown", this.handleKeyDown);
    document.removeEventListener("keyup", this.handleKeyUp);
  }

  sendWake() {
    this.send({ type: "wake" });
  }

  sendScreenOn() {
    this.send({ type: "screen_on" });
  }

  sendScreenOff() {
    this.send({ type: "screen_off" });
  }

  sendText(text: string) {
    if (text) {
      this.send({ type: "text", text });
    }
  }

  sendSetClipboard(text: string, paste: boolean) {
    this.send({ type: "set_clipboard", text, paste });
  }

  sendGetClipboard() {
    this.send({ type: "get_clipboard" });
  }

  disconnect() {
    this.stopped = true;
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    this.detachListeners();
    if (this.ws) {
      this.ws.close();
      this.ws = null;
    }
    this.hasControl = false;
  }
}
