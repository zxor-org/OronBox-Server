package store

import (
	"context"
	"database/sql"
	"time"
)

// AdminHealthDiagnostics is a read-only operational snapshot used by the
// administration health page. Counts intentionally come from the database so
// the page remains useful when workers or object storage are degraded.
type AdminHealthDiagnostics struct {
	DatabaseSizeBytes int64
	DatabaseSessions  int64
	Publications      AdminPublicationQueueDiagnostics
	Blobs             AdminBlobDiagnostics
	OAuth             AdminOAuthDiagnostics
}

type AdminPublicationQueueDiagnostics struct {
	Pending, Running, Reviewing, Published, Failed, Cancelled int64
	Ready, Delayed, StaleRunning                              int64
}

type AdminBlobDiagnostics struct {
	Count, SizeBytes                                 int64
	ReplicaMissing, ReplicaPending, ReplicaUploading int64
	ReplicaReady, ReplicaFailed, ReplicaRetryReady   int64
}

type AdminOAuthDiagnostics struct {
	Events24Hours, Failures24Hours int64
	FailureRate                    float64
}

func (s *Store) AdminHealthDiagnostics(ctx context.Context) (AdminHealthDiagnostics, error) {
	var result AdminHealthDiagnostics
	err := s.db.QueryRowContext(ctx, `
SELECT
 pg_database_size(current_database()),
 (SELECT count(*) FROM pg_stat_activity WHERE datname=current_database()),
 count(*) FILTER (WHERE publication.state='pending'),
 count(*) FILTER (WHERE publication.state='running'),
 count(*) FILTER (WHERE publication.state='reviewing'),
 count(*) FILTER (WHERE publication.state='published'),
 count(*) FILTER (WHERE publication.state='failed'),
 count(*) FILTER (WHERE publication.state='cancelled'),
	count(*) FILTER (WHERE publication.state='pending' AND publication.next_attempt_at<=now()),
	count(*) FILTER (WHERE publication.state='pending' AND publication.next_attempt_at>now()),
 count(*) FILTER (WHERE publication.state='running' AND publication.updated_at<now()-interval '15 minutes'),
 (SELECT count(*) FROM blobs),
 (SELECT COALESCE(sum(size_bytes),0) FROM blobs),
 (SELECT count(*) FROM blobs blob WHERE NOT EXISTS (SELECT 1 FROM blob_replicas replica WHERE replica.blob_sha256=blob.sha256 AND replica.backend='r2')),
 (SELECT count(*) FROM blob_replicas WHERE backend='r2' AND state='pending'),
 (SELECT count(*) FROM blob_replicas WHERE backend='r2' AND state='uploading'),
 (SELECT count(*) FROM blob_replicas WHERE backend='r2' AND state='ready'),
 (SELECT count(*) FROM blob_replicas WHERE backend='r2' AND state='failed'),
 (SELECT count(*) FROM blob_replicas WHERE backend='r2' AND state='failed' AND next_attempt_at<=now()),
 (SELECT count(*) FROM oauth_events WHERE created_at>=now()-interval '24 hours'),
 (SELECT count(*) FROM oauth_events WHERE created_at>=now()-interval '24 hours' AND result='failure')
FROM publications publication`).Scan(
		&result.DatabaseSizeBytes, &result.DatabaseSessions,
		&result.Publications.Pending, &result.Publications.Running, &result.Publications.Reviewing,
		&result.Publications.Published, &result.Publications.Failed, &result.Publications.Cancelled,
		&result.Publications.Ready, &result.Publications.Delayed, &result.Publications.StaleRunning,
		&result.Blobs.Count, &result.Blobs.SizeBytes, &result.Blobs.ReplicaMissing,
		&result.Blobs.ReplicaPending, &result.Blobs.ReplicaUploading, &result.Blobs.ReplicaReady,
		&result.Blobs.ReplicaFailed, &result.Blobs.ReplicaRetryReady,
		&result.OAuth.Events24Hours, &result.OAuth.Failures24Hours,
	)
	if err != nil {
		return AdminHealthDiagnostics{}, err
	}
	if result.OAuth.Events24Hours > 0 {
		result.OAuth.FailureRate = float64(result.OAuth.Failures24Hours) * 100 / float64(result.OAuth.Events24Hours)
	}
	return result, nil
}

type AdminCleanupPreview struct {
	Cutoff        time.Time `json:"cutoff"`
	OAuthStates   int64     `json:"oauth_states"`
	LoginTickets  int64     `json:"login_tickets"`
	AdminSessions int64     `json:"admin_sessions"`
	UserMessages  int64     `json:"user_messages"`
}

func (preview AdminCleanupPreview) Total() int64 {
	return preview.OAuthStates + preview.LoginTickets + preview.AdminSessions + preview.UserMessages
}

func (s *Store) PreviewExpiredCleanup(ctx context.Context, cutoff time.Time) (AdminCleanupPreview, error) {
	preview := AdminCleanupPreview{Cutoff: cutoff.UTC()}
	err := s.db.QueryRowContext(ctx, `SELECT
 (SELECT count(*) FROM oauth_states WHERE expires_at<$1),
 (SELECT count(*) FROM login_tickets WHERE expires_at<$1),
 (SELECT count(*) FROM admin_sessions WHERE expires_at<$1),
 (SELECT count(*) FROM user_messages WHERE expires_at<$1)`, preview.Cutoff).Scan(
		&preview.OAuthStates, &preview.LoginTickets, &preview.AdminSessions, &preview.UserMessages,
	)
	return preview, err
}

// ExecuteExpiredCleanup only deletes rows covered by a prior preview cutoff.
// The transaction keeps the four classes of cleanup atomic.
func (s *Store) ExecuteExpiredCleanup(ctx context.Context, preview AdminCleanupPreview) (CleanupStats, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return CleanupStats{}, err
	}
	defer tx.Rollback()
	stats := CleanupStats{}
	for _, item := range []struct {
		query string
		count *int64
	}{
		{`DELETE FROM oauth_states WHERE expires_at<$1`, &stats.OAuthStates},
		{`DELETE FROM login_tickets WHERE expires_at<$1`, &stats.LoginTickets},
		{`DELETE FROM admin_sessions WHERE expires_at<$1`, &stats.AdminSessions},
		{`DELETE FROM user_messages WHERE expires_at<$1`, &stats.UserMessages},
	} {
		result, execErr := tx.ExecContext(ctx, item.query, preview.Cutoff.UTC())
		if execErr != nil {
			return CleanupStats{}, execErr
		}
		*item.count, _ = result.RowsAffected()
	}
	if err := tx.Commit(); err != nil {
		return CleanupStats{}, err
	}
	return stats, nil
}
