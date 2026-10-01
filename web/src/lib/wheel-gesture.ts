/** A control message produced from wheel input (subset of the handlers' ControlMessage). */
export interface GestureMessage {
  type: "touch" | "scroll";
  action?: number; // touch: 0 down, 1 up, 2 move
  x: number;
  y: number;
  pointer_id?: number;
  pressure?: number;
  scroll_h?: number;
  scroll_v?: number;
}

interface WheelDelta {
  deltaX: number;
  deltaY: number;
  deltaMode: number; // 0 pixels, 1 lines, 2 pages
}

// A gesture ends when no wheel event arrives for this long.
const IDLE_MS = 120;
const LINE_PX = 16;

/**
 * Turns trackpad/mouse wheel input into what the phone expects.
 *
 * Vertical two-finger scrolling stays scroll events, which lists and feeds
 * handle. A horizontal two-finger swipe becomes a real finger drag instead:
 * many Android views (galleries, tab pagers, carousels, edge-swipe back)
 * respond only to touch and ignore horizontal scroll events. Each gesture
 * locks to the axis it starts on, so wobble doesn't flip it mid-swipe.
 */
export class WheelGestures {
  private axis: "h" | "v" | null = null;
  private drag: { x: number; y: number } | null = null;
  private idleTimer: ReturnType<typeof setTimeout> | null = null;

  constructor(private readonly emit: (msg: GestureMessage) => void) {}

  wheel(e: WheelDelta, at: { x: number; y: number }, size: { width: number; height: number }) {
    const scale = e.deltaMode === 1 ? LINE_PX : e.deltaMode === 2 ? size.width : 1;
    const dx = e.deltaX * scale;
    const dy = e.deltaY * scale;

    if (this.axis === null) {
      this.axis = Math.abs(dx) > Math.abs(dy) ? "h" : "v";
    }
    this.armIdle();

    if (this.axis === "v") {
      this.emit({ type: "scroll", x: at.x, y: at.y, scroll_h: 0, scroll_v: dy > 0 ? -1 : dy < 0 ? 1 : 0 });
      return;
    }

    if (!this.drag) {
      this.drag = { x: at.x, y: at.y };
      this.touch(0);
    }
    // Content follows the fingers: a leftward swipe (positive deltaX) moves
    // the virtual finger left.
    this.drag.x = Math.min(1, Math.max(0, this.drag.x - dx / size.width));
    this.touch(2);
  }

  /** Ends any gesture in progress (lifts the virtual finger). */
  cancel() {
    if (this.idleTimer !== null) {
      clearTimeout(this.idleTimer);
      this.idleTimer = null;
    }
    this.end();
  }

  private armIdle() {
    if (this.idleTimer !== null) clearTimeout(this.idleTimer);
    this.idleTimer = setTimeout(() => {
      this.idleTimer = null;
      this.end();
    }, IDLE_MS);
  }

  private end() {
    if (this.drag) this.touch(1);
    this.drag = null;
    this.axis = null;
  }

  private touch(action: number) {
    const { x, y } = this.drag!;
    this.emit({ type: "touch", action, x, y, pointer_id: 0, pressure: action === 1 ? 0 : 1.0 });
  }
}
