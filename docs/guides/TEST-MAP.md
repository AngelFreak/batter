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
| Device viewer: video renders, touch/scroll/keys, rotation, audio, thumbnails, two viewers | **untested in the repo.** By hand on a real phone, see `plans/progress/2026-10-06-scrcpy-5.md` | phone |
| Server jar integrity | Dockerfile `sha256sum -c` against upstream's signed SUMS | `docker build` |

Mutation-checked 2026-10-06. Reverting each of these fails at least one row above:
the flag bits, the per-event size read, session-packet consumption, the audio
session guard, the handshake check, and the browser parser bits.
