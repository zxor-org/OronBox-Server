package store

import (
	"context"
	"fmt"
	"math"
	"strconv"
)

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
