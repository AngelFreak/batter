// What a viewer chose to show around a phone's screen, remembered in this
// browser: the nav buttons, and the floating swipe pad and where it sits.

export interface PadPos {
  /** The pad's centre, as a fraction (0..1) of the phone view's width and height. */
  x: number;
  y: number;
}

export interface ViewerPrefs {
  navButtons: boolean;
  swipePad: boolean;
  padPos: PadPos;
}

type PrefsStorage = Pick<Storage, 'getItem' | 'setItem'>;

const KEY = 'batter.viewer';

export const DEFAULT_VIEWER_PREFS: ViewerPrefs = {
  navButtons: true,
  swipePad: false,
  padPos: { x: 0.85, y: 0.75 },
};

function defaultStorage(): PrefsStorage | null {
  try {
    return typeof localStorage === 'undefined' ? null : localStorage;
  } catch {
    return null; // accessing localStorage can itself throw (blocked site data)
  }
}

const clamp01 = (v: number) => Math.min(1, Math.max(0, v));

/** Keeps the pad on the view. */
export function clampPadPos(p: PadPos): PadPos {
  return { x: clamp01(p.x), y: clamp01(p.y) };
}

/** This browser's remembered viewer settings, or the defaults. */
export function loadViewerPrefs(storage = defaultStorage()): ViewerPrefs {
  try {
    const raw = storage?.getItem(KEY);
    if (!raw) return structuredClone(DEFAULT_VIEWER_PREFS);
    const p = JSON.parse(raw) as Partial<ViewerPrefs>;
    if (
      typeof p.navButtons !== 'boolean' ||
      typeof p.swipePad !== 'boolean' ||
      typeof p.padPos?.x !== 'number' ||
      typeof p.padPos?.y !== 'number'
    ) {
      return structuredClone(DEFAULT_VIEWER_PREFS);
    }
    return { navButtons: p.navButtons, swipePad: p.swipePad, padPos: clampPadPos(p.padPos) };
  } catch {
    return structuredClone(DEFAULT_VIEWER_PREFS);
  }
}

/** Remembers viewer settings; silently does nothing if storage is unavailable. */
export function saveViewerPrefs(prefs: ViewerPrefs, storage = defaultStorage()) {
  try {
    storage?.setItem(KEY, JSON.stringify(prefs));
  } catch {
    // not remembered, but nothing breaks
  }
}
