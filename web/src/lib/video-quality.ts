// Video quality for the full-quality device view: the bitrate level is
// remembered per browser and sent when the view upgrades its session.

export type VideoQuality = 'low' | 'medium' | 'high';

export const QUALITY_LEVELS: { value: VideoQuality; label: string }[] = [
  { value: 'low', label: 'Low (1.5 Mbps)' },
  { value: 'medium', label: 'Medium (4 Mbps)' },
  { value: 'high', label: 'High (8 Mbps)' },
];

export const QUALITY_KEY = 'batter_video_quality';

type Storage = { getItem(key: string): string | null; setItem(key: string, value: string): void };

function browserStorage(): Storage | null {
  try {
    return typeof localStorage === 'undefined' ? null : localStorage;
  } catch {
    return null;
  }
}

function isQuality(v: unknown): v is VideoQuality {
  return v === 'low' || v === 'medium' || v === 'high';
}

// storedQuality is this browser's chosen level (medium if none or unreadable).
export function storedQuality(storage: Storage | null = browserStorage()): VideoQuality {
  try {
    const v = storage?.getItem(QUALITY_KEY);
    return isQuality(v) ? v : 'medium';
  } catch {
    return 'medium';
  }
}

export function storeQuality(q: VideoQuality, storage: Storage | null = browserStorage()) {
  try {
    storage?.setItem(QUALITY_KEY, q);
  } catch {
    // Not remembered (private mode, blocked storage); still applied.
  }
}

// upgradeAtStoredQuality opens the full view at the remembered level, so
// opening a device costs no extra session restart.
export function upgradeAtStoredQuality<T>(
  serial: string,
  upgrade: (serial: string, opts: { quality: VideoQuality }) => Promise<T>,
  storage: Storage | null = browserStorage(),
): Promise<T> {
  return upgrade(serial, { quality: storedQuality(storage) });
}
