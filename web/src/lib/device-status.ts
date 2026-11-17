// Plain-language status for a phone and for its live streams. Status is
// always shown as an icon plus a word, never colour alone; the raw code stays
// available for the Details line.

import type { DeviceInfo } from './api';

/** Colour role: live = accent, idle/muted = neutral, warn, danger. */
export type StatusTone = 'live' | 'idle' | 'muted' | 'warn' | 'danger';
/** Shape drawn beside the label, so status reads without colour. */
export type StatusIcon = 'dot' | 'ring' | 'slash' | 'triangle' | 'square';

export interface StatusView {
  label: string;
  tone: StatusTone;
  icon: StatusIcon;
  /** One sentence on what it means or what to do; empty when obvious. */
  hint: string;
  /** The raw value this was derived from. */
  code: string;
}

type DeviceStatusInput = Pick<DeviceInfo, 'status' | 'has_session' | 'needs_reprovision'>;

/** Where a phone stands, for its card and the dashboard tiles. */
export function deviceStatus(d: DeviceStatusInput): StatusView {
  if (d.needs_reprovision) {
    return {
      label: 'Needs USB',
      tone: 'warn',
      icon: 'triangle',
      hint: 'It restarted. Plug it into USB once to control it again.',
      code: 'needs_reprovision',
    };
  }
  switch (d.status) {
    case 'connected':
      return d.has_session
        ? { label: 'Streaming', tone: 'live', icon: 'dot', hint: '', code: 'connected+session' }
        : { label: 'Idle', tone: 'idle', icon: 'ring', hint: 'Connected, no session running.', code: 'connected' };
    case 'unauthorized':
      return {
        label: 'Needs approval',
        tone: 'warn',
        icon: 'triangle',
        hint: 'Accept "Allow USB debugging?" on the phone.',
        code: 'unauthorized',
      };
    case 'offline':
      return { label: 'Offline', tone: 'muted', icon: 'slash', hint: 'adb sees it but it is not responding.', code: 'offline' };
    case 'disconnected':
      return { label: 'Offline', tone: 'muted', icon: 'slash', hint: 'Not connected to Batter.', code: 'disconnected' };
    default:
      return { label: 'Unknown', tone: 'muted', icon: 'ring', hint: '', code: String(d.status || 'unknown') };
  }
}

/** The status codes the video, audio and control clients report. */
export function streamStatus(code: string): StatusView {
  const v = (label: string, tone: StatusTone, icon: StatusIcon, hint = ''): StatusView => ({ label, tone, icon, hint, code });
  switch (code) {
    case 'idle':
      return v('Idle', 'idle', 'ring');
    case 'connecting':
      return v('Connecting', 'muted', 'ring');
    case 'connected':
      return v('Connected', 'idle', 'ring');
    case 'streaming':
      return v('Live', 'live', 'dot');
    case 'reconnecting':
      return v('Reconnecting', 'warn', 'triangle', 'The connection dropped. Retrying.');
    case 'disconnected':
      return v('Disconnected', 'muted', 'slash');
    case 'denied':
      return v('Watch only', 'warn', 'triangle', "Your access to this phone doesn't include control.");
    case 'decoder-error':
      return v("Can't play video", 'danger', 'square', "This browser can't decode the phone's H.264 stream.");
    case 'error':
      return v('Stream failed', 'danger', 'square', 'The stream stopped with an error.');
    default:
      return v('Unknown', 'muted', 'ring');
  }
}
