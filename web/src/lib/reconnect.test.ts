import { test } from 'node:test';
import assert from 'node:assert/strict';
import { backoffDelay } from './reconnect';

test('backoff doubles per attempt within jitter bounds', () => {
  for (let attempt = 0; attempt < 5; attempt++) {
    const ceiling = 1000 * 2 ** attempt;
    assert.equal(backoffDelay(attempt, () => 0), ceiling / 2);
    assert.equal(backoffDelay(attempt, () => 1), ceiling);
  }
});

test('backoff is capped at 30s', () => {
  assert.equal(backoffDelay(20, () => 1), 30_000);
  assert.equal(backoffDelay(1000, () => 1), 30_000);
});
