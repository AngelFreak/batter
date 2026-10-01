-- User groups (teams) for batch device access assignment

CREATE TABLE IF NOT EXISTS user_groups (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL UNIQUE,
    description TEXT,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS user_group_members (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    group_id UUID NOT NULL REFERENCES user_groups(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, group_id)
);

CREATE INDEX IF NOT EXISTS idx_user_group_members_group_id ON user_group_members(group_id);

CREATE TABLE IF NOT EXISTS user_group_access (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_group_id UUID NOT NULL REFERENCES user_groups(id) ON DELETE CASCADE,
    device_serial TEXT REFERENCES devices(serial) ON DELETE CASCADE,
    group_id UUID REFERENCES device_groups(id) ON DELETE CASCADE,
    permission TEXT NOT NULL DEFAULT 'view' CHECK (permission IN ('view', 'control', 'manage')),
    granted_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uga_exactly_one_target CHECK (
        (device_serial IS NOT NULL AND group_id IS NULL) OR
        (device_serial IS NULL AND group_id IS NOT NULL)
    )
);

CREATE INDEX IF NOT EXISTS idx_user_group_access_user_group_id ON user_group_access(user_group_id);
CREATE INDEX IF NOT EXISTS idx_user_group_access_device_serial ON user_group_access(device_serial);
CREATE INDEX IF NOT EXISTS idx_user_group_access_group_id ON user_group_access(group_id);
