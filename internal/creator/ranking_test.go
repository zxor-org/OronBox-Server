package creator

import (
	"strings"
	"testing"
)

func TestPublicCardsSQLRendersConfiguredRankingWeights(t *testing.T) {
	sql := publicCardsSQL(Ranking{
		CoinExtraWeight:    0.5,
		DownloadWeight:     0.25,
		FreshnessAmplitude: 4.0,
		FreshnessDecayDays: 14.0,
		FeaturedBoost:      2.0,
		JitterBase:         0.25,
	})
	for _, fragment := range []string{
		`0.5 * GREATEST`,
		`0.25 * ln(1.0 + GREATEST(r.download_count`,
		`(1.0 + 4 * exp(-GREATEST(EXTRACT(EPOCH FROM (now()-COALESCE(r.published_at`,
		`/14))`,
		`THEN 2 ELSE 1.0 END`,
		`(0.25 + (((hashtextextended(r.id::text,$5) % 10000`,
		`(0.25 + (((hashtextextended(c.id::text,$5) % 10000`,
	} {
		if !strings.Contains(sql, fragment) {
			t.Errorf("resource score is missing %q", fragment)
		}
	}
	if strings.Contains(sql, "RESOURCE_SCORE") || strings.Contains(sql, "COLLECTION_SCORE") || strings.Contains(sql, "RESOURCE_JITTER") || strings.Contains(sql, "COLLECTION_JITTER") {
		t.Fatal("score markers were not replaced")
	}
}

func TestPublicCardsSQLFallsBackToDefaultsForZeroRanking(t *testing.T) {
	sql := publicCardsSQL(Ranking{})
	for _, fragment := range []string{
		`0.35 * GREATEST`,
		`0.15 * ln(1.0 + GREATEST(r.download_count`,
		`(1.0 + 3 * exp(-GREATEST(EXTRACT(EPOCH FROM (now()-COALESCE(r.published_at`,
		`/7))`,
		`THEN 1.5 ELSE 1.0 END`,
		`(0.5 + (((hashtextextended(r.id::text,$5) % 10000`,
		`(0.5 + (((hashtextextended(c.id::text,$5) % 10000`,
	} {
		if !strings.Contains(sql, fragment) {
			t.Errorf("default score is missing %q", fragment)
		}
	}
	if strings.Contains(sql, "RESOURCE_SCORE") || strings.Contains(sql, "COLLECTION_SCORE") || strings.Contains(sql, "RESOURCE_JITTER") || strings.Contains(sql, "COLLECTION_JITTER") {
		t.Fatal("score markers were not replaced")
	}
}

func TestRankingNormalizedRejectsNonFiniteValues(t *testing.T) {
	ranking := Ranking{CoinExtraWeight: 1, DownloadWeight: 1, FreshnessAmplitude: 1, FreshnessDecayDays: 1, FeaturedBoost: 1}
	ranking.DownloadWeight = 0
	if got := ranking.normalized(); got.DownloadWeight != 0.15 {
		t.Fatalf("zero field = %v, want the default", got.DownloadWeight)
	}
	ranking.DownloadWeight = 1
	ranking.FreshnessDecayDays = 0
	if got := ranking.normalized(); got.FreshnessDecayDays != 7.0 {
		t.Fatalf("zero decay = %v, want the default", got.FreshnessDecayDays)
	}
}
