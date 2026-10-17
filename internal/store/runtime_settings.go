package store

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"

	"github.com/google/uuid"
)

func (s *Store) UpdateSetting(ctx context.Context, key string, apply func(string) (string, error)) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO server_settings(key,value)VALUES($1,'')ON CONFLICT DO NOTHING`, key); err != nil {
		return err
	}
	var old string
	if err = tx.QueryRowContext(ctx, `SELECT value FROM server_settings WHERE key=$1 FOR UPDATE`, key).Scan(&old); err != nil {
		return err
	}
	next, err := apply(old)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE server_settings SET value=$2,updated_at=now() WHERE key=$1`, key, next); err != nil {
		return err
	}
	return tx.Commit()
}

type ReasonTemplate struct {
	ID       string `json:"id"`
	Scope    string `json:"scope"`
	Decision string `json:"decision"`
	Title    string `json:"title"`
	Body     string `json:"body"`
	Position int    `json:"position"`
	Enabled  bool   `json:"enabled"`
}

func (s *Store) ReasonTemplates(ctx context.Context, scope string) ([]ReasonTemplate, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id::text,scope,decision,title,body,position,enabled FROM moderation_reason_templates WHERE ($1='' OR scope=$1)ORDER BY position,id`, scope)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ReasonTemplate{}
	for rows.Next() {
		var v ReasonTemplate
		if err = rows.Scan(&v.ID, &v.Scope, &v.Decision, &v.Title, &v.Body, &v.Position, &v.Enabled); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) SaveReasonTemplate(ctx context.Context, v ReasonTemplate, actor string) error {
	if v.ID == "" {
		v.ID = uuid.NewString()
	}
	switch v.Scope {
	case "resource_review", "comment", "plugin", "collection", "report", "account":
	default:
		return fmt.Errorf("invalid template scope")
	}
	switch v.Decision {
	case "", "approve", "reject", "hide", "suspend", "dismiss":
	default:
		return fmt.Errorf("invalid template decision")
	}
	if len(v.Title) == 0 || len(v.Title) > 200 || len(v.Body) == 0 || len(v.Body) > 4000 {
		return fmt.Errorf("invalid template content")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO moderation_reason_templates(id,scope,decision,title,body,position,enabled,created_by)VALUES($1,$2,$3,$4,$5,$6,$7,NULLIF($8,'')::uuid)ON CONFLICT(id)DO UPDATE SET scope=excluded.scope,decision=excluded.decision,title=excluded.title,body=excluded.body,position=excluded.position,enabled=excluded.enabled,updated_at=now()`, v.ID, v.Scope, v.Decision, v.Title, v.Body, v.Position, v.Enabled, actor)
	return err
}

func (s *Store) DisableReasonTemplate(ctx context.Context, id string) error {
	r, err := s.db.ExecContext(ctx, `UPDATE moderation_reason_templates SET enabled=false,updated_at=now() WHERE id=$1`, id)
	if err != nil {
		return err
	}
	n, _ := r.RowsAffected()
	if n == 0 {
		return fmt.Errorf("template not found")
	}
	return nil
}

func EncodeSetting(value any) (string, error) {
	raw, err := json.Marshal(value)
	return string(raw), err
}

// SetSettings commits related policy values together.
func (s *Store) SetSettings(ctx context.Context, values map[string]string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for key, value := range values {
		if _, err = tx.ExecContext(ctx, `INSERT INTO server_settings(key,value)VALUES($1,$2) ON CONFLICT(key)DO UPDATE SET value=excluded.value,updated_at=now()`, key, value); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Ranking weight keys live in server_settings so operators can tune the
// recommendation score without a redeploy. The environment defaults from
// RANKING_* are only the fallback for keys that were never overridden.
const (
	RankingCoinExtraKey    = "ranking.coin_extra_weight"
	RankingDownloadKey     = "ranking.download_weight"
	RankingFreshnessAmpKey = "ranking.freshness_amplitude"
	RankingFreshnessDecay  = "ranking.freshness_decay_days"
	RankingFeaturedKey     = "ranking.featured_boost"
	RankingJitterKey       = "ranking.jitter_base"
)

type RankingWeights struct {
	CoinExtraWeight    float64
	DownloadWeight     float64
	FreshnessAmplitude float64
	FreshnessDecayDays float64
	FeaturedBoost      float64
	JitterBase         float64
}

var rankingWeightKeys = []string{RankingCoinExtraKey, RankingDownloadKey, RankingFreshnessAmpKey, RankingFreshnessDecay, RankingFeaturedKey, RankingJitterKey}

// RankingWeights reads every override that exists in server_settings. A zero
// field means the key was never set and the caller keeps its fallback.
func (s *Store) RankingWeights(ctx context.Context) (RankingWeights, error) {
	var weights RankingWeights
	rows, err := s.db.QueryContext(ctx, `SELECT key,value FROM server_settings WHERE key=ANY($1::text[])`, rankingWeightKeys)
	if err != nil {
		return weights, err
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return weights, err
		}
		parsed, parseErr := strconv.ParseFloat(value, 64)
		if parseErr != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) || parsed <= 0 {
			continue
		}
		switch key {
		case RankingCoinExtraKey:
			weights.CoinExtraWeight = parsed
		case RankingDownloadKey:
			weights.DownloadWeight = parsed
		case RankingFreshnessAmpKey:
			weights.FreshnessAmplitude = parsed
		case RankingFreshnessDecay:
			weights.FreshnessDecayDays = parsed
		case RankingFeaturedKey:
			weights.FeaturedBoost = parsed
		case RankingJitterKey:
			weights.JitterBase = parsed
		}
	}
	return weights, rows.Err()
}

// SaveRankingWeights upserts all five keys, rejecting any value that is not a
// positive finite number.
func (s *Store) SaveRankingWeights(ctx context.Context, weights RankingWeights) error {
	values := []struct {
		key   string
		value float64
	}{
		{RankingCoinExtraKey, weights.CoinExtraWeight},
		{RankingDownloadKey, weights.DownloadWeight},
		{RankingFreshnessAmpKey, weights.FreshnessAmplitude},
		{RankingFreshnessDecay, weights.FreshnessDecayDays},
		{RankingFeaturedKey, weights.FeaturedBoost},
		{RankingJitterKey, weights.JitterBase},
	}
	for _, entry := range values {
		if math.IsNaN(entry.value) || math.IsInf(entry.value, 0) || entry.value <= 0 {
			return fmt.Errorf("ranking weight %s must be a positive number", entry.key)
		}
		if err := s.SetSetting(ctx, entry.key, strconv.FormatFloat(entry.value, 'f', -1, 64)); err != nil {
			return err
		}
	}
	return nil
}
