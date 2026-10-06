import { test } from 'node:test';
import assert from 'node:assert/strict';
import { parsePacketHeader } from './scrcpy-packet';

function header(high: number, low = 0): ArrayBuffer {
  const buf = new ArrayBuffer(12 + 4);
  const view = new DataView(buf);
  view.setUint32(0, high);
  view.setUint32(4, low);
  view.setUint32(8, 4);
  return buf;
}

test('config packets carry bit 62', () => {
  assert.deepEqual(parsePacketHeader(header(0x40000000)), { config: true, key: false, pts: 0 });
});

test('keyframes carry bit 61, with the PTS below it', () => {
  assert.deepEqual(parsePacketHeader(header(0x20000000, 1000)), { config: false, key: true, pts: 1000 });
});

test('a delta is neither config nor key', () => {
  assert.deepEqual(parsePacketHeader(header(0, 33333)), { config: false, key: false, pts: 33333 });
});

test('the PTS uses all 61 bits below the flags', () => {
  // (Leaking the keyframe bit into the PTS is caught by the keyframe test.)
  const got = parsePacketHeader(header(0x1fffffff, 0xffffffff));
  assert.deepEqual(got, { config: false, key: false, pts: 0x1fffffff * 2 ** 32 + 0xffffffff });
});

test('session packets and short messages are not media', () => {
  assert.equal(parsePacketHeader(header(0x80000000, 1080)), null);
  assert.equal(parsePacketHeader(new ArrayBuffer(11)), null);
});
