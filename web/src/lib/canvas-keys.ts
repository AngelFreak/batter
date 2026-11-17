import { keycodeMap } from "./device-keymap";

export interface KeyMessage {
  type: string;
  action?: number;
  keycode?: number;
  repeat?: number;
  metastate?: number;
  text?: string;
}

export interface KeySink {
  send(msg: KeyMessage): void;
  /** Ctrl/Cmd+V on the screen. If absent, the key goes to the phone as is. */
  paste?(): void;
  /** Ctrl/Cmd+C on the screen. If absent, the key goes to the phone as is. */
  copy?(): void;
}

function metastate(e: KeyboardEvent): number {
  let meta = 0;
  if (e.shiftKey) meta |= 1;
  if (e.altKey) meta |= 2;
  if (e.ctrlKey) meta |= 0x1000;
  return meta;
}

/**
 * Sends keys to the phone while its screen (the canvas) has focus, and only
 * then: listening on the canvas rather than the document leaves keys typed in
 * buttons and text fields to the page. Every key pressed on the screen is
 * released on the phone, even if focus moves before the key comes up.
 */
export class CanvasKeys {
  private target: HTMLElement | null = null;
  /** Keys pressed on the screen and not yet released: e.code -> keycode. */
  private held = new Map<string, number>();

  constructor(private sink: KeySink) {}

  attach(target: HTMLElement) {
    this.detach();
    this.target = target;
    target.addEventListener("keydown", this.handleKeyDown);
    target.addEventListener("keyup", this.handleKeyUp);
    target.addEventListener("blur", this.releaseAll);
  }

  detach() {
    if (!this.target) return;
    this.releaseAll();
    this.target.removeEventListener("keydown", this.handleKeyDown);
    this.target.removeEventListener("keyup", this.handleKeyUp);
    this.target.removeEventListener("blur", this.releaseAll);
    this.target = null;
  }

  private handleKeyDown = (e: KeyboardEvent) => {
    if (e.isComposing) return;

    if ((e.ctrlKey || e.metaKey) && e.key === "v" && this.sink.paste) {
      e.preventDefault();
      this.sink.paste();
      return;
    }
    if ((e.ctrlKey || e.metaKey) && e.key === "c" && this.sink.copy) {
      e.preventDefault();
      this.sink.copy();
      return;
    }

    e.preventDefault();

    const keycode = keycodeMap[e.code];
    if (keycode !== undefined) {
      this.held.set(e.code, keycode);
      this.sink.send({
        type: "key",
        action: 0,
        keycode,
        repeat: e.repeat ? 1 : 0,
        metastate: metastate(e),
      });
      return;
    }

    if (e.key.length === 1 && !e.ctrlKey && !e.metaKey) {
      this.sink.send({ type: "text", text: e.key });
    }
  };

  private handleKeyUp = (e: KeyboardEvent) => {
    if (e.isComposing) return;
    e.preventDefault();
    const keycode = this.held.get(e.code);
    if (keycode === undefined) return;
    this.held.delete(e.code);
    this.sink.send({ type: "key", action: 1, keycode, repeat: 0, metastate: metastate(e) });
  };

  private releaseAll = () => {
    this.held.forEach((keycode) => {
      this.sink.send({ type: "key", action: 1, keycode, repeat: 0, metastate: 0 });
    });
    this.held.clear();
  };
}
