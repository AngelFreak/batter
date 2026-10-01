-- The box NIC chosen as the phone network's port (at most one row; none =
-- the phone network is off). It is matched by MAC, since USB NICs can be
-- re-plugged under another name; name is what the box called it, so Batter
-- can give it back under that name.

CREATE TABLE IF NOT EXISTS phone_lan_port (
    id BOOLEAN PRIMARY KEY DEFAULT true CHECK (id),
    mac TEXT NOT NULL CHECK (mac ~ '^([0-9a-f]{2}:){5}[0-9a-f]{2}$'),
    name TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
