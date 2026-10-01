# Deploying Batter

A step-by-step guide for putting Batter on a server ("the box") with Android
phones wired to it: over USB, or on a wired phone network (section 7). No
configuration file is needed for a standard install.

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

## 7. Phone network (ethernet)

Phones can get internet only over a wired network that Batter runs. Each
phone gets a USB-C ethernet adapter; the adapters go into a switch plugged
into a **second network port** of the box (NIC2). Batter is that network's
DHCP server and router: Android sees a real ethernet connection (so apps work
in airplane mode too), and Batter lets each phone out only through the
WireGuard tunnel of the VPN profile chosen for it under **Edit Device →
Internet**. Phones on USB are for setup and control only: they get no
internet. The hardware and a shopping list are in the
[README](../README.md#hardware).

What each phone on the network can reach:

| Phone has | It reaches |
|---|---|
| No VPN profile | Nothing (only DHCP from Batter). |
| A profile | The internet through that profile's tunnel, DNS included (queries go to the profile's `DNS =` server). |
| A profile whose tunnel is down or disabled | Nothing: there is no direct fallback. |

Never reachable from a phone: Batter's web app and API, the database, the
box itself (on any address or protocol: while Batter uses NIC2, the box has no
interface on the phone network at all), the box's own LAN (NIC1) or the
internet directly. IPv6 is not offered or forwarded. Batter's own connections
to the phones (adb) work. Other devices on the switch, such as the switch's
own management interface, get an address but nothing else, and are listed as
"not a phone" under **Admin → Phone network**.

### How it works, and the privileges it needs

When an admin picks NIC2, Batter moves it from the box into the batter
container's own network namespace (renamed `phonelan`). The firewall for it
is installed first, and the container's IP forwarding is only turned on once
the port is up. Turning the phone network off, or picking another port, moves
the NIC back to the box **down**, under its own name, and removes every
firewall table, rule and forwarding setting Batter added. Batter never adds
anything to the box's own networking.

To see and move the box's NICs, the batter container runs with `pid: host`
(in `docker-compose.yml`), in addition to `privileged: true`, which it already
needed for USB and WireGuard. With it, Batter enters the box's network
namespace through `/proc/1/ns/net` (with `nsenter`) to list the NICs and move
the chosen one; it runs nothing else there. `pid: host` also lets the
container see (and, being privileged, signal) the box's processes. The Docker
socket is not mounted.

If the batter container stops, a graceful stop gives the NIC back to the box
down. If the container dies abruptly, the kernel returns the NIC to the box
when the container's namespace goes away, closing it on the way, so it
should arrive down (under the name `phonelan`; check it once, as below). Either way Batter takes it again,
matched by its MAC address, when it starts. A USB NIC that is unplugged and
plugged back in is taken again automatically; while it is missing, the phone
network is off and **Admin → Phone network** says so.

### Keep the box's hands off NIC2

While Batter is stopped, NIC2 is back on the box. If something on the box
then brings it up (NetworkManager, netplan, ifupdown), the box gets an
address on the phones' switch (at least an IPv6 link-local one), and phones
can reach the box directly: its SSH, Batter's HTTPS, anything listening.
So the box must not manage NIC2 at all. **Admin → Phone network** warns if
Batter finds NIC2 up on the box, or sees the box bring it up after giving it
back.

Find NIC2's name and MAC with `ip link`. Then:

- **NetworkManager** (most desktops, many servers):

  ```bash
  printf '[keyfile]\nunmanaged-devices=mac:aa:bb:cc:dd:ee:ff\n' | sudo tee /etc/NetworkManager/conf.d/90-batter-phones.conf
  sudo systemctl reload NetworkManager
  nmcli device status   # NIC2 shows "unmanaged"
  ```

  Use the MAC, not the name: Batter gives the NIC back as `phonelan` after a
  crash. Also delete any connection profile for it (`nmcli connection show`,
  `sudo nmcli connection delete <name>`).

- **netplan** (Ubuntu Server): make sure no file in `/etc/netplan/` mentions
  NIC2, and that no `match:` block (such as `match: {name: "en*"}` with
  `dhcp4: true`) catches it. Then `sudo netplan apply`.

- **ifupdown** (`/etc/network/interfaces`): remove any `auto`/`allow-hotplug`
  and `iface` lines for NIC2.

Check with the box running Batter and NIC2 picked: `ip link` on the box no
longer lists NIC2 (it is inside the container). With Batter stopped
(`docker compose stop batter`), `ip link show <NIC2>` (or `phonelan`) shows
it `DOWN` and `ip addr show <NIC2>` shows no addresses.

### Turn it on

