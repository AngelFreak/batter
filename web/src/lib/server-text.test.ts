import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, readdirSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { AUDIO_REASONS, audioReasonText, isIPPrefix, missingPortText, nicRefusalText, portWarningText } from './server-text';

// npm test runs from web/.
const ROOT = resolve(process.cwd(), '..', 'internal');
const read = (...p: string[]) => readFileSync(join(ROOT, ...p), 'utf8');
const deviceGo = () =>
  readdirSync(join(ROOT, 'device'))
    .filter((f) => f.startsWith('scrcpy') && f.endsWith('.go') && !f.endsWith('_test.go'))
    .map((f) => read('device', f))
    .join('\n');

test('every known audio reason is still a literal the server writes', () => {
  const go = deviceGo();
  for (const reason of AUDIO_REASONS.keys()) assert.ok(go.includes(`"${reason}"`), reason);
  assert.ok(go.includes('"unsupported audio codec 0x%08x"'));
});

test('the warning and refusal templates still match the Go source', () => {
  const port = read('lan', 'port.go');
  assert.ok(port.includes('"%s was up on the box before Batter took it: something on the box manages it "'));
  assert.ok(port.includes('"the box brought %s up after Batter gave it back: something on the box manages it, "'));
  assert.ok(port.includes(`"network port %s (%s) isn't on the box; plug it in"`));
  const host = read('lan', 'hostnet.go');
  assert.ok(host.includes(`"it's part of %s on the box"`));
  assert.ok(host.includes(`"it carries the box's own network connection (default route)"`));
  assert.ok(host.includes(`"it has addresses on the box (" + strings.Join(used, ", ") + "); remove them, and whatever configures them, first"`));
  assert.ok(host.includes(`"the box has routes through it (" + r.Dst + ")"`));
});

test('audio reasons become sentences; unknown text is replaced', () => {
  assert.equal(audioReasonText('the audio stream ended'), 'The audio stream ended.');
  assert.equal(audioReasonText('unsupported audio codec 0x6f707573'), 'The phone sent an audio format Batter can’t play.');
  assert.equal(audioReasonText(undefined), 'Audio unavailable.');
  assert.equal(audioReasonText('open /dev/snd: permission denied'), 'Audio unavailable. The reason is in the server log.');
  assert.equal(audioReasonText('unsupported audio codec 0x<script>'), 'Audio unavailable. The reason is in the server log.');
});

test('port warnings keep the interface name and drop anything else', () => {
  const up =
    'enx001122 was up on the box before Batter took it: something on the box manages it (NetworkManager, netplan, ifupdown). ' +
    'While Batter is stopped the box can bring it up again, and phones can then reach the box. Set it unmanaged (docs/DEPLOY.md).';
  assert.match(portWarningText(up), /^enx001122 was up on the box before Batter took it, so something on the box manages it\./);
  const back =
    'the box brought eth1 up after Batter gave it back: something on the box manages it, and phones on the switch can now reach the box. ' +
    'Unplug the switch or set eth1 unmanaged (docs/DEPLOY.md).';
  assert.match(portWarningText(back), /^The box brought eth1 up after Batter gave it back/);
  assert.equal(
    portWarningText('netlink: operation not permitted'),
    'Something on the box manages the phone network port. The details are in the server log.',
  );
  assert.equal(
    portWarningText('<img src=x> was up on the box before Batter took it: something on the box manages it '),
    'Something on the box manages the phone network port. The details are in the server log.',
  );
});

test('port refusals keep validated names and addresses only', () => {
  assert.equal(nicRefusalText("it's part of br0 on the box"), 'It’s part of br0 on the box.');
  assert.equal(
    nicRefusalText("it carries the box's own network connection (default route)"),
    'It carries the box’s own network connection (default route).',
  );
  assert.equal(
    nicRefusalText('it has addresses on the box (192.168.1.5/24, fd00::5/64); remove them, and whatever configures them, first'),
    'It has addresses on the box (192.168.1.5/24, fd00::5/64). Remove them, and whatever configures them, first.',
  );
  assert.equal(nicRefusalText('the box has routes through it (10.0.0.0/8)'), 'The box has routes through it (10.0.0.0/8).');
  assert.equal(nicRefusalText('the box has routes through it (evil text)'), 'The box uses it. The details are in the server log.');
  assert.equal(nicRefusalText('exec: "ip": executable file not found'), 'The box uses it. The details are in the server log.');
});

test('a missing port keeps its validated name and MAC only', () => {
  assert.equal(
    missingPortText("network port enx001122 (aa:bb:cc:00:00:01) isn't on the box; plug it in"),
    'Network port enx001122 (aa:bb:cc:00:00:01) isn’t on the box. Plug it in.',
  );
  const fixed = 'The chosen network port isn’t on the box. Plug it in.';
  assert.equal(missingPortText("network port <b>x</b> (aa:bb:cc:00:00:01) isn't on the box; plug it in"), fixed);
  assert.equal(missingPortText("network port eth1 (not-a-mac) isn't on the box; plug it in"), fixed);
  assert.equal(missingPortText(undefined), fixed);
});

test('addresses and routes must be real IPs with prefixes that fit', () => {
  for (const ok of ['192.168.1.5/24', '0.0.0.0/0', 'fd00::5/64', '::1/128', 'fe80::1:2:3:4/10', '1:2:3:4:5:6:7:8/128']) {
    assert.ok(isIPPrefix(ok, true), ok);
  }
  assert.ok(isIPPrefix('10.0.0.5', false));
  for (const bad of ['999.999.999.999/999', '10.0.0.1/33', '::::/', 'fd00::5/129', '1.2.3/8', '1:2:3:4:5:6:7:8:9/64',
    'a::b::c/64', '1:2:3::4:5::6:7:8/64', '10.0.0.1/24/1', '01.2.3.4/8', 'fd00::5']) {
    assert.ok(!isIPPrefix(bad, true), bad);
  }
  assert.equal(
    nicRefusalText('it has addresses on the box (999.999.999.999/999); remove them, and whatever configures them, first'),
    'The box uses it. The details are in the server log.',
  );
  assert.equal(nicRefusalText('the box has routes through it (::::/)'), 'The box uses it. The details are in the server log.');
  assert.equal(nicRefusalText('the box has routes through it (default)'), 'The box has routes through it (default).');
  assert.equal(nicRefusalText('the box has routes through it (10.0.0.5)'), 'The box has routes through it (10.0.0.5).');
});
