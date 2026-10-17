
CREATE TABLE resource_deletion_operations(
 id uuid PRIMARY KEY, resource_id uuid NOT NULL UNIQUE, owner_id uuid REFERENCES users(id) ON DELETE SET NULL,
 resource_name text NOT NULL, targets jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE resource_deletion_steps(
 id uuid PRIMARY KEY,operation_id uuid NOT NULL REFERENCES resource_deletion_operations(id),provider text NOT NULL,
 external_id text NOT NULL,state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','running','failed','awaiting_external','completed')),
 receipt text NOT NULL DEFAULT '',error_message text NOT NULL DEFAULT '',attempts int NOT NULL DEFAULT 0,
 next_attempt_at timestamptz NOT NULL DEFAULT now(),lease_token uuid,lease_expires_at timestamptz,
 updated_at timestamptz NOT NULL DEFAULT now(),UNIQUE(operation_id,provider,external_id)
);
CREATE INDEX deletion_steps_due ON resource_deletion_steps(state,next_attempt_at);
