import { test } from 'node:test';
import assert from 'node:assert/strict';
import { swipePath, type SwipeDirection } from './swipe-pad';

const along = (dir: SwipeDirection) => {
  const path = swipePath(dir);
  const axis = dir === 'up' || dir === 'down' ? 'y' : 'x';
  return { path, values: path.map((p) => p[axis]) };
};

test('a swipe is a finger going down, moving, and lifting', () => {
  const path = swipePath('up');
  assert.equal(path[0].action, 0);
  assert.equal(path[path.length - 1].action, 1);
  assert.ok(path.slice(1, -1).every((p) => p.action === 2));
  assert.ok(path.length >= 5, 'too few points for Android to see a swipe');
});

test('swipe up moves the finger up the middle of the screen (next video in a feed)', () => {
  const { path, values } = along('up');
  assert.ok(values[0] > values[values.length - 1]);
  assert.ok(values.every((v, i) => i === 0 || v <= values[i - 1]), 'not monotonic');
  assert.ok(path.every((p) => p.x === 0.5));
});

test('swipe down, left and right go the way they say', () => {
  const down = along('down').values;
  assert.ok(down[0] < down[down.length - 1]);
  const left = along('left');
  assert.ok(left.values[0] > left.values[left.values.length - 1]);
  assert.ok(left.path.every((p) => p.y === 0.5));
  const right = along('right').values;
  assert.ok(right[0] < right[right.length - 1]);
});

test('swipes stay clear of the edges, where Android has its own gestures', () => {
  for (const dir of ['up', 'down', 'left', 'right'] as const) {
    for (const p of swipePath(dir)) {
      assert.ok(p.x >= 0.15 && p.x <= 0.85 && p.y >= 0.15 && p.y <= 0.85, `${dir} reaches the edge at ${p.x},${p.y}`);
    }
  }
});
