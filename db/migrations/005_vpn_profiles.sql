-- VPN profiles: tethered phones always reach the internet through one. Each
-- profile gets a slot (0-31) from which its relay's uid and port, WireGuard
-- interface and routing table are derived. config is the wg-quick text,
-- private key included; the API never returns it.

CREATE TABLE IF NOT EXISTS vpn_profiles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL UNIQUE,
    slot SMALLINT NOT NULL UNIQUE CHECK (slot BETWEEN 0 AND 31),
    config TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE devices ADD COLUMN IF NOT EXISTS vpn_profile_id UUID REFERENCES vpn_profiles(id) ON DELETE SET NULL;

-- Phones tethered under the single-tunnel version (reverse_tether). At
-- startup Batter imports that version's DATA_DIR/wireguard.json as the
-- "Default" profile and assigns these phones to it; with nothing to import
-- they are left off, since tethering no longer has a direct option.
CREATE TABLE IF NOT EXISTS legacy_tethered_devices (
    serial TEXT PRIMARY KEY
);

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_name = 'devices' AND column_name = 'reverse_tether') THEN
        INSERT INTO legacy_tethered_devices (serial)
            SELECT serial FROM devices WHERE reverse_tether
            ON CONFLICT DO NOTHING;
        ALTER TABLE devices DROP COLUMN reverse_tether;
    END IF;
END $$;
