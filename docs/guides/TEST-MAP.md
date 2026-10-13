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
