import { test } from 'node:test';
import assert from 'node:assert/strict';
import { loadAudioPrefs, saveAudioPrefs, DEFAULT_AUDIO_PREFS } from './audio-prefs';

function memoryStorage() {
  const data = new Map<string, string>();
  return {
    getItem: (k: string) => data.get(k) ?? null,
    setItem: (k: string, v: string) => void data.set(k, v),
  };
}

test('audio starts muted at full volume when nothing is stored', () => {
  assert.deepEqual(loadAudioPrefs(memoryStorage()), { muted: true, volume: 1 });
  assert.deepEqual(DEFAULT_AUDIO_PREFS, { muted: true, volume: 1 });
});

test('mute and volume are remembered', () => {
  const storage = memoryStorage();
  saveAudioPrefs({ muted: false, volume: 0.4 }, storage);
  assert.deepEqual(loadAudioPrefs(storage), { muted: false, volume: 0.4 });
});

test('storage that throws (private mode, blocked site data) falls back to defaults', () => {
  const broken = {
    getItem: () => {
      throw new Error('SecurityError');
    },
    setItem: () => {
      throw new Error('QuotaExceededError');
    },
  };
  assert.doesNotThrow(() => saveAudioPrefs({ muted: false, volume: 0.5 }, broken));
  assert.deepEqual(loadAudioPrefs(broken), DEFAULT_AUDIO_PREFS);
});

test('garbage or out-of-range stored values are ignored', () => {
  const storage = memoryStorage();
  storage.setItem('batter.audio', 'not json');
  assert.deepEqual(loadAudioPrefs(storage), DEFAULT_AUDIO_PREFS);
  storage.setItem('batter.audio', JSON.stringify({ muted: 'no', volume: 7 }));
  assert.deepEqual(loadAudioPrefs(storage), DEFAULT_AUDIO_PREFS);
});
