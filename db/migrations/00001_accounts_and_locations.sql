-- +goose Up
CREATE TABLE users (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    username text NOT NULL UNIQUE CHECK (username <> '' AND username = lower(btrim(username))),
    password_hash text NOT NULL CHECK (password_hash <> ''),
    display_name text NOT NULL CHECK (btrim(display_name) <> ''),
    role text NOT NULL CHECK (role IN ('ADMIN', 'OPERATOR', 'DOCUMENTATION')),
    is_active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE user_sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users (id),
    refresh_token_hash bytea NOT NULL UNIQUE CHECK (octet_length(refresh_token_hash) > 0),
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (expires_at > created_at)
);

CREATE INDEX user_sessions_active_user_idx ON user_sessions (user_id) WHERE revoked_at IS NULL;

CREATE TABLE loading_locations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL CHECK (btrim(name) <> ''),
    is_active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX loading_locations_name_idx ON loading_locations (lower(btrim(name)));

CREATE TABLE user_loading_locations (
    user_id uuid NOT NULL REFERENCES users (id),
    loading_location_id uuid NOT NULL REFERENCES loading_locations (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, loading_location_id)
);

CREATE INDEX user_loading_locations_location_idx ON user_loading_locations (loading_location_id);

-- +goose Down
DROP TABLE user_loading_locations;
DROP TABLE loading_locations;
DROP TABLE user_sessions;
DROP TABLE users;