1. Plug the switch into NIC2 (see [the phone switch](#the-phone-switch-use-a-managed-switch-with-port-isolation)
   below) and make sure the box doesn't manage NIC2 (above).
2. In Batter, open **Admin → Phone network** (also offered right after the
   admin account is created). Pick NIC2 from the list and click **Apply**.
   The list shows only the box's physical wired ports; the one carrying the
   box's own internet, and any with addresses or routes on the box, are shown
   as unusable with the reason.
3. Give each VPN profile a `DNS =` line (**Admin → VPN**): the phones' DNS
   queries go to that server, through the tunnel.

The phone network is `10.77.0.0/24`: Batter is `10.77.0.1`, phones get
`10.77.0.100`-`10.77.0.250`, and each adapter keeps its address for good. If
that subnet is used elsewhere on your networks, set `PHONE_LAN` and
`PHONE_LAN_POOL` in `.env` (see [`.env.example`](../.env.example)).

### Move a phone to ethernet

1. Add the phone as usual over USB (**Add Device**), or open an existing
   one's **Edit Device**.
2. At the **Ethernet** step (or in Edit Device) click **Switch to
   ethernet** while the phone is still on USB. This turns on adb over the
   network (port 5555) and removes the old reverse-tethering app.
3. Unplug USB, plug in the adapter (with its charger). Within a few seconds
   Batter finds the phone on the network and shows it as **Ethernet ·
   10.77.0.x**. Everything (live view, audio, files, APKs, screen lock) works
   as on USB.
4. Choose its VPN profile under **Edit Device → Internet**.

**After a phone restarts**, Android turns adb over the network off again.
The phone keeps its network and VPN, but Batter can't control it and shows
**Needs USB re-provision**: plug it into USB, click **Switch to ethernet**
in Edit Device, and put the adapter back. A phone on USB is always
controllable over USB.

### The phone switch: use a managed switch with port isolation

Phones plugged into the same switch can reach each other directly. That
traffic never passes through the box, so Batter's VPN and firewall can't stop
it. A phone could, for example, open connections to another phone's apps. To
close this, use a managed switch with **port isolation**: isolated ports can
only talk to the uplink port (the box), never to each other.

An unmanaged switch works, but leaves phone-to-phone traffic open.

**What to buy.** A small Ubiquiti UniFi switch is a good fit:

- **UniFi Switch Flex Mini** (USW-Flex-Mini): 5 gigabit ports, so 1 to the box
  and 4 phones. Powered by USB-C or PoE.
- For more phones (up to 7), the **UniFi Switch Lite 8 PoE** (USW-Lite-8-PoE):
  8 ports. Any bigger UniFi switch works too; check that its tech specs list
  port isolation.

Any other brand works if it has "port isolation" or "protected ports".

**UniFi switches need the UniFi Network app.** They have no web page of their
own; you set them up from the UniFi Network app (a UniFi gateway, a Cloud Key,
or the free UniFi Network Server software on a laptop). Settings are stored on
the switch, so it keeps isolating after the app is gone.

1. Plug the switch into a normal network where the UniFi Network app can see
   it, and **adopt** it.
2. Go to **UniFi Devices** → the switch → **Ports**.
3. For each port a phone will use: select the port, and under
   **Profile Overrides** turn on **Port Isolation**. Apply.
4. Leave the port that goes to the box **not isolated**, or nothing reaches
   the box.
5. Move the switch to the box: the uplink port to the box's phone network
   port, phones on the isolated ports.

The app will show the switch as offline from then on. That's expected;
isolation keeps working. If you ever need to change it, plug the switch back
into the app's network, or factory-reset it (hold the reset button about 10 s)
and start over.

The switch asks Batter's phone network for an address like any other device.
It gets one, but no internet, the same as a phone without a VPN profile.

**Check it works** (needs two phones on the switch, both connected in Batter).
From the box, ping phone B from phone A:

```bash
docker compose exec batter adb -s <phoneA-ip>:5555 shell ping -c 2 -W 2 <phoneB-ip>
```

With isolation on, this gets no replies (`100% packet loss`). If phone B
answers, isolation isn't on for those ports.

### What Batter can't enforce

Batter controls everything that passes through it, but phones on the same
switch can also talk to **each other directly**, without passing Batter.
Only the switch can stop that (port isolation, above). Without it, a phone
could reach another phone's open ports, or pose as another phone's adapter
(same MAC and address) to use that phone's VPN profile.

Batter does stop: a device on the switch borrowing another phone's address
(each phone's traffic must come from its own adapter's MAC), devices that
aren't a phone with a profile getting anywhere, and any phone reaching
Batter, the box or anything else except through its tunnel. It can't stop
the box itself from bringing NIC2 up while Batter is stopped; that's the
"keep the box's hands off NIC2" step.

## 8. Day-to-day

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

## 9. Optional settings

Everything has a working default; see [`.env.example`](../.env.example) for the
full list. The ones you might touch:

| Setting | Default | When to change it |
|---|---|---|
| `BATTER_PORT` | `127.0.0.1:3000` | Only for direct plain-HTTP access to the app port. |
| `BATTER_API_PORT` | `8080` | If port 8080 is already taken on the box (it is bound on 127.0.0.1 only). |
| `LOG_LEVEL` | `info` | `debug` when troubleshooting. |
| `ALLOWED_ORIGINS` | same host only | Only if the UI is served from another domain. |

### Remote viewers on slow links

Live video is latency-first: a viewer whose connection can't keep up skips
ahead to the newest picture instead of falling behind. For that to work over
slow or relayed links (e.g. NetBird via a relay), also stop the box's kernel
from queueing seconds of video per connection. Run once on the box (it
persists across reboots):

```bash
echo "net.ipv4.tcp_notsent_lowat = 131072" | sudo tee /etc/sysctl.d/90-batter-latency.conf && sudo sysctl -p /etc/sysctl.d/90-batter-latency.conf
```

## 10. Troubleshooting

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
| Phone on ethernet gets no address | Check the adapter's link light, and that the switch is on the port picked under **Admin → Phone network** and that page says it's on. `docker compose logs batter \| grep lan` shows DHCP and firewall errors; if the firewall can't be installed, Batter keeps the port down rather than run it open. |
| **Admin → Phone network** says unavailable | The batter container needs `pid: host` (it's in the shipped `docker-compose.yml`); check you haven't overridden it. |
| **Admin → Phone network** warns the box manages the port | Something on the box (NetworkManager, netplan, ifupdown) brings NIC2 up. Set it unmanaged as in "Keep the box's hands off NIC2". |
| Phone shows **Needs USB re-provision** | It restarted. Plug it into USB and click **Switch to ethernet** in Edit Device. |
| Phone on ethernet has no internet | It needs a VPN profile (Edit Device → Internet), and that profile's tunnel must be up (Admin → VPN, **Check exit IP**). |
