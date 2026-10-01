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
- Optional, for HTTPS: a DNS name pointing at the box (e.g. `batter.example.com`).

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

Open `http://<box-ip>:3000` in a browser. The first visit shows **Create admin
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

## 6. HTTPS (recommended when reachable beyond your LAN)

Logins and the live stream go over plain HTTP otherwise. The simplest option is
[Caddy](https://caddyserver.com/docs/install) on the box, which gets and renews
a certificate automatically.

1. Create a `.env` file next to `docker-compose.yml`:

   ```env
   # Only accept connections from Caddy on this machine
   BATTER_PORT=127.0.0.1:3000
   # Let the login rate limit see real client IPs from Caddy
   TRUSTED_PROXIES=127.0.0.1,::1
   ```

2. `/etc/caddy/Caddyfile`:

   ```
   batter.example.com {
       reverse_proxy 127.0.0.1:3000
   }
   ```

3. Apply both:

   ```bash
   docker compose up -d
   sudo systemctl reload caddy
   ```

4. Open `https://batter.example.com`. Nothing else needs changing — WebSockets
   (live video and input) work through Caddy as-is.

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
| `BATTER_PORT` | `3000` | Different port, or `127.0.0.1:3000` behind a proxy. |
| `TRUSTED_PROXIES` | none | Behind Caddy/nginx: `127.0.0.1,::1`. |
| `LOG_LEVEL` | `info` | `debug` when troubleshooting. |
| `ALLOWED_ORIGINS` | same host only | Only if the UI is served from another domain. |

## 9. Troubleshooting

| Problem | Fix |
|---|---|
| **Scan for devices** finds nothing | Check the phone shows the USB debugging prompt and tap **Allow**. Make sure no `adb` runs on the host (`adb kill-server`). Re-plug the cable, wait a few seconds, scan again. |
| Phone shows as **unauthorized** | Unlock the phone and accept the **Allow USB debugging?** prompt. |
| Video stays on "connecting" | Check `docker compose logs batter` for scrcpy errors, and that the phone is still listed by **Scan for devices**. |
| Can log in but live view never connects (behind a proxy) | The proxy must pass WebSocket upgrades (Caddy does by default). For nginx, set `proxy_http_version 1.1` and the `Upgrade`/`Connection` headers. |
| Everyone gets "too many requests" on login | Behind a proxy without `TRUSTED_PROXIES`, all users share one limit. Set it as in step 6. |
| `batter` container isn't `healthy` | `docker compose logs batter` — a database or migration error is printed at startup. |
