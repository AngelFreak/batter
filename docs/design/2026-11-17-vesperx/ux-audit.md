# Batter web UI: usability audit (static read of origin/main, `web/src`)

Scope: every page under `web/src/app` and every component under `web/src/components`, plus the input and API libs where they decide UX behaviour. App not run; widths and overflow are estimated from classes. Line numbers are origin/main. Paths are relative to `web/src/`. "Viewer" means the single-phone live view (`components/device/device-viewer.tsx` + `app/devices/[serial]/page.tsx`).

Quantitative signals across the codebase: `text-[10px]` x101, `text-gray-500` x108, `text-gray-600` (as text) x10, `placeholder-gray-600` x17, `focus:outline-none` x26 vs `focus-visible`/`focus:ring` x0, responsive prefixes (`sm:/md:/lg:`) in exactly 1 file (`multiplexer/page.tsx:188`), `aria-*` in 4 files, `role=` once (VPN switch), `confirm()` once, `<form>` once (login). Contrast (computed): gray-500 on gray-900 = 3.67:1, gray-500 on gray-800 = 3.04:1, gray-600 on gray-900 = 2.35:1, gray-600 on gray-800 = 1.94:1 (WCAG AA needs 4.5:1 for small text). gray-400 is fine (5.8 to 7.9:1).

---

## 1. Top 15 issues, ranked by operator impact

### 1. The viewer toolbar is 12 equal-weight 10px buttons that overflow
- Where: `components/device/device-viewer.tsx:238-343` (Mute 239, volume 251, Frame 264, Buttons 272, Swipe pad 281, Keyboard 290, Clipboard 298, Paste 306, Copy 312, Install APK 318, Screenshot 324, Wake 331, Screen Off 337); Quality select and Stop Session are a second control row in `app/devices/[serial]/page.tsx:131-156`.
- What goes wrong: one flat row, `flex items-center gap-2` with no wrap and no overflow handling. By my estimate it needs about 1050px beside the status text, and the content area is viewport minus the 224px sidebar, so it already overflows on a 1280px laptop and badly at anything narrower. Nothing says which actions matter (Screenshot and Screen Off have the same weight as Frame, a cosmetic toggle). Labels are ambiguous: "Buttons" (which buttons?), "Frame" (what frame?), "Swipe pad". Paste/Copy (306-317) duplicate the Clipboard panel's From Device/To Device (410-426) and the Ctrl+V/Ctrl+C shortcuts (`lib/device-input.ts:173-187`). Wake and Screen Off are two buttons for one state. Toggle buttons use the same `bg-brand-600` as primary actions like "Send" (446), so "on" and "do it" look identical, and only the audio button has `aria-pressed` (243).
- Fix: group per section 3 below: status chip left, a short primary cluster, one View popover (Frame, Nav buttons, Swipe pad, Quality), one Clipboard popover, one More menu. Use icon + tooltip for the primaries, and `aria-pressed` on every toggle. Make the bar `flex-wrap` or collapse to the menu below a breakpoint.

### 2. The device grid does not scale to 20-100 phones (no search, sort, status filter or bulk action)
- Where: `app/dashboard/page.tsx:130-176` (header controls), `components/device/device-grid.tsx:29` (fixed `minmax(180px,1fr)` cards), `app/multiplexer/page.tsx:63,188-209`, `app/groups/page.tsx:369-382`.
- What goes wrong: the only way to find a phone is to scan the grid. The only filter is the group dropdown, and it is hidden entirely when no groups exist (141). There is no text search by nickname or serial, no status filter ("show only offline / unauthorized / restarted"), no sort, no card-size control, no multi-select. To drive a subset in the multiplexer you re-pick phones one by one (only "Select All" exists), and a group cannot be sent to Multi View or the Multiplexer. The only group actions are Start All / Stop All. "x registered, y connected, z streaming" (133-137) is 12px gray-500 text, not a fleet-health summary you can click.
- Fix: sticky filter bar with search, status chips with counts (Connected, Streaming, Offline, Unauthorized, Needs USB) and a group select that is always visible. Add a card-size toggle (S/M/L). Add multi-select with a bulk bar: Open in Multiplexer, Open in Multi View, Start sessions, Add to group. Add "Open group in Multi View / Multiplexer" on each group row. Put `Needs USB` ahead of healthy phones by default.

