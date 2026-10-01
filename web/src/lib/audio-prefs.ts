export interface AudioPrefs {
  muted: boolean;
  /** Browser-side gain, 0..1. */
  volume: number;
}

type PrefsStorage = Pick<Storage, 'getItem' | 'setItem'>;

const KEY = 'batter.audio';

// Muted until the user opts in: browsers block autoplay anyway.
export const DEFAULT_AUDIO_PREFS: AudioPrefs = { muted: true, volume: 1 };

function defaultStorage(): PrefsStorage | null {
  try {
    return typeof localStorage === 'undefined' ? null : localStorage;
  } catch {
    return null; // accessing localStorage can itself throw (blocked site data)
  }
}

/** This browser's remembered audio settings, or the defaults. */
export function loadAudioPrefs(storage = defaultStorage()): AudioPrefs {
  try {
    const raw = storage?.getItem(KEY);
    if (!raw) return { ...DEFAULT_AUDIO_PREFS };
    const p = JSON.parse(raw) as Partial<AudioPrefs>;
    if (typeof p.muted !== 'boolean' || typeof p.volume !== 'number' || !(p.volume >= 0 && p.volume <= 1)) {
      return { ...DEFAULT_AUDIO_PREFS };
    }
    return { muted: p.muted, volume: p.volume };
  } catch {
    return { ...DEFAULT_AUDIO_PREFS };
  }
}

/** Remembers audio settings; silently does nothing if storage is unavailable. */
export function saveAudioPrefs(prefs: AudioPrefs, storage = defaultStorage()) {
  try {
    storage?.setItem(KEY, JSON.stringify(prefs));
  } catch {
    // private mode or quota: the setting just isn't remembered
  }
}
