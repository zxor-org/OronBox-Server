package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// AdminAnalytics aggregates user signups and downloads over a trailing
// window. Only data already stored today is used; there is no activity or
// session touch involved, so daily-active style metrics are intentionally out
// of scope.
type AdminAnalytics struct {
	Range      string            `json:"range"`
	UserGrowth []AnalyticsPoint  `json:"user_growth"`
	Downloads  []AnalyticsPoint  `json:"downloads"`
	Totals     AdminAnalyticsSum `json:"totals"`
}

type AnalyticsPoint struct {
	Date  string `json:"date"`
	Count int64  `json:"count"`
}

type AdminAnalyticsSum struct {
	TotalUsers         int64 `json:"total_users"`
	TotalDownloads     int64 `json:"total_downloads"`
	Downloads7d        int64 `json:"downloads_7d"`
	Downloads30d       int64 `json:"downloads_30d"`
	NewUsers7d         int64 `json:"new_users_7d"`
	NewUsers30d        int64 `json:"new_users_30d"`
	Resources          int64 `json:"resources"`
	PublishedResources int64 `json:"published_resources"`
}

const (
	adminAnalyticsDayBuckets   = "day"
	adminAnalyticsMonthBuckets = "month"
)

// analyticsBucketExpr buckets in UTC regardless of the session timezone, so
// the SQL labels always agree with the Go-side gap fill.
func analyticsBucketExpr(bucket string) string {
	return fmt.Sprintf("date_trunc('%s', created_at AT TIME ZONE 'UTC')::date", bucket)
}

func (s *Store) AdminAnalytics(ctx context.Context, raw string) (AdminAnalytics, error) {
	now := time.Now().UTC()
	rangeName, bucket, start := "30d", adminAnalyticsDayBuckets, time.Now().UTC().AddDate(0, 0, -29)
	switch raw {
	case "90d":
		rangeName, bucket = "90d", adminAnalyticsDayBuckets
		start = now.AddDate(0, 0, -89)
	case "12m":
		rangeName, bucket = "12m", adminAnalyticsMonthBuckets
		start = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -11, 0)
	}
	if bucket == adminAnalyticsDayBuckets {
		start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
	}

	result := AdminAnalytics{Range: rangeName}
	if err := s.db.QueryRowContext(ctx, `
SELECT
 (SELECT count(*) FROM users),
 (SELECT count(*) FROM download_events),
 (SELECT count(*) FROM download_events WHERE created_at>now()-interval '7 days'),
 (SELECT count(*) FROM download_events WHERE created_at>now()-interval '30 days'),
 (SELECT count(*) FROM users WHERE created_at>now()-interval '7 days'),
 (SELECT count(*) FROM users WHERE created_at>now()-interval '30 days'),
 (SELECT count(*) FROM resources),
 (SELECT count(*) FROM resources WHERE moderation_state='visible')`).Scan(
		&result.Totals.TotalUsers, &result.Totals.TotalDownloads,
		&result.Totals.Downloads7d, &result.Totals.Downloads30d,
		&result.Totals.NewUsers7d, &result.Totals.NewUsers30d,
		&result.Totals.Resources, &result.Totals.PublishedResources,
	); err != nil {
		return AdminAnalytics{}, err
	}

	userRows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
SELECT to_char(%s,'YYYY-MM-DD'), count(*)
FROM users WHERE created_at>=$1 GROUP BY 1`, analyticsBucketExpr(bucket)), start)
	if err != nil {
		return AdminAnalytics{}, err
	}
	result.UserGrowth, err = collectAnalyticsSeries(userRows, start, bucket, now)
	userRows.Close()
	if err != nil {
		return AdminAnalytics{}, err
	}

	downloadRows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
SELECT to_char(%s,'YYYY-MM-DD'), count(*)
FROM download_events WHERE created_at>=$1 GROUP BY 1`, analyticsBucketExpr(bucket)), start)
	if err != nil {
		return AdminAnalytics{}, err
	}
	result.Downloads, err = collectAnalyticsSeries(downloadRows, start, bucket, now)
	downloadRows.Close()
	if err != nil {
		return AdminAnalytics{}, err
	}

	return result, nil
}

// collectAnalyticsSeries reads an aggregated "YYYY-MM-DD,count" result set and
// pads every bucket in the window so charts never have gaps. The bucket string
// must match the one used in the query so the Go cursor lands on the same
// labels. The day series is inclusive of today (exactly one bucket per day in
// the window); the month series stops before the current month (exactly twelve
// buckets).
func collectAnalyticsSeries(rows *sql.Rows, start time.Time, bucket string, now time.Time) ([]AnalyticsPoint, error) {
	counts := make(map[string]int64)
	for rows.Next() {
		var date string
		var count int64
		if err := rows.Scan(&date, &count); err != nil {
			return nil, err
		}
		counts[date] = count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	points := []AnalyticsPoint{}
	if bucket == adminAnalyticsMonthBuckets {
		end := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		for cursor := start; cursor.Before(end); cursor = cursor.AddDate(0, 1, 0) {
			points = append(points, AnalyticsPoint{Date: cursor.Format("2006-01-02"), Count: counts[cursor.Format("2006-01-02")]})
		}
		return points, nil
	}
	for cursor := start; !cursor.After(now); cursor = cursor.Add(24 * time.Hour) {
		points = append(points, AnalyticsPoint{Date: cursor.Format("2006-01-02"), Count: counts[cursor.Format("2006-01-02")]})
	}
	return points, nil
}
