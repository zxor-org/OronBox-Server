package store

import (
	"context"
	"time"
)

var activityZone = time.FixedZone("Asia/Shanghai", 8*60*60)

type ActivityReceipt struct {
	Date         string    `json:"date"`
	ServerTime   time.Time `json:"server_time"`
	NextDayStart time.Time `json:"next_day_start"`
}

func ActivityDay(now time.Time) ActivityReceipt {
	local := now.In(activityZone)
	next := time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, activityZone)
	return ActivityReceipt{local.Format("2006-01-02"), now.UTC(), next.UTC()}
}
func (s *Store) RecordActivity(ctx context.Context, user, version, platform string, now time.Time) (ActivityReceipt, error) {
	receipt := ActivityDay(now)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return receipt, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO user_activity_daily(day,user_id)VALUES($1,$2) ON CONFLICT DO NOTHING`, receipt.Date, user); err != nil {
		return receipt, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO user_client_usage_daily(day,user_id,version,platform)VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, receipt.Date, user, version, platform); err != nil {
		return receipt, err
	}
	return receipt, tx.Commit()
}

type ActivityCoverage struct {
	Version  string `json:"version"`
	Platform string `json:"platform"`
	Users    int64  `json:"users"`
}
type ActivityStats struct {
	Date     string             `json:"date"`
	DAU      int64              `json:"dau"`
	WAU      int64              `json:"wau"`
	MAU      int64              `json:"mau"`
	Daily    []AnalyticsPoint   `json:"daily"`
	Coverage []ActivityCoverage `json:"coverage"`
}

func (s *Store) ActivityStats(ctx context.Context) (ActivityStats, error) {
	out := ActivityStats{Date: ActivityDay(time.Now()).Date, Daily: []AnalyticsPoint{}, Coverage: []ActivityCoverage{}}
	err := s.db.QueryRowContext(ctx, `SELECT count(DISTINCT user_id) FILTER(WHERE day=$1::date),count(DISTINCT user_id) FILTER(WHERE day >= $1::date-6),count(DISTINCT user_id) FROM user_activity_daily WHERE day BETWEEN $1::date-29 AND $1::date`, out.Date).Scan(&out.DAU, &out.WAU, &out.MAU)
	if err != nil {
		return out, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT d::date::text,count(a.user_id) FROM generate_series($1::date-29,$1::date,'1 day') d LEFT JOIN user_activity_daily a ON a.day=d::date GROUP BY d ORDER BY d`, out.Date)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var p AnalyticsPoint
		if err = rows.Scan(&p.Date, &p.Count); err != nil {
			rows.Close()
			return out, err
		}
		out.Daily = append(out.Daily, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	rows, err = s.db.QueryContext(ctx, `SELECT version,platform,count(DISTINCT user_id) FROM user_client_usage_daily WHERE day BETWEEN $1::date-29 AND $1::date GROUP BY version,platform ORDER BY count(DISTINCT user_id) DESC,version,platform`, out.Date)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var p ActivityCoverage
		if err = rows.Scan(&p.Version, &p.Platform, &p.Users); err != nil {
			return out, err
		}
		out.Coverage = append(out.Coverage, p)
	}
	return out, rows.Err()
}

type ActivityUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	LastDay  string `json:"last_day"`
}

func (s *Store) ActivityUsers(ctx context.Context, version, platform string, page int) ([]ActivityUser, int64, error) {
	items := []ActivityUser{}
	var total int64
	day := ActivityDay(time.Now()).Date
	err := s.db.QueryRowContext(ctx, `SELECT count(DISTINCT user_id) FROM user_client_usage_daily WHERE day BETWEEN $1::date-29 AND $1::date AND version=$2 AND platform=$3`, day, version, platform).Scan(&total)
	if err != nil {
		return items, 0, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT u.id,u.username,max(a.day)::text FROM user_client_usage_daily a JOIN users u ON u.id=a.user_id WHERE a.day BETWEEN $1::date-29 AND $1::date AND a.version=$2 AND a.platform=$3 GROUP BY u.id,u.username ORDER BY max(a.day) DESC,u.id LIMIT 30 OFFSET $4`, day, version, platform, (page-1)*30)
	if err != nil {
		return items, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var u ActivityUser
		if err = rows.Scan(&u.ID, &u.Username, &u.LastDay); err != nil {
			return items, 0, err
		}
		items = append(items, u)
	}
	return items, total, rows.Err()
}
