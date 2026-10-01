const BASE_DELAY_MS = 1000;
const MAX_DELAY_MS = 30_000;

/**
 * Delay before reconnect attempt `attempt` (0-based): exponential from 1s,
 * capped at 30s, with jitter in [ceiling/2, ceiling] so many tabs/tiles
 * reconnecting after a backend restart don't all hit it at once.
 */
export function backoffDelay(attempt: number, random: () => number = Math.random): number {
  const ceiling = Math.min(MAX_DELAY_MS, BASE_DELAY_MS * 2 ** Math.min(attempt, 30));
  return Math.round(ceiling / 2 + (random() * ceiling) / 2);
}

/**
 * Schedules reconnect attempts with backoff. Owners call schedule() when their
 * socket drops unexpectedly, reset() once the connection is proven healthy
 * (data received — not merely opened, so a server that accepts then drops
 * still backs off), and stop() when the user is done with it.
 */
export class Reconnector {
  private attempt = 0;
  private timer: ReturnType<typeof setTimeout> | null = null;

  constructor(private readonly reconnect: () => void) {}

  schedule() {
    this.cancel();
    const delay = backoffDelay(this.attempt++);
    this.timer = setTimeout(() => {
      this.timer = null;
      this.reconnect();
    }, delay);
  }

  reset() {
    this.attempt = 0;
  }

  cancel() {
    if (this.timer !== null) {
      clearTimeout(this.timer);
      this.timer = null;
    }
  }
}
