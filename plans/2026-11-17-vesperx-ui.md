# Plan: VesperX visual overhaul of the web UI

**Date:** 2026-11-17 · **Branch:** `feature/vesperx-ui` (worktree, from `main` @ e7917ec)
**Status:** reviewed and approved by Codex (4 rounds, see Review). Building.

## What

Restyle every screen of the Batter web app in the VesperX brand (screen mode, flat),
using the "VesperX console" mockup, and fix the usability problems the audit found
along the way. The mockup is saved in the repo as
`docs/design/2026-11-17-vesperx/vesperx.html` (with its screenshots) so reviewers can
see the target. It contains invented data only.

## Why

The founder asked for "a modern sleek and easy to use look". Four directions were
mocked up on 2026-11-17 (Blackfield refined, Minimal, Console, VesperX console). The
founder chose VesperX ("Okay try the vesperx"). A usability audit of `main`
(`docs/design/2026-11-17-vesperx/ux-audit.md`, issues numbered A1–A15 below) found
problems a restyle alone would not fix, and most of them are in scope. Today the app
is stock Tailwind gray with an indigo accent, emoji icons, no responsive layout, and a
viewer toolbar of twelve equal-weight 10px buttons.

## Scope

### In

1. **Design tokens.** VesperX colours, type, radius and spacing as CSS custom
   properties in `globals.css`, exposed to Tailwind as a `vx` palette
   (`bg-vx-surface`, `text-vx-fg-muted`, …). Values are the brand's
   (`#0A0E12` canvas, `#34D6A8` accent, …), plus the two AA fixes from the mockup:
   `--vx-fg-label` `#8A949E` and a 3:1 control border.
