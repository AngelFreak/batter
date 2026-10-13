# Diff review: Claude Sonnet (same vendor; an extra pass, NOT the §2.2 gate)

- **Reviewer:** Claude Sonnet (`sonnet`), run as a `pr-review-toolkit:code-reviewer` subagent, read-only
- **Scope:** `git diff origin/main...feature/scrcpy-5` (PR #6), against the upstream v3.3.4/v5.0 sources
- **Date:** 2026-10-06
- **Verdict:** APPROVE WITH CHANGES

## Findings and disposition

1. **MED: missed flag migration in `test/proxyit/proxy_test.go`.** The test still
   treated `msg[0]&0x40` as the keyframe flag. Since 5.0 that's the config bit, and a
   new viewer is sent the stored config packet first, so the test passed on the first
   message without proving a keyframe crosses the proxy.
   **Fixed:** the test now checks keyframe `0x20` and requires that config `0x40` is not set.
   **Verified:** passes against fakeadb. With fakeadb's keyframe flag zeroed (temporary
   mutation, reverted), it fails with "no keyframe over the WebSocket".
   The author's own repo-wide grep had looked for `& 0x80` but not `& 0x40`.
2. **LOW: `internal/config` imports `internal/device`** for `ServerVersion`. There's
   no cycle today. **Not changed.** If `device` ever needs `config`, move the constant
   to a small leaf package.

Reviewer-confirmed correct: the session-packet byte layout against
`Streamer.writeSessionMeta`, the flag shift in Go and TS, the `Session.size` atomic
(single writer, no direct field reads left), the per-event size read in `ControlStream`,
the audio guard, and the Dockerfile/install-script hash checks.

## Still open

The §2.2 different-vendor review (Codex/Grok/Gemini or a human) has **not** happened.
This Sonnet pass does not satisfy it.
