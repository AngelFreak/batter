import { streamStatus, type StatusView } from '@/lib/device-status';

/**
 * What the video client's status code means to a person. The video client has
 * two codes the shared stream mapping doesn't know ('unsupported' and
 * 'needs-https'); everything else goes through streamStatus.
 */
export function videoView(code: string): StatusView {
  if (code === 'unsupported') {
    return {
      label: "Can't play video",
      tone: 'danger',
      icon: 'square',
      hint: "This browser can't play the phone's video. Try a recent Chrome, Edge or Safari.",
      code,
    };
  }
  if (code === 'needs-https') {
    return {
      label: "Can't play video",
      tone: 'danger',
      icon: 'square',
      hint: 'Video playback needs a secure page (HTTPS or localhost).',
      code,
    };
  }
  return streamStatus(code);
}

/** Video codes that mean the picture can't be shown until something changes. */
export function isVideoFailure(code: string): boolean {
  return code === 'error' || code === 'decoder-error' || code === 'unsupported' || code === 'needs-https';
}

/** What the viewer tells the page header. */
export interface ViewerLive {
  video: string;
  control: string;
  fps: number;
  /** The phone screen has keyboard focus and control is connected: keys go to the phone. */
  keysToPhone: boolean;
}

export const VIEWER_LIVE_INITIAL: ViewerLive = { video: 'connecting', control: 'connecting', fps: 0, keysToPhone: false };