### 3. Device state is not readable at a glance; it is shown twice and partly by colour only
- Where: `components/device/device-card.tsx:16-21` (status colour map), `142-149` (badge: only "Connected" or "Disconnected"), `162` (second indicator, a dot), `152-156` (raw status text overlay); viewer `device-viewer.tsx:212,228-230`.
- What goes wrong: each card has two overlapping indicators. The badge collapses `offline`, `unauthorized` and `disconnected` into "Disconnected", while only the small dot distinguishes them by colour (yellow/red/gray, 8px, no text). "Unauthorized" means the operator must tap Allow on the phone and "offline" means replug; they need different actions. A live phone shows no "Live" marker, only the absence of an overlay. The overlay prints raw internal strings (`connecting`, `reconnecting`, `decoder-error`, `needs-https`, `error`; see `lib/device-video.ts:79-126,279-282`). The viewer status bar prints them too, plus "Control: denied/reconnecting" (`lib/device-input.ts:71-94`) in 12px gray. `denied` is a permissions problem but looks like a transient glitch. Elsewhere the same state is red/green/gray dots only (`groups/page.tsx:470,492`, `multi-view-cell.tsx:60`, `multiplexer-grid.tsx:36`).
- Fix: one status model with one component: icon + word + colour (Live, Ready, Connecting, Offline, Needs Allow on phone, Needs USB, No access). Map internal codes to plain sentences with a next step ("Tap Allow on the phone"). Reuse it in the card, viewer header, groups, multi view and multiplexer. Show Live explicitly. Show the connection kind (USB / Ethernet, VPN profile) on the card, not only in the edit modal.

### 4. Actions fail or succeed silently; the toast is single-slot and 2 seconds
- Where: `device-viewer.tsx:55-58` (toast), `102-117` (screenshot), `331-342` (Wake, Screen Off), `306-317` (Paste, Copy), `156-166` (APK and file push), `383-387` (toast placement); `lib/device-input.ts:106-109` (`send` drops messages unless control is open).
- What goes wrong: (a) Wake, Screen Off and Screenshot give no confirmation. (b) `send()` silently drops anything while control is reconnecting, but the toolbar stays enabled (only the nav bar and swipe pad honour `controlStatus`, at 380 and 391), so clicks vanish. (c) `handleScreenshot` has `try/finally` and no `catch`, and `fetchScreenshot` returns `null` on any failure, so a failed capture produces nothing, and a successful one only downloads with no confirmation. (d) The toast timer is never cleared (57), so a second toast is erased by the first one's timer; every message, including "Failed: ..." errors, disappears after 2s. A long APK install shows "Installing x..." for 2s and then nothing until it ends. (e) Stop Session, delete, role change, save: see 5 and 12.
- Fix: disable control-dependent buttons unless `controlStatus === 'connected'` with a tooltip saying why. Use a toast queue (clear the previous timer, 4s default, errors sticky with Dismiss). Show a persistent progress chip for installs and pushes. Add a `catch` and a visible "Screenshot failed" message.

