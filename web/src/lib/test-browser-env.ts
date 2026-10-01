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

export function fakeCanvas() {
  return {
    width: 0,
    height: 0,
    getContext: () => ({ drawImage() {} }),
    addEventListener() {},
    removeEventListener() {},
    getBoundingClientRect: () => ({ left: 0, top: 0, width: 1, height: 1 }),
  } as unknown as HTMLCanvasElement;
}

/** A small binary video packet (12-byte header, non-config) — enough to count as data. */
export function videoPacket(): ArrayBuffer {
  return new ArrayBuffer(16);
}
