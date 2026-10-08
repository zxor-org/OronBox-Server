package store

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"strings"
)

// SchemaSQL is intentionally plain PostgreSQL DDL so the server has no ORM
// runtime or generated client. Every table is safe to apply repeatedly.
const SchemaSQL = `
CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE TABLE IF NOT EXISTS users (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), bandbbs_uid integer UNIQUE NOT NULL, username varchar(64) NOT NULL, avatar_url text, role varchar(16) NOT NULL DEFAULT 'user', banned boolean NOT NULL DEFAULT false, ban_reason text, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE IF NOT EXISTS oauth_states (id varchar(128) PRIMARY KEY, provider varchar(32) NOT NULL, purpose varchar(32) NOT NULL, created_at timestamptz NOT NULL DEFAULT now(), expires_at timestamptz NOT NULL, used_at timestamptz, app_id varchar(64) NOT NULL, platform varchar(32) NOT NULL, return_uri varchar(512) NOT NULL, user_id uuid REFERENCES users(id) ON DELETE CASCADE, ip varchar(45), user_agent text NOT NULL);
CREATE TABLE IF NOT EXISTS oauth_events (id bigserial PRIMARY KEY, created_at timestamptz NOT NULL DEFAULT now(), provider varchar(32) NOT NULL, event_type varchar(32) NOT NULL, result varchar(32) NOT NULL, ip varchar(45) NOT NULL DEFAULT '', error_code varchar(64) NOT NULL DEFAULT '', error_message text NOT NULL DEFAULT '', latency_ms bigint NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS oauth_grants (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE, provider varchar(32) NOT NULL, subject varchar(64) NOT NULL, scopes text[] NOT NULL DEFAULT '{}', access_token_cipher bytea NOT NULL, refresh_token_cipher bytea, expires_at timestamptz, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(), UNIQUE(user_id,provider));
CREATE TABLE IF NOT EXISTS login_tickets (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), ticket_hash bytea UNIQUE NOT NULL, user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE, created_at timestamptz NOT NULL DEFAULT now(), expires_at timestamptz NOT NULL, used_at timestamptz);
CREATE TABLE IF NOT EXISTS sessions (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE, access_hash bytea UNIQUE NOT NULL, refresh_hash bytea UNIQUE NOT NULL, access_expires_at timestamptz NOT NULL, refresh_expires_at timestamptz NOT NULL, revoked_at timestamptz, created_at timestamptz NOT NULL DEFAULT now(), last_seen_at timestamptz NOT NULL DEFAULT now(), platform varchar(32) NOT NULL);
CREATE TABLE IF NOT EXISTS admin_sessions (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), session_hash bytea UNIQUE NOT NULL, user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE, username varchar(64) NOT NULL, ip varchar(45), expires_at timestamptz NOT NULL, created_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE IF NOT EXISTS github_grants (user_id uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE, github_user_id bigint NOT NULL, login varchar(64) NOT NULL, access_token_cipher bytea NOT NULL, scopes text[] NOT NULL DEFAULT '{}', created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE IF NOT EXISTS resource_interactions (resource_id varchar(128) PRIMARY KEY, title varchar(128) NOT NULL DEFAULT '', latest_version varchar(32) NOT NULL DEFAULT '0.0.1', restype varchar(32) NOT NULL DEFAULT 'quick_app', owner_id uuid NOT NULL REFERENCES users(id), download_count integer NOT NULL DEFAULT 0, coin_count integer NOT NULL DEFAULT 0, rating double precision NOT NULL DEFAULT 5.0, rating_count integer NOT NULL DEFAULT 0, updated_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE IF NOT EXISTS resource_comments (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), resource_id varchar(128) NOT NULL REFERENCES resource_interactions(resource_id) ON DELETE CASCADE, user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE, parent_id uuid REFERENCES resource_comments(id) ON DELETE SET NULL, content text NOT NULL, state varchar(16) NOT NULL DEFAULT 'visible', is_deleted boolean NOT NULL DEFAULT false, moderation_reason text, created_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE IF NOT EXISTS resource_coin_votes (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), resource_id varchar(128) NOT NULL REFERENCES resource_interactions(resource_id) ON DELETE CASCADE, user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE, amount integer NOT NULL DEFAULT 1 CHECK(amount>0), created_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE IF NOT EXISTS resource_submissions (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), resource_id varchar(128) NOT NULL REFERENCES resource_interactions(resource_id) ON DELETE CASCADE, title varchar(128) NOT NULL DEFAULT '', version varchar(32) NOT NULL DEFAULT '0.0.1', creator_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE, pr_number integer, pr_url varchar(512), branch varchar(128), repo_commit_hash varchar(64) NOT NULL DEFAULT '', config jsonb NOT NULL DEFAULT '{}', status varchar(32) NOT NULL DEFAULT 'draft', created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(), UNIQUE(resource_id,version));
CREATE TABLE IF NOT EXISTS resource_collaborators (resource_id varchar(128) NOT NULL REFERENCES resource_interactions(resource_id) ON DELETE CASCADE, user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE, role varchar(32) NOT NULL DEFAULT 'collaborator', status varchar(16) NOT NULL DEFAULT 'pending', invited_by uuid REFERENCES users(id) ON DELETE SET NULL, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(resource_id,user_id));
CREATE TABLE IF NOT EXISTS publications (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), submission_id uuid NOT NULL REFERENCES resource_submissions(id) ON DELETE CASCADE, provider varchar(32) NOT NULL, category_id integer NOT NULL DEFAULT 0, state varchar(32) NOT NULL DEFAULT 'pending', config jsonb NOT NULL DEFAULT '{}', external_id varchar(128), external_url varchar(512), error_message text, status_detail jsonb, attempts integer NOT NULL DEFAULT 0, next_attempt_at timestamptz, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(), UNIQUE(submission_id,provider,category_id));
CREATE TABLE IF NOT EXISTS publication_attempts (id bigserial PRIMARY KEY, publication_id uuid NOT NULL REFERENCES publications(id) ON DELETE CASCADE, attempt_number integer NOT NULL, phase varchar(16) NOT NULL, state_from varchar(32) NOT NULL, state_to varchar(32) NOT NULL, error_message text NOT NULL DEFAULT '', detail jsonb, created_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE IF NOT EXISTS external_bindings (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), resource_id varchar(128) NOT NULL REFERENCES resource_interactions(resource_id) ON DELETE CASCADE, provider varchar(32) NOT NULL, category_id integer NOT NULL DEFAULT 0, external_id varchar(128) NOT NULL, external_url varchar(512) NOT NULL DEFAULT '', meta jsonb NOT NULL DEFAULT '{}', created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(), UNIQUE(resource_id,provider,category_id), UNIQUE(provider,external_id));
CREATE TABLE IF NOT EXISTS review_cases (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), submission_id uuid UNIQUE NOT NULL REFERENCES resource_submissions(id) ON DELETE CASCADE, state varchar(16) NOT NULL DEFAULT 'pending', priority integer NOT NULL DEFAULT 0, assigned_reviewer_id uuid REFERENCES users(id) ON DELETE SET NULL, checklist jsonb NOT NULL DEFAULT '[]', note text NOT NULL DEFAULT '', created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE IF NOT EXISTS review_case_events (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), case_id uuid NOT NULL REFERENCES review_cases(id) ON DELETE CASCADE, actor_id uuid REFERENCES users(id) ON DELETE SET NULL, event varchar(32) NOT NULL, note text NOT NULL DEFAULT '', checklist jsonb NOT NULL DEFAULT '[]', created_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE IF NOT EXISTS review_appeals (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE, subject_type varchar(32) NOT NULL, subject_id varchar(128) NOT NULL, message text NOT NULL, status varchar(16) NOT NULL DEFAULT 'open', resolution text NOT NULL DEFAULT '', handled_by uuid REFERENCES users(id) ON DELETE SET NULL, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE IF NOT EXISTS moderation_reason_templates (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), scope varchar(32) NOT NULL, decision varchar(16) NOT NULL DEFAULT '', title varchar(128) NOT NULL, body text NOT NULL, position integer NOT NULL DEFAULT 0, enabled boolean NOT NULL DEFAULT true);
CREATE TABLE IF NOT EXISTS user_messages (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE, kind varchar(32) NOT NULL DEFAULT 'system', event varchar(64) NOT NULL DEFAULT '', data jsonb NOT NULL DEFAULT '{}', title varchar(128), body text, ref varchar(128), read_at timestamptz, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(), expires_at timestamptz);
CREATE TABLE IF NOT EXISTS user_coin_accounts (user_id uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE, balance integer NOT NULL DEFAULT 0 CHECK(balance>=0), last_checkin_date date, updated_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE IF NOT EXISTS daily_coin_checkins (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE, checkin_date date NOT NULL, coins_awarded integer NOT NULL DEFAULT 1, streak_days integer NOT NULL DEFAULT 1, created_at timestamptz NOT NULL DEFAULT now(), UNIQUE(user_id,checkin_date));
CREATE TABLE IF NOT EXISTS coin_ledger (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE, delta_units integer NOT NULL, kind varchar(32) NOT NULL, reference_type varchar(32), reference_id varchar(128), note text NOT NULL DEFAULT '', balance_after integer NOT NULL, created_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE IF NOT EXISTS feedback_tickets (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE, target_source varchar(32), target_id varchar(128), title varchar(128) NOT NULL, content text NOT NULL, status varchar(16) NOT NULL DEFAULT 'open', created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE IF NOT EXISTS feedback_replies (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), ticket_id uuid NOT NULL REFERENCES feedback_tickets(id) ON DELETE CASCADE, author_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE, message text NOT NULL, is_admin boolean NOT NULL DEFAULT false, created_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE IF NOT EXISTS feedback_internal_notes (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), ticket_id uuid NOT NULL REFERENCES feedback_tickets(id) ON DELETE CASCADE, author_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE, message text NOT NULL, created_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE IF NOT EXISTS feedback_status_history (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), ticket_id uuid NOT NULL REFERENCES feedback_tickets(id) ON DELETE CASCADE, actor_id uuid REFERENCES users(id) ON DELETE SET NULL, from_status varchar(16) NOT NULL, to_status varchar(16) NOT NULL, created_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE IF NOT EXISTS app_releases (version varchar(32) PRIMARY KEY, minimum_version varchar(32) NOT NULL DEFAULT '', notes_zh text NOT NULL DEFAULT '', notes_en text NOT NULL DEFAULT '', published_at timestamptz NOT NULL DEFAULT now(), synced_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE IF NOT EXISTS download_events (id bigserial PRIMARY KEY, resource_id varchar(128) NOT NULL, user_id uuid REFERENCES users(id) ON DELETE SET NULL, ip varchar(45) NOT NULL, user_agent text NOT NULL DEFAULT '', created_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE IF NOT EXISTS audit_logs (id bigserial PRIMARY KEY, actor_user_id uuid REFERENCES users(id) ON DELETE SET NULL, action varchar(64) NOT NULL, result varchar(32) NOT NULL, ip varchar(45), target_data jsonb NOT NULL DEFAULT '{}', created_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE IF NOT EXISTS server_settings (key varchar(64) PRIMARY KEY, value jsonb NOT NULL, updated_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE IF NOT EXISTS plugins (id varchar(128) PRIMARY KEY, uploader_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT, name varchar(128) NOT NULL, version varchar(64) NOT NULL, author varchar(128) NOT NULL, description text NOT NULL DEFAULT '', runtime varchar(32) NOT NULL DEFAULT 'js', permissions jsonb NOT NULL DEFAULT '[]'::jsonb, state varchar(32) NOT NULL DEFAULT 'pending', moderation_reason text NOT NULL DEFAULT '', package_sha256 char(64) NOT NULL DEFAULT '', created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now());
CREATE INDEX IF NOT EXISTS plugins_uploader_idx ON plugins(uploader_id);
CREATE INDEX IF NOT EXISTS plugins_state_idx ON plugins(state);
CREATE INDEX IF NOT EXISTS resource_comments_resource_idx ON resource_comments(resource_id,created_at);
CREATE INDEX IF NOT EXISTS resource_coin_votes_resource_user_idx ON resource_coin_votes(resource_id,user_id);
CREATE INDEX IF NOT EXISTS resource_submissions_creator_status_idx ON resource_submissions(creator_id,status,updated_at DESC);
CREATE INDEX IF NOT EXISTS resource_collaborators_user_status_idx ON resource_collaborators(user_id,status);
CREATE INDEX IF NOT EXISTS publications_submission_state_idx ON publications(submission_id,state);
CREATE INDEX IF NOT EXISTS user_messages_user_idx ON user_messages(user_id,kind,read_at,created_at DESC);
CREATE INDEX IF NOT EXISTS app_releases_published_idx ON app_releases(published_at DESC);
ALTER TABLE resource_submissions ADD COLUMN IF NOT EXISTS repo_commit_hash varchar(64) NOT NULL DEFAULT '';
ALTER TABLE resource_submissions ADD COLUMN IF NOT EXISTS config jsonb NOT NULL DEFAULT '{}';
CREATE INDEX IF NOT EXISTS download_events_resource_idx ON download_events(resource_id,ip,created_at);
CREATE INDEX IF NOT EXISTS user_coin_ledger_idx ON coin_ledger(user_id,created_at);
ALTER TABLE resource_comments ADD COLUMN IF NOT EXISTS ai_action varchar(16) NOT NULL DEFAULT 'pass';
ALTER TABLE resource_comments ADD COLUMN IF NOT EXISTS ai_reason text NOT NULL DEFAULT '';
ALTER TABLE resource_comments ADD COLUMN IF NOT EXISTS ai_model varchar(64) NOT NULL DEFAULT '';
CREATE TABLE IF NOT EXISTS blogs (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), title varchar(256) NOT NULL, slug varchar(128) UNIQUE NOT NULL, summary text NOT NULL DEFAULT '', content text NOT NULL, cover_url text NOT NULL DEFAULT '', category varchar(32) NOT NULL DEFAULT 'announcement', status varchar(16) NOT NULL DEFAULT 'published', author_id uuid REFERENCES users(id) ON DELETE SET NULL, view_count integer NOT NULL DEFAULT 0, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now());
CREATE INDEX IF NOT EXISTS blogs_status_idx ON blogs(status, created_at DESC);
`

func Migrate(ctx context.Context, s *Store) error {
	if s == nil || s.Pool == nil {
		return fmt.Errorf("store is not initialized")
	}
	return s.WithTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(4242)`); err != nil {
			return err
		}
		var legacy bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_name='app_releases' AND column_name='download_url')`).Scan(&legacy); err == nil && legacy {
			if _, err := tx.Exec(ctx, `DROP TABLE app_releases`); err != nil {
				return err
			}
		}
		for _, statement := range strings.Split(SchemaSQL, ";") {
			statement = strings.TrimSpace(statement)
			if statement == "" {
				continue
			}
			if _, err := tx.Exec(ctx, statement); err != nil {
				return err
			}
		}
		return nil
	})
}

// applySchema is kept separate to make the migration transaction easy to test.
func applySchema(ctx context.Context, s *Store) error {
	for _, statement := range strings.Split(SchemaSQL, ";") {
		statement = strings.TrimSpace(statement)
		if statement == "" {
			continue
		}
		if _, err := s.Pool.Exec(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

func Schema(ctx context.Context, s *Store) error {
	if s == nil || s.Pool == nil {
		return fmt.Errorf("store is not initialized")
	}
	if err := applySchema(ctx, s); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	return nil
}
