# Test map

What Batter can prove about itself, and what it can't yet (VesperX baseline §7).
The map was **started 2026-10-06 with the scrcpy device stream only**. Every other
function and viewpoint is unmapped until someone adds it here. Don't read a
missing row as "covered".

Rig key: **unit** = `go test` / `npm test` alone. **fake phone** = the real session
code against `internal/device/scrcpytest` or `test/fakeadb`, over loopback.
**DB** = also needs `BATTER_TEST_DATABASE_URL`. **phone** = a real phone, by hand.

## Device stream (scrcpy-server 5.0 protocol)

| Function / viewpoint | Evidence | Rig |
|---|---|---|
| Handshake: name + codec, then a session packet gives the size | `TestSessionStreamsOpusAudioAlongsideVideo`: `Size()` is 1080×2400 from the fake's session packet | fake phone |
| A pre-4.0 server is refused, not misparsed | `TestHandshakeRejectsOldFraming`: v3 framing makes `newSession` fail with "unexpected first video packet" | fake phone |
| Rotation updates the size; session packets never reach viewers | `TestRotationUpdatesSizeWithoutReachingViewers`: after a 2400×1080 session packet the viewer's first packet is the config packet, then the keyframe, and `Size()` is 2400×1080 | fake phone |
| Touch after rotation lands (server drops stale sizes) | `TestTouchAfterRotationCarriesNewSize`: the touch bytes the phone receives carry 1080×2400, then 2400×1080 after rotation | fake phone |
| Config / keyframe flags (bits 62/61) drive viewer resync | `TestSlowViewerSkipsToKeyframe…`, `videosub_test.go` | unit |
| Audio config flag (bit 62); audio ends on a session packet | `TestSessionStreamsOpusAudioAlongsideVideo`, `TestAudioEndsOnSessionPacket` (a session packet with a plausible size is not played) | fake phone |
| Keyframe on viewer join, shared between joiners | `TestViewersJoiningTogetherShareKeyframes` (fakeadb, v5 framing) | fake phone + DB |
| Browser header decode (session/config/key/delta, PTS mask) | `scrcpy-packet.test.ts`; `device-audio.test.ts` decodes through the real player | unit |
| Real scrcpy-server 5.0: handshake, decode, touch/text/keys/scroll, rotation + touch after it, audio, thumbnail tier, two viewers | **not automated.** Run by hand 2026-10-06 against an emulator through the API/WebSockets; see `plans/progress/2026-10-06-scrcpy-5.md` | emulator (by hand) |
| Device viewer page in a browser (canvas, input mapping, thumbnails grid) | **not automated.** Seen 2026-10-13 by hand in headless Chromium (Playwright build 1243, which decodes H.264): the Pixel 8a and Galaxy A06 render live under 5.0.1, and control connects. The thumbnails grid was not looked at. The page overflows at 390px wide (sidebar doesn't collapse), which is an existing problem | Chrome + phone |
| Server jar integrity | Dockerfile `sha256sum -c` against upstream's signed SUMS | `docker build` |

Mutation-checked 2026-10-06. Reverting each of these fails at least one row above:
the flag bits, the per-event size read, session-packet consumption, the audio
session guard, the handshake check, and the browser parser bits.

## Navigation buttons (viewer and multiplexer)

| Function / viewpoint | Evidence | Rig |
|---|---|---|
| Back / Home / Recents / All apps send the right control messages (down then up) | `device-nav.test.ts`: keycodes 3, 187 and 284; Back is scrcpy's back-or-screen-on | unit |
| The viewer's buttons and right-click reach the control socket | `device-nav.test.ts`: `DeviceInputHandler.nav` and a right-click put exactly those messages on the socket | unit |
| A right-click is Back only, never also a tap; a left click still taps | `device-nav.test.ts`: full right-click (down, contextmenu, up with button 2) sends only Back; left click sends touch down/up | unit |
| The multiplexer broadcasts each press, and right-click, to every device | `device-nav.test.ts` | unit |
| The bar renders under the phone, and each button works on a real phone | **not automated.** 2026-10-13, by hand: buttons clicked in headless Chromium against the Pixel 8a (Android 17) and Galaxy A06 (Android 14). Home, Recents and Back work on both; right-click leaves Recents on both. **All apps does nothing on the Galaxy**, because One UI's launcher ignores KEYCODE_ALL_APPS; it opens the drawer on the Pixel | Chrome + phone |

Mutation-checked 2026-10-13: a no-op `nav()`, removing Back from right-click, a wrong
keycode, Back without its key-up, and removing the left-button guard from each mouse
handler. Each one fails at least one row.

## Web UI (VesperX restyle, 2026-11-17)

Plan: `plans/2026-11-17-vesperx-ui.md`. Screenshots: `plans/progress/2026-11-17-vesperx-ui/`.

Rig key additions:
- **walk** means the production build (`next build` + `next start`) against the real backend and `test/fakeadb`, driven by Playwright in headless Chromium (build 1243) at 1440 and 390 wide. The walkthrough script is described in the progress notes.
- **walk (stubbed)** means the same rig, but with `/api/v1/devices` replaced in the browser, because fakeadb has one phone and adb overwrites stored statuses.
- fakeadb's video frames are not real H.264, so **no walk row proves video renders in the new layout.**

### Functions

| Function | Evidence | Rig |
|---|---|---|
| Keys reach the phone only while its screen has focus | `canvas-keys.test.ts`, on the attached handlers of both the viewer and the multiplexer: a key on the focused screen sends key-down and key-up; Tab, Enter, Space and letters typed elsewhere send nothing and aren't `preventDefault`-ed | unit |
| A key held on the screen is released when focus moves | `canvas-keys.test.ts`: key-down, then blur, then key-up elsewhere sends exactly one key-up; a key-up for a key pressed elsewhere sends nothing | unit |
| Ctrl/Cmd+C and V copy and paste with the phone only from its screen | `canvas-keys.test.ts`: outside the screen they keep their browser meaning; on it, they read the clipboard or send `get_clipboard` | unit |
| IME composition is never intercepted | `canvas-keys.test.ts` | unit |
| Shift+Esc leaves the screen; plain Esc is Back | unit: `canvas-keys.test.ts`. walk: the `KEYS → PHONE` marker shows on focus and goes after Shift+Esc | unit + walk |
| A press on the screen focuses it | `canvas-keys.test.ts` (`focus()` on mousedown, both handlers) | unit |
| Watch only for a phone the user may not control | unit: `device-input.test.ts` and `device-input-multiplexer.test.ts` (403 probe gives `denied` and no retry; other failures keep reconnecting; an opened socket isn't probed). walk: the viewer user with a view grant on FAKE01 sees "Watch only" and a disabled Wake | unit + walk |
| Error text: only the API's literal messages are shown | `api-error.test.ts`: the allowlist equals the Go handlers' `"error": "…"` literals in both directions; raw Go errors, fallbacks and network failures give the generic sentence | unit |
| Server status text: phone-network warning, missing port, port refusals and audio reasons are rebuilt from known templates; anything else becomes a fixed sentence | `server-text.test.ts`: templates tied to the Go source; validated names, MACs and IPv4/IPv6 prefixes; injected and malformed text replaced. Browser pass 4 (stubbed API) on the phone-network page. The audio tooltip has no browser evidence, because fakeadb never reports audio as unavailable | unit + browser (stubbed) |
| Status labels | `device-status.test.ts`: every device status and stream code has a word and a shape; only streaming uses the accent; unknown values show "Unknown" and keep the code | unit |
| Dashboard tiles, filter, search, sort, empty reasons | unit: `dashboard-filter.test.ts`. walk: the Offline tile toggles; "zzz" gives the no-match state; Clear filters works | unit + walk |
| Only VesperX styling in the source | `palette-guard.test.ts`: no stock-palette colour classes, `dark:`, white or black, or text under 11px anywhere in `src`; a self-test proves it catches each kind | unit |

All rows above were mutation-checked on 2026-11-17: breaking the production code makes at least one of the named tests fail.

### Viewpoints

All checked in the walk on 2026-11-17 unless marked. The walk checks for no sideways scroll (page and `<main>`) on every page at 1440 and 390, and records no request leaving the app's origin.

| Viewpoint | How it's reached | What was checked | Rig |
|---|---|---|---|
| Sign in | `/login` | Form, labels, no sideways scroll at 390 | walk |
| First-run setup | `/login` on an empty DB | **Not re-checked** after the restyle | — |
| Dashboard | `/dashboard` | Tiles, toolbar, grid, card menu | walk |
| Dashboard: attention banner | a phone that needs USB or approval | Banner text, and Reconnect for an admin | walk (stubbed) |
| Dashboard: connection lost | a devices poll fails | Banner shows and the last list stays | walk |
| Dashboard: empty states | no match / empty group / no devices | No match only; the others are covered by unit tests | unit + walk |
| Remove phone confirm | card menu, then Delete | "Remove {name} from Batter?"; Esc closes it | walk |
| Drawer | below 820px, the menu button | Opens; Esc closes it | walk |
| Viewer | `/devices/FAKE01` | Header, chips, toolbar, bottom bar at 390. Video frames are **not** checked (fakeadb) | walk |
| Viewer: More menu, Clipboard panel | toolbar | Opens, items listed | walk |
| Viewer: Stop session confirm | Stop session | "Stop the session on Desk 1?" | walk |
| Viewer: toasts | Wake | "Wake sent" | walk |
| Viewer: watch only | viewer user with a view grant | "Watch only", and controls disabled | walk |
| Viewer: still starting | no video for 15s (fakeadb frames never decode) | "Still starting…" with Retry and Back | walk |
| Viewer: no session, decoder error | stopped session / bad stream | **Not reached in the walk** | — |
| Multiplexer: picker, active banner | `/multiplexer`, Desk 1, Start | "Typing goes to 1 phone", Stop | walk |
| Multiplexer: confirm when n > 1 | two phones | **Not checked**: fakeadb has one phone | — |
| Multi View: click to control | `/multiview`, click Desk 1 | "Controlling Desk 1" in the header | walk |
| Groups, expanded | `/groups`, Desk row | Rows, tabs | walk (screenshot) |
| Users, role confirm | `/admin/users`, row menu | "Make … an operator?"; on your own row it's disabled | walk |
| Teams, VPN, Phone network | `/admin/…` | Render at both widths. VPN "new profile" dialog opens. Teams "Create team" stays disabled with an empty name. Other dialogs **not** opened | walk |
| Edit modal | card menu, then Edit | Opens as a dialog; Esc returns focus to the menu button | walk |
| Wizard | Add device | "STEP 1 OF 7", the scan lists FAKE01 | walk |
| Reconnect modal | attention banner, then Reconnect | Opens as a dialog | walk (stubbed) |
| Screen-lock panel | wizard prepare step | **Not reached in the walk** | — |
