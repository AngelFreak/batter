import { test } from 'node:test';
import assert from 'node:assert/strict';
import { countByStatus, filterDevices, emptyReason, statusBucket, displayName } from './dashboard-filter';
import type { DeviceInfo } from './api';

const dev = (serial: string, over: Partial<DeviceInfo> = {}): DeviceInfo => ({
  serial,
  state: 'device',
  status: 'connected',
  model: 'Pixel 8a',
  product: 'akita',
  has_session: false,
  ...over,
});

const fleet = [
  dev('S1', { nickname: 'Desk 2', has_session: true }),
  dev('S2', { nickname: 'Desk 10' }),
  dev('S3', { nickname: 'Lab A', status: 'unauthorized', model: 'Galaxy A06' }),
  dev('S4', { nickname: 'Lab B', status: 'disconnected' }),
  dev('S5', { needs_reprovision: true, model: 'Galaxy A06' }),
];

test('tile counts add up and match each filter', () => {
  const c = countByStatus(fleet);
  assert.deepEqual(c, { all: 5, streaming: 1, idle: 1, attention: 2, offline: 1 });
  for (const status of ['streaming', 'idle', 'attention', 'offline'] as const) {
    const shown = filterDevices(fleet, { status, query: '', sort: 'name' });
    assert.equal(shown.length, c[status], status);
    assert.ok(shown.every((d) => statusBucket(d) === status), status);
  }
});

test('search matches nickname, model and serial, case-insensitively', () => {
  const q = (query: string) => filterDevices(fleet, { status: 'all', query, sort: 'name' }).map((d) => d.serial);
  assert.deepEqual(q('desk'), ['S1', 'S2']);
  assert.deepEqual(q('GALAXY'), ['S5', 'S3']);
  assert.deepEqual(q('s4'), ['S4']);
  assert.deepEqual(q('  '), ['S1', 'S2', 'S5', 'S3', 'S4']);
});

test('name sort is natural (Desk 2 before Desk 10); status sort puts attention first', () => {
  const byName = filterDevices(fleet, { status: 'all', query: '', sort: 'name' }).map(displayName);
  assert.deepEqual(byName.slice(0, 2), ['Desk 2', 'Desk 10']);
  const byStatus = filterDevices(fleet, { status: 'all', query: '', sort: 'status' }).map((d) => d.serial);
  assert.deepEqual(byStatus, ['S5', 'S3', 'S1', 'S2', 'S4']);
});

test('the empty state says why', () => {
  const base = { query: '', status: 'all' as const, canAdd: true };
  assert.deepEqual(emptyReason({ ...base, total: 0, inGroup: 0, shown: 0 }), { kind: 'no-devices', canAdd: true });
  assert.deepEqual(emptyReason({ ...base, total: 5, inGroup: 0, shown: 0, group: 'Lab' }), { kind: 'group-empty', group: 'Lab' });
  assert.deepEqual(emptyReason({ ...base, total: 5, inGroup: 3, shown: 0, group: 'Lab', query: ' ab ' }), {
    kind: 'no-match',
    query: 'ab',
    status: 'all',
  });
  assert.equal(emptyReason({ ...base, total: 5, inGroup: 5, shown: 2 }), null);
});
