
ALTER TABLE oauth_grants ADD COLUMN pending_token_cipher bytea, ADD COLUMN invalid_at timestamptz;
CREATE TABLE session_rotation_receipts (
 refresh_hash bytea PRIMARY KEY, request_hash bytea NOT NULL,
 session_id uuid NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
 current_refresh_hash bytea NOT NULL, response_cipher bytea NOT NULL,
 expires_at timestamptz NOT NULL
);
CREATE INDEX session_rotation_receipts_expiry_idx ON session_rotation_receipts(expires_at);
ALTER TABLE github_device_flows ADD COLUMN next_poll_at timestamptz NOT NULL DEFAULT now();
