import { test } from 'node:test';
import assert from 'node:assert/strict';
import { deviceStatus, streamStatus } from './device-status';

test('every device status the API sends gets a label, icon and tone', () => {
  const cases: [Parameters<typeof deviceStatus>[0], string, string][] = [
    [{ status: 'connected', has_session: true }, 'Streaming', 'dot'],
    [{ status: 'connected', has_session: false }, 'Idle', 'ring'],
    [{ status: 'unauthorized', has_session: false }, 'Needs approval', 'triangle'],
    [{ status: 'offline', has_session: false }, 'Offline', 'slash'],
    [{ status: 'disconnected', has_session: false }, 'Offline', 'slash'],
    [{ status: 'connected', has_session: false, needs_reprovision: true }, 'Needs USB', 'triangle'],
  ];
  for (const [input, label, icon] of cases) {
    const s = deviceStatus(input);
    assert.equal(s.label, label, JSON.stringify(input));
    assert.equal(s.icon, icon, JSON.stringify(input));
  }
});

test('only a streaming phone uses the accent', () => {
  assert.equal(deviceStatus({ status: 'connected', has_session: true }).tone, 'live');
  for (const status of ['connected', 'offline', 'disconnected', 'unauthorized'] as const) {
    assert.notEqual(deviceStatus({ status, has_session: false }).tone, 'live', status);
  }
});

test('an unknown device status falls back to Unknown with its raw code', () => {
  const s = deviceStatus({ status: 'bootloader' as never, has_session: false });
  assert.equal(s.label, 'Unknown');
  assert.equal(s.code, 'bootloader');
});

test('every stream code the clients report has plain text, never the raw code', () => {
  for (const code of ['idle', 'connecting', 'connected', 'streaming', 'reconnecting', 'disconnected', 'denied', 'decoder-error', 'error']) {
    const s = streamStatus(code);
    assert.notEqual(s.label, 'Unknown', code);
    assert.notEqual(s.label, code, `${code} shown raw`);
    assert.equal(s.code, code);
  }
  assert.equal(streamStatus('decoder-error').tone, 'danger');
  assert.equal(streamStatus('denied').label, 'Watch only');
});

test('an unknown stream code is Unknown, keeping the code for Details', () => {
  const s = streamStatus('weird');
  assert.equal(s.label, 'Unknown');
  assert.equal(s.code, 'weird');
});
