/** Finger swipes sent from the on-screen swipe pad. */
export type SwipeDirection = 'up' | 'down' | 'left' | 'right';

export interface SwipePoint {
  action: number; // 0 down, 2 move, 1 up
  x: number; // 0..1 of the screen
  y: number;
}

// The finger travels from FROM to TO along the swipe's axis, through the
// middle of the screen, staying clear of the edges: Android treats swipes
// from an edge as its own gestures (back, notifications, home).
const FROM = 0.75;
const TO = 0.25;
const STEPS = 10;

/** Where the finger goes for a swipe: down, STEPS moves, up. */
export function swipePath(dir: SwipeDirection): SwipePoint[] {
  // "up" means the finger moves up, as on the phone (the next video in a feed).
  const [start, end] = dir === 'up' || dir === 'left' ? [FROM, TO] : [TO, FROM];
  const vertical = dir === 'up' || dir === 'down';
  const at = (t: number) => {
    const v = start + (end - start) * t;
    return vertical ? { x: 0.5, y: v } : { x: v, y: 0.5 };
  };
  const path: SwipePoint[] = [{ action: 0, ...at(0) }];
  for (let i = 1; i <= STEPS; i++) path.push({ action: 2, ...at(i / STEPS) });
  path.push({ action: 1, ...at(1) });
  return path;
}

/** Time between points: the whole swipe takes about 200 ms, like a quick flick. */
export const SWIPE_STEP_MS = 18;
