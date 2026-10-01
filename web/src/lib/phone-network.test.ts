import { test } from 'node:test';
import assert from 'node:assert/strict';
import { describeNIC, pickable, stateSummary } from './phone-network';

test('state summary names the port in use, or says phones get nothing', () => {
  assert.deepEqual(stateSummary({ state: 'active', port: { mac: 'm', name: 'enp2s0' } }), {
    label: 'On, using enp2s0',
    tone: 'ok',
  });
  assert.equal(stateSummary({ state: 'off' }).label, 'Off: phones get no internet');
  assert.equal(stateSummary({ state: 'missing', port: { mac: 'm', name: 'enx1' } }).label, "Port enx1 isn't on the box");
  assert.equal(stateSummary({ state: 'error' }).tone, 'bad');
});

test('ports are described by link, driver and bus', () => {
  assert.equal(describeNIC({ name: 'a', up: false, carrier: false, driver: 'r8152', bus: 'usb', usable: true }), 'no cable · r8152, USB');
  assert.equal(describeNIC({ name: 'a', up: true, carrier: true, usable: true }), 'cable connected');
  assert.equal(describeNIC({ name: 'a', up: true, carrier: true, usable: true, in_use: true }), 'in use by Batter');
});

test('only usable ports can be picked, the one in use first', () => {
  const nics = [
    { name: 'eth0', up: true, carrier: true, usable: false },
    { name: 'enp3s0', up: false, carrier: false, usable: true },
    { name: 'enp2s0', up: true, carrier: true, usable: true, in_use: true },
  ];
  assert.deepEqual(pickable(nics).map((n) => n.name), ['enp2s0', 'enp3s0']);
});
