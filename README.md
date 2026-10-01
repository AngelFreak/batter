# Batter

Remote Android phone management platform. View, control, and manage multiple Android devices from a web browser using low-latency H.264 video streaming over WebSocket.

![Dashboard](docs/screenshot-dashboard.png)

![Users](docs/screenshot-users.png)
![Groups](docs/screenshot-groups.png)

## Features

- **Live video streaming** - Real-time H.264 video from Android devices via scrcpy, decoded in-browser with WebCodecs API
- **Touch & keyboard control** - Full remote control with touch, scroll, and keyboard input forwarding
- **Clipboard sync** - Copy text between your browser and the device in both directions
- **File push & APK install** - Drag-and-drop files to the device's Download folder, or install APKs directly
- **Multi-view** - Watch a grid of devices at once and take control of any one of them
- **Multiplexer** - Broadcast the same touch/keyboard input to several devices simultaneously
- **Device grid** - Dashboard with live thumbnail previews for all connected devices
- **Adaptive quality** - Automatic thumbnail (360p/5fps) and full-quality (1024p/30fps) session tiers
- **Screenshot cache** - Cached last screenshot shown when devices are disconnected or sessions are idle
- **Device registration** - Guided wizard to register, validate, and probe device properties
- **Device groups** - Organize devices into groups with batch session start/stop, per-group access grants
- **Teams** - Group users into teams and grant team-level access to devices and device groups
- **User management** - Role-based access control (admin, operator, viewer) with expandable user cards, access overview, and password reset
- **RBAC** - Per-device, per-group, and per-team access permissions (view, control, manage) for non-admin users
- **Phone networking** - Wired (Ethernet) phones get their network from Batter, each phone's internet only through its own WireGuard VPN profile with a kill switch; USB for setup and control
- **Fully offline** - No external CDN, fonts, or scripts. The app works entirely without internet after deployment

## Deployment

```bash
git clone https://github.com/AngelFreak/batter.git
cd batter
docker compose up -d
```

Then open `https://<host>` and create the admin account. No `.env` is needed:
HTTPS is served by a built-in Caddy with its own local certificate authority
(trust `https://<host>/ca.crt` once per computer to remove the browser
warning), the login-token secret is generated on first start, the app accepts
requests from whatever host you open it on, and database migrations run
automatically.

**Full step-by-step guide** — phones, users, HTTPS, updates, backups and
troubleshooting: [docs/DEPLOY.md](docs/DEPLOY.md). Optional settings are listed
in [.env.example](.env.example).

## Hardware

Batter runs on one Linux machine ("the box") with the phones wired to it. There
are two ways to wire them. Phones stay in airplane mode with Wi-Fi off and no
SIM, so a phone's only network is its cable: on Ethernet, all of its internet
traffic goes through the WireGuard VPN profile assigned to it; on USB it has no
internet.

| | USB setup | Ethernet setup (recommended) |
|---|---|---|
| Phone connects with | A USB cable to the box | A USB-C ethernet adapter, to a switch on the box |
| Phone's internet | None — USB is for setup and as a control fallback | A wired LAN routed by the box, through its VPN profile |
| What apps see | n/a | A normal wired (Ethernet) network |
| Control and video | adb over USB | adb over the network |
| Charging | From the USB port or hub | A USB-C charger plugged into the adapter |
| Extra hardware per phone | A cable | An adapter, a charger and a patch cable |

### USB setup

Plug the phones into the box to set them up, and as a way to control them
without the network. Phones on USB have no internet.

- **Box:** any Linux machine with Docker and free USB ports.

