# Deploying Batter

A step-by-step guide for putting Batter on a server ("the box") that has
Android phones plugged into it over USB. No configuration file is needed for a
standard install.

## 1. What you need

- A Linux machine with free USB ports (or a powered USB hub) for the phones.
- [Docker](https://docs.docker.com/engine/install/) with the Compose plugin
  (`docker compose version` should print a version).
- Android phones with **USB debugging** enabled:
  1. **Settings → About phone** → tap **Build number** 7 times.
  2. **Settings → System → Developer options** → turn on **USB debugging**.
- Ports 80 and 443 free on the box (Batter serves HTTPS itself).

**Don't run `adb` on the box itself.** Batter runs its own `adb` inside the
container, and only one `adb` can hold a USB phone at a time. If `adb` is
installed on the host, run `adb kill-server` and don't start it again.

## 2. Install

```bash
git clone https://github.com/AngelFreak/batter.git
cd batter
docker compose up -d
```

The first build takes a few minutes. When it's done, check both containers are
healthy:

```bash
docker compose ps
```

You should see `postgres` and `batter` with status `healthy` (allow ~30 s).

## 3. Create the admin account — do this straight away

Open `https://<box-ip>` in a browser (LAN IP, NetBird IP or name — any works).
The browser warns about the certificate the first time; see
[step 6](#6-https-certificate) to make that go away, or click through for now
(Chrome: **Advanced → Proceed**). The first visit shows **Create admin
account**. Pick a username and a strong password (8+ characters) and submit;
you're logged in.

Until this is done, *anyone* who can reach the page can claim the admin
account, so do it before exposing the box to a wider network.

## 4. Add phones

1. Plug a phone in. On the phone, tap **Allow** on the **Allow USB debugging?**
   prompt and tick **Always allow from this computer**. (This is remembered
   across Batter updates.)
2. In Batter, click **Add Device** → **Scan for devices**. If the phone isn't
   listed yet, wait a few seconds and scan again — it can take a moment after
   plugging in.
3. Select it and follow the wizard (validate → details → groups → access).
4. Click the phone's card to open the live view.

## 5. Users and access

Create users under **Users** (admin only). Roles:

| Role | Can do |
|---|---|
| **admin** | Everything, on every device. Manages users, teams and groups. |
| **operator** | Register new devices. Device access as granted. |
| **viewer** | Only devices they've been granted. |

Per-device (or per-group / per-team) grants decide what a non-admin can do:

| Grant | Allows |
|---|---|
| **view** | Watch the screen. |
| **control** | Also touch/type, wake, stop the session, push files, install APKs. |
| **manage** | Also rename or delete the device in Batter. |

## 6. HTTPS certificate

Batter always serves HTTPS (browsers only allow live video on secure pages). A
built-in Caddy issues certificates from its own local certificate authority for
whatever address you open the box by, so there's nothing to configure — but
browsers don't know that authority yet, hence the warning.

Trust it once per computer to get rid of the warning:

1. Download `https://<box-ip>/ca.crt` (click through the warning this once).
2. Install it as a trusted root:
   - **macOS:** double-click it → Keychain Access opens → double-click the
     "Caddy Local Authority" certificate → **Trust → Always Trust**.
   - **Windows:** double-click it → **Install Certificate → Local Machine →
     Place all certificates in: Trusted Root Certification Authorities**.
   - **Linux (Chrome/Chromium):** Settings → Privacy and security → Security →
     Manage certificates → Authorities → **Import**, tick "Trust this
     certificate for identifying websites".
   - **Linux (system-wide, e.g. curl):** `sudo cp ca.crt /usr/local/share/ca-certificates/batter.crt && sudo update-ca-certificates`
3. Restart the browser.

The authority's key stays on the box (in the `caddy_data` volume) and is kept
across updates, so this is a one-time step per computer.

Ports 3000 (the web app) and 8080 (the API) are plain HTTP and only reachable
from the box itself; everything else goes through HTTPS on port 443 (port 80
redirects to it). Caddy sends API and live-video requests straight to port
8080 and the rest to 3000.

## 7. Day-to-day

```bash
docker compose logs -f batter     # follow logs
docker compose restart batter     # restart the app (data is kept)
docker compose down               # stop everything (data is kept)
docker compose up -d              # start again
```

### Updating

```bash
git pull
docker compose up -d --build
```

Database changes are applied automatically when the new version starts. Users
stay logged in; open live views reconnect by themselves within a few seconds.

### Backups

The database is the only thing that needs backing up:

```bash
docker compose exec postgres pg_dump -U batter batter > batter-$(date +%F).sql
```

The dump contains the VPN profiles' WireGuard private keys (anyone holding
it can use those VPN accounts), so store it as you would a password: not
world-readable, not in a shared folder, and encrypted if it leaves the
server.

Restore into a fresh install:

```bash
docker compose up -d postgres
docker compose exec -T postgres psql -U batter batter < batter-YYYY-MM-DD.sql
docker compose up -d
```

The other volumes rebuild themselves if lost: `batter_data` (screenshot cache
and the login-token secret — losing it just logs everyone out) and `adb_keys`
(phones ask to **Allow USB debugging** once more).

## 8. Optional settings

Everything has a working default; see [`.env.example`](../.env.example) for the
full list. The ones you might touch:

| Setting | Default | When to change it |
|---|---|---|
| `BATTER_PORT` | `127.0.0.1:3000` | Only for direct plain-HTTP access to the app port. |
| `BATTER_API_PORT` | `8080` | If port 8080 is already taken on the box (it is bound on 127.0.0.1 only). |
| `LOG_LEVEL` | `info` | `debug` when troubleshooting. |
| `ALLOWED_ORIGINS` | same host only | Only if the UI is served from another domain. |

## 9. Troubleshooting

| Problem | Fix |
|---|---|
| **Scan for devices** finds nothing | Check the phone shows the USB debugging prompt and tap **Allow**. Make sure no `adb` runs on the host (`adb kill-server`). Re-plug the cable, wait a few seconds, scan again. |
| Phone shows as **unauthorized** | Unlock the phone and accept the **Allow USB debugging?** prompt. |
| Video stays on "connecting" | Check `docker compose logs batter` for scrcpy errors, and that the phone is still listed by **Scan for devices**. |
| Live view says **needs-https** | You opened `http://…:3000`; use `https://<box-ip>` instead. |
| Certificate warning every time | Trust `https://<box-ip>/ca.crt` as in step 6, then restart the browser. |
| HTTPS doesn't load | Ports 80 and 443 must be free on the box: `sudo ss -ltnp 'sport = :443'`. Check `docker compose logs caddy`. |
| Pages load but nothing works (API errors 502) | Port 8080 on the box may be taken by something else: `sudo ss -ltnp 'sport = :8080'`. Set `BATTER_API_PORT` to a free port in `.env` and `docker compose up -d`. |
| `batter` container isn't `healthy` | `docker compose logs batter` — a database or migration error is printed at startup. |
