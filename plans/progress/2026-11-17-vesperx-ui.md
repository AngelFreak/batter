# Progress: VesperX UI overhaul (2026-11-17)

Plan: `plans/2026-11-17-vesperx-ui.md`. Screenshots: `plans/progress/2026-11-17-vesperx-ui/`.

## How it was built

1. **Foundation (Opus).** Tokens, fonts, primitives, the app shell, the libraries and
   the keyboard rule. Committed alone (6a271a0).
2. **Four screen agents (Sonnet), in parallel, each on its own files:**
   - A: login, dashboard, card, grid, edit modal and wizard. The two modals moved
     from D to A to balance the load; they open from the dashboard.
   - B: the viewer and its panels.
   - C: Multi View, Multiplexer and Groups.
   - D: the admin pages.

   They type-checked only; no agent ran the tests or the build.
3. **Integration (Opus).** I reviewed each agent's report and diff and fixed what
   I found:
   - **C removed Multi View's click-to-control.** That would have changed what the
     page does, so C was sent back to restore it.
   - **Keyboard trap.** Tab is sent to the phone, so a keyboard user couldn't leave
     the screen. Shift+Esc now hands focus back to the page (B reported this).
   - **`w-touch` was missing** from the Tailwind config, and disabled buttons
     couldn't show their reason (B reported both).
   - **Revoke wording.** C's group revoke dialog said "Their open sessions end now",
     taken from the plan. The backend doesn't do that (D found it), so the dialog
     now says what does happen.
   - **Own role.** An admin could open "Make … operator" on their own row; it is now
     disabled. The backend still allows it: see Follow-ups.
   - **Small fixes:** the sign-in form showed the password rule; names were cut short
     in the stacked user cards; the Groups list was centred under a full-width header.
   - **Watch only never appeared.** It's found in the walk: see below.

## Watch only was unreachable (found in the walk)

The control socket's permission check runs before the WebSocket upgrade and answers
403. A browser only sees a failed connection, so a user with view-only access saw
"Reconnecting" forever, never "Watch only". This was already true before the restyle;
the new UI just made it visible.

The fix is web-only, in `src/lib/control-probe.ts`:
- **The probe.** When the control socket closes before it ever opened, the client
  fetches the same URL once as plain HTTP. The middleware answers 403 there too, and
  the client then reports `denied` and stops retrying.
- **Why it's safe.** With permission, the handler answers 404 or 400 before claiming
  control, so the probe changes nothing on the phone. The cost is one
  "failed to upgrade control websocket" line in the server log per failed connect.
- **Tests.** Both handlers are tested and mutation-checked.

## Verification rig

- **Postgres:** `postgres:16-alpine` on 127.0.0.1:5439.
- **Backend:** built from this branch, on 127.0.0.1:8080, with `test/fakeadb` as adb
  (one phone, FAKE01, `FAKEADB_STREAM_FPS=10`).
- **Seed data:**
  - an admin, an operator, and a viewer with a view grant on FAKE01;
  - five extra device rows;
  - two groups.

  The passwords were generated and kept outside the repo.
- **Web:** the production build (`next build`, then `next start` on 127.0.0.1:3123).
- **Browser:** Playwright 1.57 driving headless Chromium build 1243, at 1440×900 and
  390×844.

Four passes, 60 checks (59 passed):

| Pass | What it covered | Result |
|---|---|---|
| 1 | Every page at both widths, with no sideways scroll | 18/18 |
| 2 | Filters, empty and lost-connection states, delete confirm, card menu, viewer (keys marker, Shift+Esc, Wake toast, More, Clipboard, Stop confirm), drawer, Users role confirm, Groups expanded, Multiplexer banner, viewer role (no Add device, no Admin, Watch only, Wake disabled) | 23/23, after the fixes above |
| 3 | Edit modal and focus return, wizard, own-role lock, VPN dialog, Multi View click to control, reconnect modal (stubbed), "Still starting" after 15s | 9/10 |

| 4 | After code review: rebuilt server text on the phone-network page (API stubbed): warning, missing port, known and unknown refusals, malformed addresses, 390 with no sideways scroll | 8/8 |

Pass 4 also confirmed that the viewer's audio tooltip shows no raw reason. That
check is weak: fakeadb never reports audio as unavailable, so the browser never
shows the rebuilt sentence, and `server-text.test.ts` is the only evidence for it.
Screenshots: `phone-network-warning-1440.png` and `phone-network-missing-390.png`.

The one pass-3 failure was the script's: it clicked "Create team" with an empty name,
and that button is disabled by design. No pass made a request outside the app's
origin.

**Two kinds of state couldn't be reached with real data:**
- **Attention states.** fakeadb has one phone, and the device list overwrites stored
  statuses with what adb reports. So the attention and reconnect screenshots
  (`*-STUBBED-*`) replace `/api/v1/devices` in the browser.
- **Live video.** fakeadb's frames aren't real H.264. No screenshot shows video in the
  new layout, and no real phone was used this time.

## Checks

- `npm test`: 119/119, including `palette-guard`'s full scan of `src`.
- `tsc --noEmit`: clean.
- `next build`: clean.
- **No font CDN.** The rendered pages and CSS reference neither `googleapis` nor
  `gstatic`; the fonts load from `/_next/static/media`. The only `googleapis` string
  is a constant inside Next's own runtime, and the walk saw no off-origin request.

## Follow-ups (not in this change)

- **Backend: self-demotion.** It allows changing your own role, including demoting the
  last admin (`handlers/users.go` UpdateUser). The UI now blocks it for your own row;
  the API should too.
- **Backend: revoke.** Revoking access doesn't end the person's open sessions. The plan
  assumed it did; the UI now says what actually happens.
- **Backend: raw errors.** About 15 handlers return a raw `err.Error()`. The UI's
  allowlist hides them, but the API still sends them.
- **Backend: control denial.** The control socket could report denial in-band, which
  would make the probe unnecessary.
- **VPN config errors** ("line N: …") are dynamic, so they show as the generic message.
  They can echo config content, so they are deliberately not allowlisted.
- **Not walked:** live video on a real phone in the new layout, the multiplexer with two
  or more phones (the confirmations), the screen-lock panel, the no-session and decoder
  error states, and first-run setup.
