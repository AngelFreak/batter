import { test } from 'node:test';
import assert from 'node:assert/strict';
import { loadViewerPrefs, saveViewerPrefs, clampPadPos, DEFAULT_VIEWER_PREFS } from './viewer-prefs';

function memoryStorage() {
  const data = new Map<string, string>();
  return {
    getItem: (k: string) => data.get(k) ?? null,
    setItem: (k: string, v: string) => void data.set(k, v),
  };
}

test('by default the buttons show and the swipe pad is off', () => {
  const p = loadViewerPrefs(memoryStorage());
  assert.equal(p.navButtons, true);
  assert.equal(p.swipePad, false);
  assert.deepEqual(p, DEFAULT_VIEWER_PREFS);
});

test('the toggles and the pad position are remembered', () => {
  const storage = memoryStorage();
  saveViewerPrefs({ navButtons: false, swipePad: true, padPos: { x: 0.2, y: 0.3 } }, storage);
  assert.deepEqual(loadViewerPrefs(storage), { navButtons: false, swipePad: true, padPos: { x: 0.2, y: 0.3 } });
});

test('a pad position off the view is pulled back onto it', () => {
  assert.deepEqual(clampPadPos({ x: -0.5, y: 1.7 }), { x: 0, y: 1 });
  const storage = memoryStorage();
  storage.setItem('batter.viewer', JSON.stringify({ navButtons: true, swipePad: true, padPos: { x: 4, y: -1 } }));
  assert.deepEqual(loadViewerPrefs(storage).padPos, { x: 1, y: 0 });
});

test('garbage or unreadable storage falls back to the defaults', () => {
  const storage = memoryStorage();
  storage.setItem('batter.viewer', '{not json');
  assert.deepEqual(loadViewerPrefs(storage), DEFAULT_VIEWER_PREFS);
  storage.setItem('batter.viewer', JSON.stringify({ navButtons: 'yes' }));
  assert.deepEqual(loadViewerPrefs(storage), DEFAULT_VIEWER_PREFS);
  const broken = {
    getItem: () => {
      throw new Error('SecurityError');
    },
    setItem: () => {
      throw new Error('QuotaExceededError');
    },
  };
  assert.deepEqual(loadViewerPrefs(broken), DEFAULT_VIEWER_PREFS);
  saveViewerPrefs(DEFAULT_VIEWER_PREFS, broken); // doesn't throw
});
