-- Phones on the wired LAN: each ethernet adapter (by MAC) keeps the IP
-- Batter's DHCP server gave it for good, and serial is the phone behind it
-- once Batter has identified it over adb. A phone's internet (VPN profile)
-- follows its serial, so it's routed by the lease bound to that serial.

CREATE TABLE IF NOT EXISTS lan_leases (
    mac TEXT PRIMARY KEY CHECK (mac ~ '^([0-9a-f]{2}:){5}[0-9a-f]{2}$'),
    ip INET NOT NULL UNIQUE,
    serial TEXT UNIQUE,
    hostname TEXT NOT NULL DEFAULT '',
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
