// Minimal browser globals for unit-testing the WebSocket clients under node.

export class FakeWebSocket {
  static OPEN = 1;
  static instances: FakeWebSocket[] = [];
  readyState = 0;
  binaryType = 'blob';
  sent: string[] = [];
  onopen: (() => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  onmessage: ((e: { data: unknown }) => void) | null = null;
  constructor(public url: string) {
    FakeWebSocket.instances.push(this);
  }
  open() {
    this.readyState = FakeWebSocket.OPEN;
    this.onopen?.();
  }
  message(data: unknown) {
    this.onmessage?.({ data });
  }
  send(data: string) {
    this.sent.push(data);
  }
  // Like a browser, close() doesn't fire onclose synchronously.
  close() {
    this.readyState = 3;
  }
  /** Simulate the connection dropping (server restart, network blip). */
  drop() {
    this.readyState = 3;
    this.onclose?.();
  }
}

export function installBrowserEnv() {
  const g = globalThis as Record<string, unknown>;
  g.WebSocket = FakeWebSocket;
  g.window = { location: { protocol: 'http:', host: 'test' } };
  g.localStorage = { getItem: () => 'tok' };
  g.document = { addEventListener() {}, removeEventListener() {} };
}

export type FakeCanvas = HTMLCanvasElement & {
  /** Fire a DOM event at the canvas's registered listeners. */
  fire(type: string, event?: Record<string, unknown>): void;
};

export function fakeCanvas(): FakeCanvas {
  const listeners = new Map<string, Set<(e: unknown) => void>>();
  return {
    width: 0,
    height: 0,
    getContext: () => ({ drawImage() {} }),
    addEventListener(type: string, fn: (e: unknown) => void) {
      if (!listeners.has(type)) listeners.set(type, new Set());
      listeners.get(type)!.add(fn);
    },
    removeEventListener(type: string, fn: (e: unknown) => void) {
      listeners.get(type)?.delete(fn);
    },
    // 400x800 at the page origin, so clientX/Y map to x/400, y/800.
    getBoundingClientRect: () => ({ left: 0, top: 0, width: 400, height: 800 }),
    fire(type: string, event: Record<string, unknown> = {}) {
      const e = { preventDefault() {}, ...event };
      listeners.get(type)?.forEach((fn) => fn(e));
    },
  } as unknown as FakeCanvas;
}

/** A small binary video packet (12-byte header, non-config) — enough to count as data. */
export function videoPacket(): ArrayBuffer {
  return new ArrayBuffer(16);
}