2. **Fonts.** See [Fonts](#fonts).
3. **Icons and navigation (A14).** The sidebar's emoji become `lucide-react` icons
   (already a dependency). Multi View and Multiplexer each get a one-line explainer:
   "watch many phones" and "type into many phones".
4. **Shared primitives** in `web/src/components/ui/`:
   - Button (primary, ghost, danger)
   - StatusChip (icon plus word)
   - Dialog and ConfirmDialog: real dialogs with `role="dialog"`, `aria-modal`, Esc to
     close, focus trap and focus return, and scrolling on short screens (A11)
   - Toast: stacked, with an error variant that stays until dismissed (A4)
   - Menu, for More: opens by keyboard, closes on Esc
   - Tile
   - Field: a label linked to its input, with a hint and an error slot (A12)
5. **App shell (A7).** The sidebar becomes a drawer behind a menu button below 820px.
   Heights use `dvh` instead of `100vh`. No horizontal page scroll at 390px.
6. **Screens restyled:** login, dashboard, device viewer, multi view, multiplexer,
   groups, the admin pages (users, teams, VPN, phone network), and every modal (device
   edit, wizard, reconnect, screen lock, Ethernet panel). This covers the mobile list
   in audit section 4.
7. **Dashboard (A2, A3, A8):**
   - status tiles that count and filter, search, and sort;
   - an attention banner, and status as icon plus word, shown once per card;
   - when a refresh fails, the last good list stays on screen under a
     "Connection lost, retrying" banner, instead of the grid being replaced by an error;
   - empty states that say why and what to do: "No phones in group X", "No phones
     match 'ab'", "You have no devices; ask an admin".

   The dashboard changes are computed client-side from the list the page already
   loads.
8. **Viewer (A1, A8):**
   - one primary toolbar: Unmute and volume, Keyboard, Clipboard, Screenshot, Wake;
   - the rest under More: Install APK, Screen off, device frame, nav buttons, swipe
     pad;
   - Paste and Copy move into the clipboard panel;
   - at phone width, the toolbar becomes a bottom bar;
   - the header shows the nickname, with the serial secondary;
   - the error and no-session states get Retry, Start session and Back.
9. **Keyboard (A6).** See [Keyboard focus](#keyboard-focus).
10. **Confirmations and outcomes (A5, A4).** See
    [Confirmations](#confirmations). Wake, Screen off and Screenshot show a toast.
    Failed actions show an error toast with a safe, user-facing message (see
    [Confirmations](#confirmations)); nothing fails silently.
11. **Roles (A9).** Controls the user's role can't use are hidden or disabled, with
    the reason ("Viewers can watch only"). The viewer header shows "Watch only" when
    control is denied. Controls that need a live connection are disabled while the
    control socket is down. The backend still enforces roles; this only stops the UI
    offering actions that will fail.
12. **Access (A10, A15).** Cards, menus and group rows are reachable and usable by
    keyboard and touch. No control appears only on hover. Text is 12px or larger.
    Focus is always visible. All text pairs pass AA.
13. **Forms (A12).** Real `<form>` elements with linked labels. Rules are shown as
    hints, not only placeholders. Server errors appear on the form, instead of being
    swallowed.
14. **Multi View and Multiplexer, without changing what they do (A13):**
    - Tiles show phone names instead of serials.
    - Mirrored tiles are labelled "Mirroring", instead of showing a `not-allowed`
      cursor.
    - While control is active, a persistent bar says "Typing goes to N phones", with a
      Stop button.
    - Broadcast nav buttons and "Send to all" ask first when more than one phone is
      targeted.
15. **Readable status (A3).** Raw codes (`decoder-error`, `denied`) map to plain text,
    and the raw code stays visible under Details. The mapping lives in a new
    `src/lib/device-status.ts`.

### Out (separate plans)

- **Bulk select with bulk actions on the dashboard (part of A2).** It needs new
  multi-device flows. Groups already have batch actions.
- **Merging Multi View and Multiplexer into one "Wall" page, and a page-size cap
  (rest of A13).** That changes what the features do, not how they look.
- **The mockup's "proposed" items:** latency per card, the session panel, the activity
  log, and per-group health from the backend.
- **A light theme.**
- **MFA.** The mockup shows it, but Batter has none. CLAUDE.md §5 requires it where the
  app owns login, so it should be its own plan.
- **Any backend change.** This is web-only.

## Keyboard focus

Today `DeviceInputHandler` and `MultiplexerInputHandler` listen for keys on `document`,
and call `preventDefault` on every key while a viewer or the multiplexer is open.
The new behaviour:

- **When keys go to the phone:** only while the device canvas has focus. The canvas
  gets `tabIndex=0` and an accessible label ("Phone screen, keys go to the phone").
  It takes focus on pointer-down, so one click on the screen means typing reaches
  the phone, as now.
- **Indicator:** the header shows `KEYS → PHONE` while the canvas has focus, and
  nothing otherwise.
- **Toolbar and other controls:** clicking a toolbar control moves focus to that
  control, so its keys stay in the page (Tab, Enter and Space work normally).
  Keyboard-only users reach the canvas by tabbing to it.
- **Copy and paste:** Ctrl or Cmd with C or V is intercepted (to copy from or paste to
  the phone) only while the canvas has focus. In text fields, the browser's own
  editing shortcuts and IME composition are untouched.
- **Text input:** the viewer's text bar and the multiplexer's "Send to all" field are
  ordinary inputs, unaffected. Enter-to-send keeps working.
- **Nav buttons and swipe pad:** they prevent `mousedown` default, so pressing them
  doesn't take focus away from the canvas. Typing keeps going to the phone after a
  swipe.
- **Esc:** inside an open dialog or menu, Esc closes it and is not sent to the phone.
  This holds because the canvas doesn't have focus then.
- **Held keys across a focus change:** a key pressed while the canvas had focus always
  gets its key-up sent, even if focus has moved by then. When the canvas loses focus,
  every key still held is released on the phone. A key pressed elsewhere sends nothing,
  including its key-up. So no key is left stuck down on the phone.

## Confirmations

Every confirmation names the target and the consequence. The confirm button repeats
the verb. A failed action shows an error toast and leaves the dialog open where
retrying makes sense. The toast shows server text only when it is on an allowlist:

- **Known message:** if the API's `error` field exactly matches one of the literal
  messages the handlers write ("name is required", "no active session for device",
  "invalid credentials", …), the toast shows it. The allowlist lives in a new
  `src/lib/api-error.ts`, built from the literal strings in `internal/api/handlers/`.
- **Anything else:** any other text, a 5xx, or a network failure shows a generic
  "Couldn't {verb}. Try again." The response text is not shown.

Status codes, response bodies and stack traces are never shown (CLAUDE.md §5).

The allowlist is needed because 15 handlers return a raw `err.Error()`, on 4xx as well
as 5xx (for example `devices.go:471`, `491` and `522`). The backend still sends that
text, and fixing it is a backend change, which is out of scope here. It is flagged as
a separate task.

| Action | Where | Dialog | On failure |
|---|---|---|---|
| Stop session | viewer | "Stop the session on {name}? {n} people are watching; it ends for all of them. The phone keeps running." → **Stop for everyone** | error toast, session state unchanged |
| Stop all | dashboard, group | "Stop {n} sessions?" with the list | per-device result toast |
| Delete device | card menu, edit | "Remove {name} from Batter? The phone is not touched; it can be registered again." → **Remove** | error toast |
| Revoke access | groups, teams | "Revoke {who}'s access to {what}? Their open sessions end now." → **Revoke** | error toast |
| Change role | users | "Make {user} an {role}?" plus what that role can do → **Make {role}** | error in the dialog |
| Disable user | users | "Disable {user}? They are signed out and can't sign in until re-enabled." → **Disable** | error in the dialog |
| VPN off | device, VPN | "Turn off the VPN for {name}? Its traffic is blocked by the kill switch until a profile is set." (the exact wording follows what the backend does) → **Turn off** | error toast |
| Ethernet port apply | phone network | "Apply port {port} to {name}? The phone's network restarts." → **Apply** | error in the dialog |
| Broadcast nav or "Send to all" | multiplexer | "Send {action} to {n} phones?" (only when n > 1) → **Send to {n}** | per-phone toast |

## Fonts

- **Files:** `web/src/app/fonts/archivo.woff2` (Archivo, variable, weights 100–900)
  and `web/src/app/fonts/jetbrains-mono.woff2` (JetBrains Mono, variable, weights
  400–800; checked with fontTools). Both are copied from SunSet's `docs/design/fonts/`.
- **Licences:** each font's `*-OFL.txt` licence text (with its copyright line) sits
  beside it, plus a `README.md` with the source and the SHA-256 of each file. The SIL
  Open Font License 1.1 allows bundling and redistribution, provided the copyright and
  licence notices travel with the files. These fonts are not covered by Batter's own
  licence.
- **Loading:** `next/font/local` in `layout.tsx`, with
  `src: './fonts/archivo.woff2', weight: '100 900', display: 'swap',
  variable: '--vx-font-sans'` (and the same for mono). The fallback is the system
  font stack.
- **Checks:** after `next build`, grep the build output for `googleapis` and
  `gstatic`; there should be no matches. During the browser check, the network log
  should show no request leaving the app's own origin.

## Approach

1. **Foundation (me, alone):** tokens, fonts, the Tailwind palette, the UI
   primitives, the app shell and sidebar, `device-status.ts`, and the keyboard-focus
   change in both input handlers (with tests). It lands as one commit. Its component
   APIs are then frozen; if an agent needs a change, it asks, and I make it.
2. **Screens, in parallel.** Four Sonnet coding agents work in this worktree, each
   owning only its own files. No agent edits a shared file.

   | Agent | Owns |
   |---|---|
   | A | `app/login/*`, `app/page.tsx`, `app/dashboard/*`, `components/device/device-card.tsx`, `device-grid.tsx` |
   | B | `app/devices/[serial]/*`, `components/device/device-viewer.tsx`, `nav-bar.tsx`, `swipe-pad.tsx`, `screen-lock-panel.tsx`, `ethernet-panel.tsx`, `reconnect-modal.tsx` |
   | C | `app/multiview/*`, `app/multiplexer/*`, `app/groups/*`, `components/device/multi-view-cell.tsx`, `multiplexer-grid.tsx` |
   | D | `app/admin/**`, `components/device/device-edit-modal.tsx`, `device-wizard.tsx` |
   | Me only | `components/ui/*`, `components/layout/*`, `app/layout.tsx`, `globals.css`, `tailwind.config.ts`, `src/lib/*`, tests, docs |

   The agents check their work with `npx tsc --noEmit` only. They do not run
   `npm test` or `next build`: both write shared output folders, and four agents
   running them at once would collide.
3. **Integration (me):** review every agent's diff against the plan and the mockup,
   fix inconsistencies, then run `npm test`, `tsc`, `next build` and `go test`. Then the
   browser walkthrough. Each screen group lands as its own commit.

## Files touched

- **Under `web/`:**
  - `tailwind.config.ts`, `src/app/globals.css`, `src/app/layout.tsx`
  - `src/app/fonts/*` (new) and `src/components/ui/*` (new)
  - `src/components/layout/*`
  - every page under `src/app/` and every component under `src/components/device/`
  - `src/lib/device-input.ts` and `src/lib/device-input-multiplexer.ts`
  - `src/lib/device-status.ts`, `src/lib/dashboard-filter.ts` and `src/lib/api-error.ts` (all new), with tests
- **Docs:** `docs/design/2026-11-17-vesperx/` (mockup, screenshots, audit),
  `docs/guides/TEST-MAP.md`, and `CHANGELOG.md` (new).

## Risks

- **Large diff.** Nearly every UI file changes, which makes it heavy to review. The
  foundation and each screen group land as separate commits.
- **Input regressions.** The keyboard change alters how keys reach the phone. It is
  covered by tests on the attached handlers and checked by typing into a phone.
- **Consistency across agents.** The agents share frozen primitives and tokens, own
  disjoint files, and I review every diff before it lands.
- **Same-vendor builders.** The coding agents are Claude (Sonnet). They build; they do
  not review. The review is by a different vendor (below).
- **Behaviour changes operators will notice:**
  - keys reach the phone only after its screen is clicked or focused;
  - destructive actions now ask first;
  - a viewer-role user sees fewer controls.

  All three are intended, and all three go in the changelog.

## Tests

**Automated, against production code:**

- **`device-status.test.ts`:** every backend status and error code the web receives
  maps to a label, an icon and a tone. Unknown values fall back to a neutral
  "Unknown" with the raw code, never blank.
- **`api-error.test.ts`:** an allowlisted message is shown as written. An unknown
  string, a raw Go error (`pq: …`, `exec: …`), a 5xx and a network failure all give
  the generic text. Every allowlist entry is also a literal string in the Go handlers,
  so a renamed message fails the test instead of silently falling back.
- **`dashboard-filter.test.ts`:** the tiles' counts match the list, each filter and
  search returns exactly the matching devices, and the empty-state reason is right
  for "group empty", "no match" and "no devices". The dashboard page imports these
  functions; the browser walkthrough checks that it shows their result.
- **Keyboard, on the attached handlers** (fake document with a settable
  `activeElement`; the handlers attach for real):
  - with the canvas focused, a letter sends `key`/`text` to the phone (positive
    case);
  - with a button or input focused, Tab, Enter, Space and letters send nothing and
    are not `preventDefault`-ed;
  - Ctrl+V and Ctrl+C (and Cmd on macOS) are intercepted only with the canvas
    focused; in a text field they keep their browser meaning;
  - a key pressed with the canvas focused still sends its key-up after focus moves,
    and blurring the canvas releases held keys; a key-up for a key pressed elsewhere
    sends nothing;
  - IME `isComposing` events are never intercepted;
  - the multiplexer behaves the same way.
- **Palette guard** (`palette-guard.test.ts`): it scans `src/**/*.tsx` and `*.ts`,
  excluding tests, for Tailwind colour utilities on the old palettes. The pattern is
  `(bg|text|border|ring|divide|placeholder|outline|fill|stroke|from|via|to|accent|caret|decoration|shadow)-(gray|brand|indigo|slate|zinc|neutral)-\d{2,3}`,
  with an optional opacity suffix. It ignores comments and other class names. It
  includes a self-test proving it catches `bg-gray-800` and passes `font-brand` and
  `bg-vx-surface`. The emoji check is limited to navigation icons in
  `components/layout/`.
- **Mutation check:** each new test is checked against the production code by
  reverting or breaking it and confirming a failure.
- **Existing suites:** `npm test`, `tsc`, `next build`, and `go test ./...`
  (unchanged).

**Verified by looking (§2.4, §7 viewpoints).** Every screen, dialog, panel and menu
goes into `docs/guides/TEST-MAP.md`, with how it is reached, what it shows, and its
conditional states:
- empty, error, loading, watch-only and disconnected states;
- each confirmation;
- the drawer open, the More menu, the clipboard panel, and the toast stack.

Each row is marked as automated or checked in the browser walkthrough, with the
screenshot that proves it.

## Verification (§2.4)

Run the real app: Postgres in Docker, the backend with `test/fakeadb` as adb (one fake
phone, FAKE01), extra device rows seeded for the grid, and the production build
(`next build` + `next start`). Then, in headless Chromium (Playwright build 1243,
which decodes H.264):
- screenshot every screen and state in the TEST-MAP at 1440 and 390;
- assert that no page scrolls sideways at 390 (`scrollWidth <= innerWidth`);
- click through every confirmation, the More menu and the drawer;
- tab through the viewer toolbar, and check focus and the `KEYS → PHONE` indicator;
- sign in as a viewer-role user and check the watch-only state;
- record the network log and confirm no off-origin requests;
- if a real phone is cabled to runcho, type into it and check what it receives.

Screenshots go in `plans/progress/2026-11-17-vesperx-ui/`.

## Release

The web UI ships in the Batter image. This change starts `CHANGELOG.md`, with an entry
saying what operators will see: the new look, keys going to the phone only once its
screen is focused, confirmations, and the watch-only state.

Per §2.7, a version that leaves the building is not released until both of these
exist:
1. the `CHANGELOG.md` entry;
2. the portal release notes (`content/manuals/batter-release-notes.md` in the `docs`
   repo, deployed with `npm run deploy`).

The portal notes are a release-time step for whoever cuts the version. Merging this
branch alone is not a release.

## Roles

- Orchestrator and Tech Architect: Claude Opus 5.5 (this session).
- UI Designer: Claude Sonnet agents (the mockups).
- Coders: Claude Opus 5.5 (foundation and integration) and Claude Sonnet agents
  (screens).
- Reviewer: Codex (OpenAI), a different vendor.

## Review

**Round 1: Codex CLI 0.160.0, 2026-11-17. APPROVE WITH CHANGES.** The output is
kept in the session scratchpad and summarised here. Each finding and how the plan now
handles it:

1. **HIGH, audit fixes left out.** The plan now brings in A8 (last good data, empty
   and error states), A9 (role-aware controls), A10–A12 (access, real dialogs, forms),
   the parts of A13 that don't change what the features do, and disabling controls
   while disconnected. The deferrals (bulk actions, the Wall merge) are listed with
   reasons.
2. **HIGH, keyboard focus behaviour.** Now specified case by case: canvas focus,
   toolbar clicks, copy and paste, IME, the text bars, the swipe pad and nav buttons,
   and Esc. Tests are on the attached handlers.
3. **MED, fonts.** Now gives the file paths, weight ranges and `next/font/local`
   settings, keeps the licence and copyright notices beside the files, and checks the
   build and the network log.
4. **HIGH, parallel agents.** The foundation lands first with frozen APIs. There is
   an ownership table, and shared files are mine only. Agents run only `tsc`, and I
   integrate before verifying.
5. **HIGH, tests and §7.** The TEST-MAP covers every screen, dialog, panel and state,
   marked automated or browser-checked. The keyboard tests use the attached
   handlers, and the dashboard functions are imported by the page.
6. **MED, palette guard.** It now targets Tailwind colour utilities only, with a
   stated pattern and a self-test. The emoji check is limited to the navigation
   icons.
7. **MED, confirmations.** There is a table naming the target, the consequence and
   the failure feedback for each action, including disable user and delete device.
8. **MED, release.** The portal release notes are named as a release-time step, and
   no release is complete without both records.

**Round 2: Codex CLI 0.160.0, 2026-11-17. APPROVE WITH CHANGES.** Findings 1 and 3–8
were resolved. Two items remained, and both are now in the plan:

- **Finding 2, not resolved.** The keyboard tests covered Ctrl+V but not Ctrl/Cmd+C,
  and didn't cover key-up after focus moves. The Keyboard section now states the
  held-key rule, and the tests list covers both.
- **New, MED.** "The server's message" in an error toast could leak internal detail.
  Toasts now show the `error` field for 4xx responses only, and a generic message for
  5xx and network failures. Checking this turned up 15 handlers that return a raw
  `err.Error()`. That backend issue is flagged as its own task.

**Round 3: Codex CLI 0.160.0, 2026-11-17. APPROVE WITH CHANGES.** The keyboard item
was resolved. The error-toast item was not: some 4xx responses also carry a raw
`err.Error()` (`devices.go:491`, `522`), so showing every 4xx `error` field was still
unsafe. Toasts now show server text only when it exactly matches an allowlist of the
handlers' literal messages (`api-error.ts`, with a test tying the list to the Go
source). Everything else gets the generic message.

**Round 4: Codex CLI 0.160.0, 2026-11-17. APPROVE.** "RESOLVED — only exact
allowlisted handler messages are shown." No new HIGH or MED issues.

**Consensus:** reached on 2026-11-17, with Codex approving the plan as written.
Presented to the founder, who had already chosen this direction ("Okay try the
vesperx"); the build starts from this version.
