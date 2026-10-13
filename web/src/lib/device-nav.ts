/** On-screen navigation buttons (Android's nav bar, plus the app drawer). */
export type NavAction = "back" | "home" | "recents" | "all_apps";

export interface NavMessage {
  type: "key" | "back";
  action: number;
  keycode?: number;
  repeat?: number;
  metastate?: number;
}

// Android KeyEvent keycodes.
const KEYCODES: Record<Exclude<NavAction, "back">, number> = {
  home: 3, // KEYCODE_HOME
  recents: 187, // KEYCODE_APP_SWITCH
  all_apps: 284, // KEYCODE_ALL_APPS (the app drawer)
};

/**
 * Control messages for one press of a nav button: action 0 (down) then 1 (up).
 * Back uses scrcpy's BACK_OR_SCREEN_ON rather than KEYCODE_BACK, so it also
 * wakes a sleeping screen — the same thing scrcpy's right-click does.
 */
export function navMessages(action: NavAction): NavMessage[] {
  if (action === "back") {
    return [0, 1].map((a) => ({ type: "back", action: a }));
  }
  const keycode = KEYCODES[action];
  return [0, 1].map((a) => ({ type: "key", action: a, keycode, repeat: 0, metastate: 0 }));
}