| What | How many | Model (Proshop.dk) | Approx. price |
|---|---|---|---|
| Powered USB hub | 1 per 10 phones | [i-tec USB 3.0 Charging HUB, 10 ports, 48 W](https://www.proshop.dk/USB-hub/I-Tec-USB-30-Charging-HUB-10-port-Power-Adapter-48-W-USB-hub-10-ports-Graa/2851679) (up to 10 W per port) | 313 kr |
| USB-A to USB-C data cable, 1 m | 1 per phone | [Pro USB-A ↔ USB-C, 1 m, USB 3.2 Gen 1](https://www.proshop.dk/USB-kabel/Pro-USB-A-USB-C-1m-USB-32-Gen-1-Sort/2478379) | 89 kr |

Use real data cables; charge-only cables won't work.

### Ethernet setup

Each phone gets a real wired network through a USB-C ethernet adapter that
also charges it. The phone's single USB-C port is used by the adapter, so
Batter controls the phone over the network instead of over USB.

Which of the box's network ports the switch is on is chosen in Batter: right
after creating the admin account, or later under **Admin → Phone network**. No
configuration file is involved. While Batter uses that port, the box itself
has no connection on it; the box must not manage it (see
[docs/DEPLOY.md](docs/DEPLOY.md)).

```
Internet ── box (port 1)
            box (port 2) ── managed switch ─┬─ adapter ── phone   (+ charger)
                                            ├─ adapter ── phone   (+ charger)
                                            └─ ...
```

**Shopping list** (prices from Proshop.dk, October 2026; they change)

| What | How many | Model (Proshop.dk) | Approx. price |
|---|---|---|---|
| USB-C ethernet adapter **with USB-C power pass-through** | 1 per phone | [Sandberg USB-C Gigabit Network Adapter with PD (136-60)](https://www.proshop.dk/Netvaerksadapter-netkort-printserver-mv/Sandberg-USB-C-Gigabit-Network-Adapter-with-PD/3301397): Realtek RTL8153B, 100 W pass-through | 137 kr |
| ↳ alternative | | [AXAGON ADE-TXPD](https://www.proshop.dk/Netvaerksadapter-netkort-printserver-mv/AXAGON-ADE-TXPD-USB-C-Gigabit-Ethernet-Adapter-Power-Delivery-100W/3320423): ASIX AX88179A, 100 W pass-through | 224 kr |
| USB-C charger with USB-C cable | 1 per phone | [Samsung 25W USB-C GaN Power Adapter, incl. cable](https://www.proshop.dk/Mobil-Adaptere-Opladere/Samsung-25W-USB-C-GaN-Power-Adapter-Incl-cable-Black/3203727) | 249 kr |
| Patch cable, 1 m | 1 per phone, plus 1 | [Pro LAN CAT 6 UTP, 1 m](https://www.proshop.dk/Netvaerkskabel/Pro-LAN-CAT-6-UTP-Hvid-1m/2463062) | 49 kr |
| Managed switch with port isolation, up to 4 phones | 1 | [Ubiquiti UniFi Switch Flex Mini (USW-Flex-Mini)](https://www.proshop.dk/Switch/Ubiquiti-UniFi-Switch-USW-Flex-Mini/2835111): 5 ports, 1 to the box and 4 phones. Powered by PoE or a USB-C charger (5 V, 1 A) | 242 kr |
| ↳ up to 7 phones | | [Ubiquiti UniFi Switch Lite 8 PoE (USW-Lite-8-PoE)](https://www.proshop.dk/Switch/Ubiquiti-UniFi-Switch-Lite-USW-Lite-8-POE/2891302): 8 ports | 855 kr |
| Second network port on the box, if it has no free one | 1 | [TP-Link UE300](https://www.proshop.dk/Netvaerksadapter-netkort-printserver-mv/TP-Link-UE300-USB-30-to-Gigabit-Ethernet-Network-Adapter/2518088) (USB-A 3.0, Realtek). For a USB-C port: [TP-Link UE300C](https://www.proshop.dk/Netvaerksadapter-netkort-printserver-mv/TP-Link-UE300C-USB-Type-C-to-RJ45-Gigabit-Ethernet-Network-Adapter/2942465) | 81 kr |

**About 435 kr per phone** (adapter, charger, patch cable), plus one switch per 4 phones.

Why each part matters:
- **Power pass-through:** the adapter needs a USB-C charging port. Adapters without one drain the phone's battery.
- **Port isolation:** stops phones talking to each other directly. That traffic never passes through the box, so Batter can't filter it. See [docs/DEPLOY.md](docs/DEPLOY.md).
- **A separate port on the box:** keeps the phone network apart from the box's internet.

**Before buying for a fleet**, test one adapter on one phone of each model.

**Phone requirements**

The phone must support USB host mode (OTG) and have a driver for the adapter's
chip. Most current phones do. To check, with the phone connected over USB:

```bash
adb shell pm list features | grep usb.host              # must print feature:android.hardware.usb.host
adb shell cat /proc/modules | grep -E 'r8152|ax88179'   # Realtek / ASIX drivers
```

Tested: Samsung Galaxy A17 (SM-A175F), Android 16.

**Good to know**

- Each phone is connected over USB once to switch on network control. The
  Add Device wizard walks you through it (the **Ethernet** step).
- After a phone **reboots**, it needs that USB connection again: Batter marks
  it **Needs USB re-provision**; plug it in and click **Switch to ethernet** in
  Edit Device.
- The box must leave its phone network port alone: no NetworkManager, netplan
  or ifupdown configuration for it. See [docs/DEPLOY.md](docs/DEPLOY.md).
- The UniFi switch is set up once in the UniFi Network app before it goes on
  the box. See [docs/DEPLOY.md](docs/DEPLOY.md).

---

## Architecture

```
Browser (Next.js)  <--HTTP/WS-->  Go Backend  <--ADB/scrcpy-->  Android Devices
                                      |
                                  PostgreSQL
```

| Component | Technology |
|-----------|-----------|
| Backend | Go 1.24, Gin, gorilla/websocket, pgx/v5 |
| Frontend | Next.js 14 (App Router), React 18, TypeScript, Tailwind CSS |
| Database | PostgreSQL 16 |
| Streaming | scrcpy-server (H.264), WebCodecs VideoDecoder |
| Auth | JWT (access + refresh tokens), bcrypt |

## Project Structure

```
cmd/batter/              # Application entrypoint
internal/
  api/
    handlers/            # HTTP + WebSocket handlers (devices, groups, users, teams)
    middleware/           # Auth, CORS, RBAC, audit logging middleware
    router.go            # Route definitions
  auth/                  # JWT + password hashing
  config/                # Environment config
  device/                # ADB, scrcpy sessions, screenshot cache
db/migrations/           # PostgreSQL schema migrations (embedded, applied at startup)
web/                     # Next.js frontend
  src/
    app/
      dashboard/         # Device grid with live thumbnails
      devices/[serial]/  # Full-screen device viewer
      groups/            # Device groups management
      admin/users/       # User management (admin)
      admin/user-groups/ # Team management (admin)
    components/          # React components (device cards, wizard, layout)
    lib/                 # API client, auth, video players
scripts/                 # Utility scripts
```

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | Backend HTTP port |
| `DATABASE_URL` | `postgres://batter:batter@localhost:5432/batter` | PostgreSQL connection string |
| `JWT_SECRET` | *(generated)* | Secret for signing JWT tokens; if unset, generated once and stored in `DATA_DIR/jwt-secret` |
| `JWT_EXPIRY_SECS` | `3600` | Access token expiry in seconds |
| `SCRCPY_SERVER_PATH` | `/usr/local/share/scrcpy/scrcpy-server` | Path to scrcpy-server binary |
| `SCRCPY_VERSION` | `3.3.4` | scrcpy protocol version |
| `DATA_DIR` | `./data` | Directory for screenshot cache and runtime data |
| `ALLOWED_ORIGINS` | *(same origin)* | Comma-separated browser origins; empty allows only the host the app is opened on |
| `TRUSTED_PROXIES` | *(none)* | Proxy IPs/CIDRs whose `X-Forwarded-For` is believed |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error` |
| `PHONE_LAN` | `10.77.0.1/24` | Batter's address on the phone network (its port is chosen in the UI) |
| `PHONE_LAN_POOL` | `10.77.0.100-10.77.0.250` | DHCP pool for phones |
| `VPN_EXIT_IP_URL` | `https://api.ipify.org` | Answers with the caller's IP; used to check a VPN profile's exit IP |

## API Reference

All endpoints are prefixed with `/api/v1` and require JWT authentication (except auth routes).

<details>
<summary>Auth</summary>

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/auth/login` | Login, returns access + refresh tokens |
| POST | `/auth/refresh` | Refresh access token |
| GET | `/auth/me` | Get current user info |

</details>

<details>
<summary>Devices</summary>

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/devices` | List registered devices with live status |
| POST | `/devices` | Register a new device |
| GET | `/devices/:serial` | Get single device info |
| PUT | `/devices/:serial` | Update device nickname/properties |
| DELETE | `/devices/:serial` | Delete device |
| POST | `/devices/:serial/session/start` | Start scrcpy session |
| POST | `/devices/:serial/session/stop` | Stop session |
| POST | `/devices/:serial/session/upgrade` | Switch to full quality |
| POST | `/devices/:serial/session/downgrade` | Switch to thumbnail quality |
| GET | `/devices/:serial/screenshot` | Get device screenshot (live or cached) |

</details>

<details>
<summary>Device Groups</summary>

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/groups` | List device groups |
| POST | `/groups` | Create device group |
| PUT | `/groups/:id` | Update group name/description/color |
| DELETE | `/groups/:id` | Delete device group |
| GET | `/groups/:id/devices` | List devices in group |
| POST | `/groups/:id/devices` | Add devices to group |
| DELETE | `/groups/:id/devices/:serial` | Remove device from group |
| POST | `/groups/:id/batch/start` | Batch start sessions |
| POST | `/groups/:id/batch/stop` | Batch stop sessions |
| GET | `/groups/:id/access` | List user access grants for group |
| DELETE | `/groups/:id/access/:accessId` | Revoke user access grant |
| GET | `/groups/:id/team-access` | List team access grants for group |
| POST | `/groups/:id/team-access` | Grant team access to group |
| DELETE | `/groups/:id/team-access/:accessId` | Revoke team access grant |

</details>

<details>
<summary>Users (admin only)</summary>

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/users` | List users |
| POST | `/users` | Create user |
| PUT | `/users/:id` | Update user role/status |
| DELETE | `/users/:id` | Delete user |
| GET | `/users/:id/devices` | List user's access grants |
| POST | `/users/:id/devices` | Grant device/group access to user |
| DELETE | `/users/:id/devices/:accessId` | Revoke user access |
| PUT | `/users/:id/password` | Reset user password |

</details>

<details>
<summary>Teams (admin only)</summary>

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/user-groups` | List teams |
| POST | `/user-groups` | Create team |
| PUT | `/user-groups/:id` | Update team |
| DELETE | `/user-groups/:id` | Delete team |
| GET | `/user-groups/:id/members` | List team members |
| POST | `/user-groups/:id/members` | Add member to team |
| DELETE | `/user-groups/:id/members/:userId` | Remove member from team |
| GET | `/user-groups/:id/access` | List team's device access grants |
| POST | `/user-groups/:id/access` | Grant device access to team |
| DELETE | `/user-groups/:id/access/:accessId` | Revoke team device access |

</details>

<details>
<summary>WebSocket</summary>

| Endpoint | Description |
|----------|-------------|
| `/ws/device/:serial/video` | H.264 video stream (binary frames) |
| `/ws/device/:serial/control` | Touch/keyboard input (JSON messages) |

</details>

## Development

For local development without Docker (requires Go 1.24+, Node.js 20+, ADB):

```bash
# Start only the database
make db-up

# Configure environment
cp .env.example .env
# Edit .env and set JWT_SECRET

# Start the Go backend
make dev

# In another terminal, start the Next.js frontend
cd web && npm install && npm run dev
```

Open **http://localhost:3000**.

```bash
# Other useful commands
make test      # Run tests
make build     # Build Go binary
make lint      # Run linter
make db-reset  # Reset database (deletes all data)
```

## License

Proprietary - XpertaDK
