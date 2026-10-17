package creator

import (
	"context"
	"math"
	"strconv"

	"github.com/zxor-org/OronBox-Server/internal/store"
)

// Ranking holds the tunable multipliers of the recommendation score. Zero
// fields fall back to the defaults; values are validated before they are
// formatted into SQL literals.
type Ranking struct {
	CoinExtraWeight    float64
	DownloadWeight     float64
	FreshnessAmplitude float64
	FreshnessDecayDays float64
	FeaturedBoost      float64
	JitterBase         float64
}

func (r Ranking) normalized() Ranking {
	if r.CoinExtraWeight <= 0 {
		r.CoinExtraWeight = 0.35
	}
	if r.DownloadWeight <= 0 {
		r.DownloadWeight = 0.15
	}
	if r.FreshnessAmplitude <= 0 {
		r.FreshnessAmplitude = 3.0
	}
	if r.FreshnessDecayDays <= 0 {
		r.FreshnessDecayDays = 7.0
	}
	if r.FeaturedBoost <= 0 {
		r.FeaturedBoost = 1.5
	}
	if r.JitterBase <= 0 {
		r.JitterBase = 0.50
	}
	for _, value := range []float64{r.CoinExtraWeight, r.DownloadWeight, r.FreshnessAmplitude, r.FreshnessDecayDays, r.FeaturedBoost, r.JitterBase} {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return Ranking{}
		}
	}
	return r
}

// RankingSettings returns the effective score multipliers: the environment
// defaults from s.Ranking, overlaid by any operator overrides stored in
// server_settings. Unreadable settings keep the defaults rather than failing
// the feed.
func (s *Service) RankingSettings(ctx context.Context) Ranking {
	ranking := s.Ranking
	rows, err := s.db.QueryContext(ctx, `SELECT key,value FROM server_settings WHERE key=ANY($1::text[])`, []string{
		store.RankingCoinExtraKey, store.RankingDownloadKey, store.RankingFreshnessAmpKey,
		store.RankingFreshnessDecay, store.RankingFeaturedKey, store.RankingJitterKey,
	})
	if err != nil {
		return ranking
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return ranking
		}
		parsed, parseErr := strconv.ParseFloat(value, 64)
		if parseErr != nil {
			continue
		}
		switch key {
		case store.RankingCoinExtraKey:
			ranking.CoinExtraWeight = parsed
		case store.RankingDownloadKey:
			ranking.DownloadWeight = parsed
		case store.RankingFreshnessAmpKey:
			ranking.FreshnessAmplitude = parsed
		case store.RankingFreshnessDecay:
			ranking.FreshnessDecayDays = parsed
		case store.RankingFeaturedKey:
			ranking.FeaturedBoost = parsed
		case store.RankingJitterKey:
			ranking.JitterBase = parsed
		}
	}
	return ranking
}
