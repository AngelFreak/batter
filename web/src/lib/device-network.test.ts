import { test } from 'node:test';
import assert from 'node:assert/strict';
import { arrivedOnLAN, networkLabel, networkState } from './device-network';

test('a connected phone on its adapter is on ethernet, with its address', () => {
  const d = { status: 'connected', connection: 'lan' as const, lan_address: '10.77.0.100' };
  assert.equal(networkState(d), 'ethernet');
  assert.equal(networkLabel(d), 'Ethernet · 10.77.0.100');
  assert.equal(arrivedOnLAN(d), true);
});

test('a connected phone without a LAN connection is on USB', () => {
  assert.equal(networkState({ status: 'connected', connection: 'usb' }), 'usb');
  assert.equal(networkState({ status: 'connected' }), 'usb');
  assert.equal(arrivedOnLAN({ status: 'connected', connection: 'usb' }), false);
});

test('a LAN phone whose adb over TCP is off needs re-provisioning, even though it is disconnected', () => {
  const d = { status: 'disconnected', connection: 'lan' as const, lan_address: '10.77.0.100', needs_reprovision: true };
  assert.equal(networkState(d), 'needs-reprovision');
  assert.equal(networkLabel(d), 'Needs USB re-provision');
  assert.equal(arrivedOnLAN(d), false);
});

test('anything else is not connected', () => {
  assert.equal(networkState({ status: 'disconnected' }), 'offline');
  assert.equal(networkState({ status: 'unauthorized', connection: 'lan' }), 'offline');
  assert.equal(networkLabel({ status: 'offline' }), 'Not connected');
});
