import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { KNOWN_API_ERRORS, errorMessage } from './api-error';

// npm test runs from web/.
const API_DIR = resolve(process.cwd(), '..', 'internal', 'api');

function goFiles(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) return goFiles(p);
    return name.endsWith('.go') && !name.endsWith('_test.go') ? [p] : [];
  });
}

function goLiterals(): Set<string> {
  const out = new Set<string>();
  for (const f of goFiles(API_DIR)) {
    for (const m of readFileSync(f, 'utf8').matchAll(/"error":\s*"((?:[^"\\]|\\.)*)"/g)) {
      // "failed to " + x builds a message at runtime; only whole literals count.
      if (!m[1].endsWith(' ')) out.add(m[1]);
    }
  }
  return out;
}

test('the allowlist is exactly the literal error messages in the Go handlers', () => {
  const go = goLiterals();
  assert.ok(go.size > 50, `found only ${go.size} literals; is ${API_DIR} right?`);
  const missing = [...go].filter((m) => !KNOWN_API_ERRORS.has(m));
  const stale = [...KNOWN_API_ERRORS].filter((m) => !go.has(m));
  assert.deepEqual(missing, [], 'handler messages missing from KNOWN_API_ERRORS');
  assert.deepEqual(stale, [], 'KNOWN_API_ERRORS entries no handler writes any more');
});

test('a known message is shown as a sentence', () => {
  assert.equal(errorMessage(new Error('no active session for device'), 'stop the session'), 'No active session for device.');
  assert.equal(
    errorMessage(new Error("couldn't read the phone's screen lock; is it connected?"), 'read the lock'),
    "Couldn't read the phone's screen lock; is it connected?",
  );
});

test('anything else gets the generic sentence', () => {
  const generic = "Couldn't stop the session. Try again.";
  for (const err of [
    new Error('pq: duplicate key value violates unique constraint "devices_pkey"'),
    new Error('exec: "adb": executable file not found in $PATH'),
    new Error('Failed to stop session'), // an api.ts fallback
    new TypeError('Failed to fetch'), // network failure
    'a string',
    undefined,
  ]) {
    assert.equal(errorMessage(err, 'stop the session'), generic, String(err));
  }
});
