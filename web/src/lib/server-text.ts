// Turns the server's free-text status fields (audio reasons, phone-network
// warnings and port refusals) into UI copy. Each known template is matched and
// rebuilt from validated pieces; anything else gets a fixed sentence, so text
// the server never meant for the browser can't reach the page.

const IFACE = '[A-Za-z0-9_.:@-]{1,15}';
const MAC = '[0-9a-f]{2}(?::[0-9a-f]{2}){5}';

function isIPv4(s: string): boolean {
  const parts = s.split('.');
  return parts.length === 4 && parts.every((p) => /^(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)$/.test(p));
}

function isIPv6(s: string): boolean {
  const halves = s.split('::');
  if (halves.length > 2) return false;
  const groups = (h: string) => (h === '' ? [] : h.split(':'));
  const all = halves.flatMap(groups);
  if (!all.every((g) => /^[0-9A-Fa-f]{1,4}$/.test(g))) return false;
  return halves.length === 2 ? all.length <= 7 : all.length === 8;
}

// An address, optionally with a prefix length that fits its family.
export function isIPPrefix(s: string, needPrefix: boolean): boolean {
  const [ip, prefix, ...rest] = s.split('/');
  if (rest.length > 0 || (needPrefix && prefix === undefined)) return false;
  const v4 = isIPv4(ip);
  if (!v4 && !isIPv6(ip)) return false;
  if (prefix === undefined) return true;
  return /^\d{1,3}$/.test(prefix) && Number(prefix) <= (v4 ? 32 : 128);
}

// Go literals in internal/device/scrcpy*.go, mapped to UI sentences.
export const AUDIO_REASONS: ReadonlyMap<string, string> = new Map([
  ['audio is off for this session', 'Audio is off for this session.'],
  ['the device closed the audio stream', 'The phone closed the audio stream.'],
  ['the device cannot capture audio (it needs Android 11 or newer)', 'The phone can’t capture audio. It needs Android 11 or newer.'],
  ['audio configuration error on the device', 'The phone’s audio configuration failed.'],
  ['the audio stream ended', 'The audio stream ended.'],
  ['the device has not started audio yet', 'The phone hasn’t started audio yet.'],
]);

export function audioReasonText(reason?: string): string {
  if (!reason) return 'Audio unavailable.';
  const known = AUDIO_REASONS.get(reason);
  if (known) return known;
  if (/^unsupported audio codec 0x[0-9a-f]{8}$/.test(reason)) return 'The phone sent an audio format Batter can’t play.';
  return 'Audio unavailable. The reason is in the server log.';
}

// Templates from internal/lan/port.go (setWarning).
export function portWarningText(warning: string): string {
  let m = new RegExp(`^(${IFACE}) was up on the box before Batter took it: something on the box manages it `).exec(warning);
  if (m) {
    return `${m[1]} was up on the box before Batter took it, so something on the box manages it. While Batter is stopped the box can bring it up again, and phones can then reach the box. Set it unmanaged (docs/DEPLOY.md).`;
  }
  m = new RegExp(`^the box brought (${IFACE}) up after Batter gave it back: `).exec(warning);
  if (m) {
    return `The box brought ${m[1]} up after Batter gave it back, so phones on the switch can now reach the box. Unplug the switch or set ${m[1]} unmanaged (docs/DEPLOY.md).`;
  }
  return 'Something on the box manages the phone network port. The details are in the server log.';
}

// Template from internal/lan/port.go (StateMissing).
export function missingPortText(error?: string): string {
  const m = error && new RegExp(`^network port (${IFACE}) \\((${MAC})\\) isn't on the box; plug it in$`).exec(error);
  if (m) return `Network port ${m[1]} (${m[2]}) isn’t on the box. Plug it in.`;
  return 'The chosen network port isn’t on the box. Plug it in.';
}

// Templates from internal/lan/hostnet.go (refusal).
export function nicRefusalText(reason?: string): string {
  if (!reason) return 'It can’t be used.';
  let m = new RegExp(`^it's part of (${IFACE}) on the box$`).exec(reason);
  if (m) return `It’s part of ${m[1]} on the box.`;
  if (reason === "it carries the box's own network connection (default route)") {
    return 'It carries the box’s own network connection (default route).';
  }
  m = /^it has addresses on the box \((.+)\); remove them, and whatever configures them, first$/.exec(reason);
  if (m && m[1].split(', ').every((a) => isIPPrefix(a, true))) {
    return `It has addresses on the box (${m[1]}). Remove them, and whatever configures them, first.`;
  }
  m = /^the box has routes through it \((.+)\)$/.exec(reason);
  if (m && (m[1] === 'default' || isIPPrefix(m[1], false))) return `The box has routes through it (${m[1]}).`;
  return 'The box uses it. The details are in the server log.';
}
