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
- **Fully offline** - No external CDN, fonts, or scripts. The app works entirely without internet after deployment

## Deployment

### Requirements

- [Docker](https://docs.docker.com/get-docker/) and [Docker Compose](https://docs.docker.com/compose/install/)
- A Linux host with USB ports for connecting Android devices
- One or more Android devices with **USB debugging** enabled

### Step 1: Clone and configure

```bash
git clone https://github.com/XpertaDK/batter.git
cd batter
cp .env.example .env
```

Edit `.env` and set the required `JWT_SECRET`:

```bash
# Generate a secure secret
openssl rand -hex 32
```

Paste the result into `.env`:

```env
JWT_SECRET=your-generated-secret-here
POSTGRES_PASSWORD=a-strong-database-password
```

### Step 2: Connect your Android devices

Plug in your Android device(s) via USB. Make sure USB debugging is enabled:

1. On the device, go to **Settings > About phone** and tap **Build number** 7 times
2. Go to **Settings > Developer options** and enable **USB debugging**
3. When prompted on the device, tap **Allow** to authorize the computer

Verify the host sees them:

```bash
adb devices
```

### Step 3: Start Batter

```bash
docker compose up -d
```

First start takes a few minutes to build. After that, starts are instant.

| Container | What it does |
|-----------|-------------|
| **postgres** | PostgreSQL 16 database, data persisted in a Docker volume |
| **batter** | Go backend + Next.js frontend + scrcpy-server, all in one container |

Database migrations are applied automatically on first start.

### Step 4: Open the browser

Go to **http://localhost:3000**

On first launch you'll see a setup screen to create your **admin account**. After that:

1. Click **Add Device** to register your connected Android devices
2. Click a device card to start a live remote session
3. Create **groups**, **users**, and **teams** from the sidebar

### Configuration

All configuration is done via the `.env` file. You never need to edit `docker-compose.yml`.

| Variable | Default | Description |
|----------|---------|-------------|
| `JWT_SECRET` | *(required)* | Secret for signing auth tokens. Generate with `openssl rand -hex 32` |
| `POSTGRES_PASSWORD` | `batter` | Database password. Change this in production |
| `POSTGRES_USER` | `batter` | Database username |
| `POSTGRES_DB` | `batter` | Database name |
| `BATTER_PORT` | `3000` | Port the app is accessible on |
| `ALLOWED_ORIGINS` | `http://localhost:3000` | CORS origins (change if accessing from another host) |
| `FRONTEND_URL` | `http://localhost:3000` | Frontend URL (change if accessing from another host) |

**Accessing from another machine on the network:**

```env
BATTER_PORT=3000
ALLOWED_ORIGINS=http://192.168.1.100:3000
FRONTEND_URL=http://192.168.1.100:3000
```

**Changing the port:**

```env
BATTER_PORT=9000
ALLOWED_ORIGINS=http://localhost:9000
FRONTEND_URL=http://localhost:9000
```

### Managing the deployment

```bash
# View logs
docker compose logs -f

# Stop everything (data is preserved)
docker compose down

# Start again
docker compose up -d

# Update to latest version
git pull
docker compose up -d --build

# Full reset (deletes all data)
docker compose down -v
```

### Backups

Database data is stored in the `pgdata` Docker volume. To back it up:

```bash
# Dump the database
docker compose exec postgres pg_dump -U batter batter > backup.sql

# Restore from backup
docker compose exec -T postgres psql -U batter batter < backup.sql
```

Screenshot cache is stored in the `batter_data` volume and is recreated automatically — no need to back it up.

### Putting behind a reverse proxy (HTTPS)

For production, put Batter behind a reverse proxy like Nginx or Caddy for HTTPS. Example with Caddy:

```
batter.example.com {
    reverse_proxy localhost:3000
}
```

Then update `.env`:

```env
ALLOWED_ORIGINS=https://batter.example.com
FRONTEND_URL=https://batter.example.com
```

### Troubleshooting

| Problem | Solution |
|---------|----------|
| `JWT_SECRET is required` on start | You forgot to set `JWT_SECRET` in `.env`. Run `openssl rand -hex 32` and add it. |
| `no devices found` after adding a device | Make sure USB debugging is enabled and the device authorized. Run `adb devices` on the host to verify. |
| Container can't see USB devices | The container needs `privileged: true` and `/dev/bus/usb` mounted (both set in `docker-compose.yml`). Reconnect the USB cable and retry. |
| Database errors on first startup | Wait 10 seconds — postgres needs to initialize. The healthcheck handles this automatically. |
| App works on localhost but not from other machines | Set `ALLOWED_ORIGINS` and `FRONTEND_URL` in `.env` to `http://YOUR_IP:3000`. |

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
db/migrations/           # PostgreSQL schema migrations (auto-applied)
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
| `JWT_SECRET` | *(required)* | Secret for signing JWT tokens |
| `JWT_EXPIRY_SECS` | `3600` | Access token expiry in seconds |
| `SCRCPY_SERVER_PATH` | `/usr/local/share/scrcpy/scrcpy-server` | Path to scrcpy-server binary |
| `SCRCPY_VERSION` | `3.3.4` | scrcpy protocol version |
| `DATA_DIR` | `./data` | Directory for screenshot cache and runtime data |
| `ALLOWED_ORIGINS` | *(empty)* | Comma-separated CORS origins |
| `FRONTEND_URL` | `http://localhost:3000` | Frontend URL for CORS |

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
