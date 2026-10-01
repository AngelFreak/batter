-- Per-device reverse tethering (gnirehtet): the device uses this host's
-- internet over adb. This is the desired state; it is (re)applied whenever
-- the device is connected.

ALTER TABLE devices ADD COLUMN IF NOT EXISTS reverse_tether BOOLEAN NOT NULL DEFAULT false;
