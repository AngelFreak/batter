import { test } from 'node:test';
import assert from 'node:assert/strict';
import { storedQuality, storeQuality, upgradeAtStoredQuality, QUALITY_KEY } from './video-quality';

function memoryStorage(initial: Record<string, string> = {}) {
  const data = { ...initial };
  return {
    getItem: (k: string) => (k in data ? data[k] : null),
    setItem: (k: string, v: string) => { data[k] = v; },
    data,
  };
}

test('the stored level is sent on upgrade', async () => {
  const calls: unknown[] = [];
  const storage = memoryStorage({ [QUALITY_KEY]: 'high' });
  await upgradeAtStoredQuality('S1', async (serial, opts) => { calls.push([serial, opts]); }, storage);
  assert.deepEqual(calls, [['S1', { quality: 'high' }]]);
});

test('defaults to medium when nothing (or junk) is stored', () => {
  assert.equal(storedQuality(memoryStorage()), 'medium');
  assert.equal(storedQuality(memoryStorage({ [QUALITY_KEY]: 'ultra' })), 'medium');
});

test('unavailable storage falls back instead of throwing', () => {
  const broken = {
    getItem: () => { throw new Error('denied'); },
    setItem: () => { throw new Error('denied'); },
  };
  assert.equal(storedQuality(broken), 'medium');
  assert.doesNotThrow(() => storeQuality('low', broken));
});

test('a chosen level is remembered', () => {
  const storage = memoryStorage();
  storeQuality('low', storage);
  assert.equal(storedQuality(storage), 'low');
});
