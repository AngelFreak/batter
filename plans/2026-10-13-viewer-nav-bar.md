# Plan: Back / Home / Recents / All-apps buttons under the device screen

**Date:** 2026-10-13 · **Branch:** `feature/scrcpy-5` (PR #6), at the founder's request
("merge it into this branch") · **Status:** built and verified on two phones. The §2.2 review is BLOCKED (see Review), so this is not ready to merge.

## What

A row of four navigation buttons under the phone in the device viewer and the
multiplexer: Back, Home, Recents, All apps. Right-clicking the screen sends Back, as in
scrcpy. In the multiplexer the press goes to every device.

## Why

Phones on gesture navigation (the Pixel 8a) show no buttons at all, only a handle, so in
the viewer the only way home is a precise drag from the bottom edge. The founder looked
for "the button on the bottom of the phone" on 2026-10-13 and found nothing. The code
was written on 2026-10-01 but never committed. It sits uncommitted in the main checkout
on `feature/reverse-tether-vpn`, mixed in with the unfinished Blackfield theme.

## Approach

Move only the nav-bar work, unchanged, out of the main checkout and into this branch. The
theme files (`globals.css`, `layout.tsx`, `tailwind.config.ts`, `app/theme/`) are left
where they are. The main checkout itself is not modified.

- `web/src/lib/device-nav.ts` (new): `navMessages(action)` returns the control messages
  for one press, a down then an up. Back uses scrcpy's `back` message
  (BACK_OR_SCREEN_ON, which also wakes the screen). Home, Recents and All apps are
  `key` messages with Android keycodes 3, 187 and 284.
- `web/src/components/device/nav-bar.tsx` (new): four buttons. `mousedown` is
  prevented so keyboard focus stays on the screen. Disabled while control is not
  connected.
- `web/src/lib/device-input.ts` and `device-input-multiplexer.ts`: a `nav(action)` method;
  the context-menu handler now also sends Back.
- `device-viewer.tsx` and `app/multiplexer/page.tsx`: render `NavBar`.
- `web/src/lib/test-browser-env.ts`: the fake canvas records listeners and can fire
  events, so a test can right-click it.

No backend change: the control handler already relays `key` and `back` messages
unchanged (used by the keyboard, and checked on 2026-10-12 with BACK).

## Files touched

`web/src/lib/device-nav.ts`, `web/src/lib/device-nav.test.ts`,
`web/src/components/device/nav-bar.tsx` (all new), `web/src/lib/device-input.ts`,
`web/src/lib/device-input-multiplexer.ts`, `web/src/lib/test-browser-env.ts`,
`web/src/components/device/device-viewer.tsx`, `web/src/app/multiplexer/page.tsx`,
`docs/guides/TEST-MAP.md`.

## Risks

- **Right-click changes meaning.** It used to do nothing; now it sends Back. That matches
  scrcpy, but a stray right-click in the multiplexer goes Back on every phone.
- **All apps (284, KEYCODE_ALL_APPS) needs Android 12 or later.** Older phones ignore it.
  Our phones are on 14 and 17.
- **Home in the multiplexer** sends every phone to its home screen at once. That is what
  the button says, but it can't be undone.
- **Scope.** PR #6 is the scrcpy upgrade, and this adds a feature to it. The founder chose
  that. The commit stays separate, so it can be reverted on its own.
- **Layout.** The bar takes about 40px under the screen. The viewer's phone-width
  overflow (found 2026-10-08) is a separate, existing problem and is not fixed here.

## Tests

- Unit tests (`device-nav.test.ts`): the keycodes and the down/up pairs. Then the wired
  paths: `DeviceInputHandler.nav` and right-click put the messages on the control
  socket, and the multiplexer broadcasts to every device. Each will be mutation-checked
  against the production code.
- Existing suites: `npm test`, `tsc`, `next build`.
- End to end on the Pixel 8a and Galaxy A06: send each button's messages over the
  control WebSocket and confirm the phone's focused window changes (Home → launcher,
  Recents → recents view, All apps → app drawer, Back → returns).
- In Chrome via the founder's tunnel: the bar renders under the phone, and clicking each
  button works.
- `docs/guides/TEST-MAP.md`: add the nav bar (function and viewpoint).

## Roles

Orchestrator, Coder and Tech Architect: Claude Opus 5.5 (this session). The original
code is also Claude-authored (2026-10-01). Reviewer: a different-vendor model or a human
(below).

## Review

**Blocked, 2026-10-13 06:30 UTC.** No different-vendor review has happened.

- **Codex:** `codex exec --sandbox read-only`. Usage limit, "try again at 2:56 PM".
- **Grok:** `grok --sandbox read-only`. Free usage limit reached.
- **Gemini:** not installed.

The prompt (this plan plus the staged diff) is kept in the session scratchpad. It
should be re-run against the final diff once a reviewer is available. The founder asked
for the change; that is a go-ahead, not a review.

## Deviations

- **Conflicts.** The patch did not apply cleanly, because this branch had moved on. The
  five conflicts were all import lines or the fake canvas's size. Both imports were
  kept, along with this branch's 400×800 canvas. The `test-browser-env.ts` change
  dropped out entirely, because the branch's version already records and fires
  listeners.
- **Bug found on the phones, fixed here.** Mouse-down and mouse-up sent a touch for any
  button. With right-click now meaning Back, a right-click also tapped the screen: in
  the Galaxy's Recents it opened the app card under the pointer instead of going back.
  Both handlers now ignore any button except the left (primary) one. The test came
  first: two right-click tests failed, and two left-click tests were paired as the
  positive case.
- **All apps does nothing on Samsung.** One UI's launcher ignores KEYCODE_ALL_APPS
  (284). The button is kept, because it works on the Pixel's launcher. Whether to hide
  it, or to replace it with a swipe-up gesture, is the founder's call.
- **A test-harness mistake, recorded so it isn't repeated.** The first phone run loaded
  the compiled `web/.test-out/device-nav.js`. That file was still the last mutation
  ("Back without its key-up"), because the source had been restored but not
  recompiled. So Back looked broken on both phones. It was traced through `logcat`,
  which showed a key-down with no key-up, and then to the stale build. After any
  mutation run, finish with a clean `npm test` before using `.test-out`.

## Verification (2026-10-13)

- `npm test`: 62/62. `tsc --noEmit`: clean. `docker build`: OK (`batter:scrcpy5-nav`).
- **Mutation checks:** a no-op `nav()` in each handler, removing Back from right-click
  in each, a wrong Recents keycode, Back without its key-up, and removing each of the
  four left-button guards. Each one fails at least one test.
- **Control messages on the phones,** sent through the app's control WebSocket and
  checked with screenshots and the focused window:

  | Button | Pixel 8a (Android 17) | Galaxy A06 (Android 14) |
  |---|---|---|
  | Home | home screen | home screen |
  | All apps | drawer opens | **nothing** (One UI) |
  | Back | closes the drawer, leaves Recents | leaves Recents |
  | Recents | Recents | Recents |

- **In a browser:** headless Chromium (Playwright build 1243 decodes H.264) logged in
  and opened each phone's viewer. The live screen renders with the bar under it. Every
  button and right-click was clicked, with the results above. After the fix, a
  right-click in the Galaxy's Recents went home without opening the app under the
  pointer.
- **Phones left as found:** the Galaxy on its home screen and the Pixel back on the
  Wolfpack app. One side effect: a Recents press made from inside Recents switched the
  Galaxy to its Wolfpack app once, so that app is now among its recent apps.