### 5. Destructive or disruptive actions have no confirmation, or the wrong kind
- Where and what:
  - Stop Session (`app/devices/[serial]/page.tsx:150-155`, handler `106-113`): one click, the session is shared (the page itself says so at 146), so it also blacks out other operators' views and the dashboard thumbnails. It sits next to the Quality select, away from the canvas, and errors are swallowed (110-112).
  - Stop All on a group (`app/groups/page.tsx:376-382`): no confirm, and Start All and Stop All sit in the clickable card header.
  - Revoke access, remove a device from a group (`groups/page.tsx:502,536,566`; `admin/users/page.tsx:409`; `admin/user-groups/page.tsx:469`): a single click on a hover-only 14px icon; revoking access is security-relevant.
  - Role change fires on `<select onChange>` (`admin/users/page.tsx:335-337`), including to Admin and including your own account. Disable/Enable user (347-356) is a one-click toggle. Neither has a confirm and both swallow errors (172, 181).
  - VPN enable switch (`admin/vpn/page.tsx:226-231`): turning a profile off cuts internet for all its phones (the page's own copy says so at 222), with no confirmation. The Delete button on the same page does warn with a count (306), so the pattern exists.
  - Phone network "Apply" (`admin/phone-network/page.tsx:149-162`): the page says "the box itself has no connection on it" while Batter uses the port, and "applying takes effect at once", yet there is no confirmation, so an admin can cut their own connection.
  - Device delete: three patterns exist: native `confirm()` using only the serial (`device-card.tsx:99`), an inline "Are you sure?" in the edit modal (`device-edit-modal.tsx:163-189`), and a swallowed-error delete (`dashboard/page.tsx:105-112`). None says what delete means (registration only?).
- Fix: one shared confirm pattern (small dialog or inline two-step) that names the target and the consequence ("Stop session for Pixel 7? 2 other people are watching."). Require it for stop-all, revoke, role change, disable, VPN off, port apply. Show errors.

### 6. The input handler swallows the keyboard for the whole document
- Where: `lib/device-input.ts:172-189` (`e.preventDefault()` on every keydown), `211-224` (keyup), `246-247` (listeners on `document`, not the canvas); same design in `lib/device-input-multiplexer.ts`.
- What goes wrong: while the viewer is open, Tab, Enter, Space and arrow keys are cancelled for the entire page. A keyboard user cannot Tab to the toolbar, activate a focused button (Enter and Space activation are cancelled; keyup too), or use the Quality select with arrow keys. Typing goes to the phone even when focus is on a toolbar button, with no visible indication that the keyboard is captured. Text inputs only work because of React `stopPropagation` hacks (`device-viewer.tsx:404,439`; `multiplexer/page.tsx:232`).
- Fix: attach key listeners to the canvas (it already gets `tabIndex=0` and `focus()` at `device-viewer.tsx:89-90`) or ignore events whose target is not the canvas. Show a "Keyboard: phone" chip with a focus ring on the screen when captured, and release with Esc or click-away. Keep Tab free.

### 7. No responsive layout: fixed sidebar, nothing wraps (confirmed)
- Where: `components/layout/app-shell.tsx:17-18`, `components/layout/sidebar.tsx:42` (`w-56 h-screen`, no breakpoint); `app/dashboard/page.tsx:130`; viewer header `devices/[serial]/page.tsx:118`; see section 4.
- What goes wrong: at 390px the sidebar takes 224px and leaves 166px for content. Headers are `justify-between` rows with no wrap. The grid minimum (180px) is wider than the remaining space after `p-6`. The viewer header, toolbar, group card headers, user card headers and the wizard step bar all overflow. Even on desktop the sidebar permanently costs 224px of live-view width.
- Fix: collapsible sidebar (icon rail on desktop, off-canvas drawer with a top bar below `md`). Wrap or stack headers. Use `min-h-dvh`/`h-dvh` instead of `h-screen`. Details in section 4.

### 8. Loading, error and empty states destroy context or mislead
- Where: `dashboard/page.tsx:182-183` and `38-47`; `components/device/device-grid.tsx:15-25`; `app/devices/[serial]/page.tsx:175-189`; `multiview/page.tsx:31-40,86-87`; `multiplexer/page.tsx:31-40,185-187`.
- What goes wrong: (a) The dashboard polls every 5s, and one failed poll replaces the whole grid with a red sentence (`error ? ...`), unmounting every card and closing every thumbnail socket. A transient network blip makes the wall disappear. (b) The empty state says "No devices registered. Click 'Add Device' to register your first phone" even when a group filter matched nothing, and even for viewer-role users who have no Add button. (c) The viewer shows "Starting session..." with no timeout, a bare red error with no Retry or Back, and "No active session" with no Start button. (d) Multi View and the Multiplexer swallow API errors, so a failed load reads "No streaming devices. Start sessions from the Devices page." with no link to that page and no hint that the load failed. A connected phone without a session is invisible there. (e) Many lists swallow errors (see 12).
- Fix: keep the last good data and show a "Connection lost, retrying" banner. Give empty states a reason and an action ("No phones in group X", "No phones match 'ab'", "You have no devices; ask an admin"). Viewer error and no-session states get Retry, Start session and Back. Link empty states to Devices.

### 9. The UI ignores the user's role
- Where: `dashboard/page.tsx:211-212` (Edit and Delete always passed), `167-174` (Start All Sessions for everyone), `components/device/device-card.tsx:71`; `devices/[serial]/page.tsx:150` (Stop Session for everyone); `device-viewer.tsx` (all control actions, no role check); role copy in `admin/users/page.tsx:254-256` ("Viewer (watch only)").
- What goes wrong: a viewer-role user sees Delete in the card menu, Start All, Stop Session, Wake, Screen Off and typing tools, which can only fail. The failures are mostly invisible: delete errors are swallowed (`dashboard:109`), control shows "denied" in 12px text. The Control status is the only hint a viewer cannot control.
- Fix: hide or disable by role with an explanation ("Viewers can watch only"). Put a "Watch only" badge in the viewer header when control is denied.

### 10. Cards and headers are mouse-only; key controls appear only on hover
- Where: `device-card.tsx:66-69` (card is a `<div onClick>`, not focusable), `73` (kebab `opacity-0 group-hover:opacity-100`, no `focus-within`), `76-81` (no `aria-label`, 24px target), `82-109` (menu never closes on outside click or Esc); `multi-view-cell.tsx:63-67`; `groups/page.tsx:347-349`, `admin/users/page.tsx:277-279`, `admin/user-groups/page.tsx:296-298` (expand headers are clickable divs); hover-only remove/revoke at `groups/page.tsx:504,538,568`, `users/page.tsx:411`, `user-groups/page.tsx:469`.
- What goes wrong: a keyboard user cannot open a phone from the grid, cannot expand a group, user or team, and cannot reach Edit or Delete. A touch user never sees the kebab or the remove/revoke buttons because there is no hover. Icon-only buttons have no accessible name (XIcon buttons, close `&times;`, kebab, the 7 colour swatches at `groups/page.tsx:655-662`, which are also colour-only). Targets are 22-32px.
- Fix: make cards real links (`<a href>` or button) and expand headers real `<button aria-expanded>`. Reveal row actions with `group-focus-within`, or always show them at reduced emphasis (always on touch). Add `aria-label`s, 40-44px targets, and Esc/outside-click to close menus.

### 11. Modals are not real dialogs and break on short screens
- Where: `components/device/device-wizard.tsx:240-251,283`, `device-edit-modal.tsx:75-86`, `reconnect-modal.tsx:17-26`.
- What goes wrong: no `role="dialog"`, `aria-modal`, focus trap, initial focus, Esc to close or backdrop close; the `&times;` has no label. The panel has no `max-h`/`overflow-y-auto`, so the edit modal (with the Ethernet panel) and the 7-step wizard clip on laptop-height screens and the page behind still scrolls. The wizard's 7-step indicator (254-280, labels beside every number, no wrap) is wider than a 390px modal. Commit model is inconsistent: in the edit modal the Internet/VPN select applies instantly (`device-edit-modal.tsx:135`) while Nickname needs Save, and Cancel does not undo the VPN change. The wizard registers the phone on entering step 3 (`device-wizard.tsx:112-119`), so closing the wizard after that leaves a half-set-up phone; Groups and Access failures are explicitly ignored (186-188, 202-204); a failed registration still advances (172-175).
- Fix: shared `Modal` primitive (focus trap, Esc, labelled title, scroll body, sticky footer). In the wizard show the current step title only on small screens. Say which steps are saved so far. Apply the VPN change from an explicit button or make Save cover it. Report per-item failures.

### 12. Forms: no `<form>`, unlinked labels, rules only in placeholders, errors swallowed
- Where: `admin/users/page.tsx:232-263` (create user), `groups/page.tsx:305-327`, `user-groups/page.tsx:257-280`, `login/page.tsx:73,87,99` (labels without `htmlFor`); swallowed errors at `users/page.tsx:97,172,181`, `user-groups/page.tsx:110,228`, `groups/page.tsx:125-132,213-258` (no try/catch at all), `device-edit-modal.tsx:55-57`, `dashboard/page.tsx:109-111`, `login/page.tsx:25` (setup-check failure treated as "not first run").
- What goes wrong: Enter does not submit any form except login (all others are `div` + `onClick`). Create buttons are enabled with empty fields and fail server-side. "Min 8 characters" lives only in a placeholder that disappears when typing. Create group with a duplicate name or an API error: nothing happens, no message (unhandled rejection). Edit-device Save fails: the modal just stays open. First-run admin creation has no confirm-password and no show-password toggle, so a typo locks out the only admin. There is no way for a user to change their own password (only admin reset: `admin/users/page.tsx:447-463`). Login labels are not programmatically linked and the error is not `role="alert"`.
- Fix: real `<form onSubmit>`, `<label htmlFor>`, helper text under fields, inline validation plus a disabled submit until valid, `role="alert"` errors, a try/catch plus visible error on every handler, confirm-password and show-password on setup, a self-service "Change password" in the user menu.

### 13. Multi View and Multiplexer are confusing and the broadcast mode is risky
- Where: `components/layout/sidebar.tsx:12-13`, `multiview/page.tsx:62-72,89-97`, `multiplexer/page.tsx:123-176,188-209`, `components/device/multiplexer-grid.tsx:42-57,69-71`, `multi-view-cell.tsx:69`.
- What goes wrong: the names do not say what they do (watch many vs. type into many) and neither page has a one-line explainer. Multi View's 2x2/3x3/4x4 buttons only set the column count, so with 30 streaming phones every one still renders (rows are squeezed into `h-full` with `overflow-hidden`), and each cell runs a full-quality decoder rather than a thumbnail, so nothing signals the cell limit. "Controlling <serial>" (57-58) shows a serial rather than the name. In the Multiplexer the first selected phone is silently the "INPUT" tile (`multiplexer-grid.tsx:55,79`) with no way to choose; the other tiles show a `not-allowed` cursor (47) even though they do receive the broadcast, which reads as "this tile ignores input". The only broadcast warning is the header subtitle at 12px gray-500, so there is no persistent "typing goes to 12 phones" banner. The selector shows no status, so a phone that dropped between select and Start vanishes. Nav buttons and "Send All" broadcast with no confirmation.
- Fix: one "Wall" page with a mode switch Watch / Control all, an explainer line, a page-size cap (show N, paginate), a persistent red "Broadcasting to 12 phones" bar in control mode with a Stop button, a chosen/visible primary, mirrored tiles labelled "Mirroring", and a name instead of a serial.

### 14. Sidebar icons are emoji, navigation names are opaque, user menu is minimal
- Where: `components/layout/sidebar.tsx:9-21` (icons are the literal strings 📱 📁 🖥 🔗 👤 👥 🔒 🔌, rendered at 63 and 86), `46` (tagline `text-[10px] text-gray-500`), `72` (Admin label gray-600), `103-108` (Logout: 12px gray-500 text).
- What goes wrong: this is the cause of the tofu boxes in the headless browser: emoji need a system colour-emoji font, which the box lacks. On real machines they also render differently per OS, ignore `currentColor` (so the active/hover colour does nothing to them), and look like a different product to the rest of the UI; 🖥 lacks a variation selector so some platforms draw it as monochrome text. The icon `<span>` has no `aria-hidden`, so screen readers say "mobile phone Devices". Admin links pop in after hydration (`mounted && admin`, 69), causing a layout shift. There is no `aria-current` and no `aria-label` on `<nav>`. Nav items carry no state (no count of phones needing attention, no VPN-down marker). Logout is easy to miss and there is no account menu.
- Fix: inline SVG icons (`currentColor`, `aria-hidden`), as the app already does for nav bar and swipe pad (`nav-bar.tsx`, `swipe-pad.tsx`); `lucide-react` is already a dependency per project memory, so no new package. Add `aria-current="page"`, badges (Devices: needs-attention count), and an account menu (name, role, Change password, Log out).

### 15. Type size, contrast and focus visibility are below accessible minimums
- Where: everywhere (counts above); examples `device-card.tsx:167` (serial, 10px gray-500 on gray-900 = 3.67:1), `sidebar.tsx:72` (gray-600 = 2.35:1), placeholders `login/page.tsx:78,105`, `device-viewer.tsx:406,441`; `focus:outline-none` with only a border-colour change on inputs (e.g. `login/page.tsx:78`, `device-edit-modal.tsx:124`), nothing at all on buttons.
- What goes wrong: 101 uses of 10px text carry meaning (status, permissions, labels, helper text), 3.0 to 3.7:1 for gray-500 on the dark surfaces, 1.9 to 2.4:1 for gray-600. Disabled and enabled states differ only by `opacity-50`. Buttons have no visible keyboard focus ring. Permission, role and status badges rely on red/blue/green/gray tints (`groups/page.tsx:27-31`, `users/page.tsx:31-40`).
- Fix: 12px floor for anything informative, gray-400 minimum for text on gray-900/800 (gray-500 only for decoration), a global `:focus-visible` ring, text or icon alongside every colour state.

---

## 2. Proposed navigation and information architecture

Today: Devices, Groups, Multi View, Multiplexer, then Admin: Users, Teams, VPN, Phone network. Problems: Groups is a management page but is really a filter dimension for the grid; Multi View and Multiplexer are one workflow with two names; Admin mixes people with infrastructure; there is no way to move between phones; and the browser tab always reads "Batter - Remote Phone Management" (`app/layout.tsx:6`) whichever phone you are on.

Proposed:

- **Devices** (home, the fleet). Filter bar: search, status chips with counts, group select (always visible), card size. Bulk bar on selection: Open in Wall, Start sessions, Add to group. Groups appear here as chips and as a "Manage groups" side panel (admin keeps create/edit/access there). Remove the standalone Groups page, or keep it under Admin as "Groups (manage)".
- **Wall** (replaces Multi View + Multiplexer): mode switch *Watch* | *Control all*. Source: a selection, a group, or "all streaming". One persistent bar says what keyboard input will do.
- **Phone view** (`/devices/<serial>`): breadcrumb "Devices / Group / Nickname", title uses nickname first (today it uses model: `[serial]/page.tsx:127`, while the card uses nickname, so the same phone has two names). Prev/next phone and a quick switcher so an operator can step through a group without going back to the grid. Set `document.title` to the phone name.
- **Admin**, two sub-groups: *People* (Users, Teams) and *Infrastructure* (VPN, Phone network). Add an account menu at the bottom of the nav (name, role, change password, log out).
- Shell: collapsible icon-rail sidebar (collapsed by default on the phone view), top bar plus drawer under `md`, nav badges (Devices: count needing attention; VPN: tunnel down), `aria-current`, labelled `<nav>`, a skip link, one `<h1>` per page (pages use `h2` with the sidebar holding the only `h1`).

---

## 3. Proposed grouping of the viewer's actions

Keep the screen as the hero; move chrome to a slim bar and menus.

**Always visible (primary), icon + tooltip, in this order**
1. Status chip (Live / Connecting / Watch only / Needs USB), plus fps in a tooltip.
2. Screenshot.
3. Keyboard (toggle the type-to-phone bar).
4. Audio: mute toggle + volume (volume on hover or in the popover).
5. Nav row under the screen (Back, Home, Recents, All apps) stays; it is the good part of the current UI.

**Secondary (one popover each)**
- **Clipboard** (single control): "Send my clipboard to phone" and "Get phone clipboard". Replaces Paste, Copy and the separate panel; note Ctrl+V/Ctrl+C already work.
- **Screen** (single toggle): "Screen on/off" replaces Wake + Screen Off, with the current state if known.
- **Files**: Install APK / Send file, with a "drop files anywhere on the screen" hint (drag-and-drop exists at `device-viewer.tsx:182-189` but is undiscoverable).
- **View** (checkbox list, remembered per browser): Device frame, Nav buttons, Swipe pad, Quality (move the select here; show "Low/Medium/High" and Mbps as secondary text, and keep the "changed by another viewer" notice with it).

**Overflow / danger (visually separated, confirm required)**
- **End session for everyone** (today "Stop Session", `[serial]/page.tsx:150`): explain who is watching ("2 others watching"). Place it last in a "..." menu, never beside Quality.
- Edit device / Reconnect (opens the existing modals) belong here too; today they are reachable only from the grid card's hover-only kebab.

---

## 4. Mobile (390px) issues

Static read; widths are estimated from classes. Note first that the viewer only listens to mouse events (`lib/device-input.ts:241-245`: `mousedown/mousemove/mouseup/wheel/contextmenu`; no `touch*` or `pointer*` handlers), so on a real phone or tablet browser a drag or swipe on the remote screen will not behave as a swipe. Treat phone-width use as "monitor and tap", or add pointer events.

1. **Sidebar never collapses** (`app-shell.tsx:17-18`, `sidebar.tsx:42`): 224px of 390px, leaving 166px for content.
2. **Dashboard header overflows** (`dashboard/page.tsx:130-176`): title block plus group select, Refresh, Add Device and Start All Sessions in one non-wrapping row with `px-6`.
3. **Device grid** (`device-grid.tsx:29`): `minmax(180px,1fr)` plus `p-6` exceeds the 166px column, so the page scrolls horizontally. Cards lose their kebab (hover only).
4. **Viewer header overflows** (`[serial]/page.tsx:118-157`): Back + name + serial on the left, Quality + "Now x (changed by another viewer)" + Stop Session on the right, no wrap.
5. **Viewer toolbar** (`device-viewer.tsx:238-343`): 12 controls in a non-wrapping row; the status text plus buttons overflow. Targets are about 22px tall (`px-2 py-1 text-[10px]`), nav buttons 32px (`nav-bar.tsx:58`), under the 44px guideline.
6. **Swipe pad / nav bar over a 390px screen** (`swipe-pad.tsx`, `nav-bar.tsx`) overlay the little screen area left; the framed canvas (`device-viewer.tsx:362-370`, 12px bezel) shrinks it further.
7. **Wizard step bar** (`device-wizard.tsx:254-280`): 7 numbered steps each with a label and connector is roughly 490px; it spills out of a 390px modal. The modal body has no scroll (`283`).
8. **Group, user and team card headers** (`groups/page.tsx:347-385`, `users/page.tsx:277-303`, `user-groups/page.tsx:296-340`): name, badges, buttons, status, "Last login" and chevron on one line; Start All / Stop All sit beside the chevron and are easy to mis-tap.
9. **Grant-access rows** (`groups/page.tsx:580-628`): four controls plus a button on one non-wrapping line.
10. **Tables and mono text** (`phone-network/page.tsx:171-196`, VPN key/endpoint fields `vpn/page.tsx:243-275`): a 4-column table with MAC/IP and long mono strings, no `overflow-x-auto`.
11. **Multi View** (`multiview/page.tsx:11-15`): 3x3 and 4x4 are fixed column counts regardless of width; at 390px that is 90-120px tiles.
12. **Hover-only controls** (card kebab, remove/revoke icons) do not exist on touch.
13. **`h-screen`** (`app-shell.tsx:17`, `sidebar.tsx:42`): on mobile browsers the dynamic address bar makes 100vh taller than the visible area; use `dvh`.
14. No responsive prefixes anywhere except one `sm/md` use in `multiplexer/page.tsx:188`.

---

## 5. Quick wins (no visual redesign needed)

Each is a small change to existing code.

1. Wire the existing `controlStatus` into every control-dependent toolbar button (`disabled={controlStatus !== 'connected'}` plus title), as nav bar and swipe pad already do (`device-viewer.tsx:380,391`).
2. Fix the toast: clear the previous timer in `showToast` (`device-viewer.tsx:55-58`), 4s for info and sticky for errors; add a `catch` plus toast to `handleScreenshot` (102-117) and to Wake/Screen Off feedback.
3. Show nickname in the viewer title (`[serial]/page.tsx:127`: `device?.nickname || device?.model || serial`) so it matches the card.
4. Set `document.title` per page, and per phone in the viewer, in each page's effect (`app/layout.tsx:6` is the only title today); operators with several tabs cannot tell them apart.
5. Add `aria-label` to every icon-only button (kebab `device-card.tsx:76`, XIcon buttons, `&times;` closes, colour swatches) and `aria-pressed` to the five toolbar toggles (`device-viewer.tsx:264-305`).
6. Make the card kebab visible on focus: add `group-focus-within:opacity-100 focus-within:opacity-100` to `device-card.tsx:73`; close the menu on outside click or Esc.
7. Make the card, group, user and team headers `<button>`s or add `role="button" tabIndex={0}` with Enter/Space handlers (`device-card.tsx:66`, `groups/page.tsx:347`, `users/page.tsx:277`, `user-groups/page.tsx:296`).
8. Scope the key listeners to the canvas, or return early when `e.target` is not the canvas (`lib/device-input.ts:172`), so Tab and Enter work in the toolbar.
9. Fix the empty states: use `groupSerials !== null` to say "No phones in this group", hide the Add hint when `!canAddDevice` (`device-grid.tsx:15-25`, pass a prop).
10. Keep the grid on poll failure: only show the error block when `devices.length === 0`, otherwise a small "Connection lost, retrying" banner (`dashboard/page.tsx:182-183`).
11. Hide Edit/Delete, Start All and Stop Session for the viewer role (`dashboard/page.tsx:211-212`, `device-card.tsx:71`, `[serial]/page.tsx:150`) using the existing `isOperator()`/`isAdmin()`.
12. Add a confirm on Stop Session, Stop All, revoke, role change, disable user, VPN off and port Apply, reusing the inline "Are you sure?" pattern already in `users/page.tsx:469-495` and the count-aware text at `vpn/page.tsx:306`.
13. Show errors that are currently swallowed: `dashboard/page.tsx:109`, `device-edit-modal.tsx:55`, `users/page.tsx:172,181`, `user-groups/page.tsx:228`, `[serial]/page.tsx:110`; add try/catch to `groups/page.tsx:125-132,213-258`.
14. Add `Esc` and backdrop-click to close, `role="dialog" aria-modal="true" aria-labelledby`, and `max-h-[90vh] overflow-y-auto` to the three modals.
15. Wrap create/edit inputs in `<form onSubmit>` so Enter submits; add `htmlFor`/`id` pairs to labels (login first); add a confirm-password field to first-run setup (`login/page.tsx:98-109`).
16. Replace the emoji with inline SVGs and add `aria-hidden` (`sidebar.tsx:9-21`); add `aria-current="page"` to the active link (`sidebar.tsx:57,80`).
17. Raise informative text from 10px to 12px, and `text-gray-500/600` to `text-gray-400` on dark surfaces; add a global `:focus-visible { outline: 2px solid ... }` in `globals.css` (7 lines today).
18. Label the nav entries with a subtitle: "Multi View: watch many" and "Multiplexer: type into many", and link "No streaming devices" to `/dashboard`.
19. Disable "Start All Sessions" while it runs and show "Starting 3 of 12" (`dashboard/page.tsx:85-95` loops sequentially with no feedback, and a double-click starts it twice).
20. Cap Multi View at the chosen grid size (slice `streamingDevices` to 4/9/16 with a "+N more" note and paging) (`multiview/page.tsx:90`).
