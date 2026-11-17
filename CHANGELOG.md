# Changelog

What an operator can now do or will now see, per version. Batter ships as one
Docker image, so a version covers both the web UI and the backend. Each released
version is also published in the portal release notes
(`content/manuals/batter-release-notes.md` in the `docs` repo), per the VesperX
baseline §2.7.

## Unreleased

### Changed: a new look, in the VesperX brand

Every screen is restyled: dark and square, with one green accent for what is
live. Status is shown as an icon and a word everywhere. The app now works on a
phone: below 820px wide, the menu is behind a button.

### Changed: keys go to a phone only when its screen has focus

Click or tab to the phone's screen to type into it. A `KEYS → PHONE` marker
shows when they do, and Shift+Esc hands the keyboard back to the page. The
viewer no longer takes the keyboard when it opens. Typing in a text field, or pressing Tab or Enter on a
button, now stays in the page, instead of also going to the phone. Ctrl/Cmd+C
and V copy from and paste to the phone only from its screen. The multiplexer
works the same way.

### Added

- **Confirmations before actions that are hard to undo:** stopping a session
  (it ends for everyone watching), removing a phone, revoking access, changing
  a role, disabling or deleting a user, deleting a VPN profile, applying a
  phone-network port, and sending a nav button or text to more than one phone
  from the multiplexer.
- **Feedback for every action:** Wake, Screen off and Screenshot confirm with a
  message. Failures say what went wrong, using only the server's known
  messages.
- **Dashboard:**
  - status tiles that count and filter;
  - search and sort;
  - a banner for phones that need attention;
  - empty states that say why the list is empty.

  When the server can't be reached, the last list stays on screen with a
  "Connection lost" notice.
- **Device viewer:**
  - one toolbar, with the rest under More;
  - a bottom bar on phones;
  - the phone's name in the header;
  - Retry and Start session when a stream fails or never starts;
  - "Watch only" when your access doesn't include control. Before this
    version it showed as "Reconnecting" forever.
- **Multiplexer:**
  - a bar saying how many phones your typing goes to, with Stop;
  - the other screens are labelled "Mirroring".

### Why the version moves

The UI changes how keyboard input reaches phones, and adds confirmation steps
to existing actions. Operators will notice both.
