package store_test

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

func TestRankingWeightsRoundTrip(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	adminDB, err := store.Open(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer adminDB.Close()
	databaseName := "testdb_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := adminDB.ExecContext(ctx, `CREATE DATABASE `+databaseName); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = adminDB.ExecContext(context.Background(), `DROP DATABASE `+databaseName) })
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Path = "/" + databaseName
	db, err := store.Open(parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	adminStore := store.New(db)
	empty, err := adminStore.RankingWeights(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if empty.CoinExtraWeight != 0 || empty.JitterBase != 0 {
		t.Fatalf("fresh database already carries ranking overrides: %#v", empty)
	}

	want := store.RankingWeights{CoinExtraWeight: 0.5, DownloadWeight: 0.25, FreshnessAmplitude: 4, FreshnessDecayDays: 14, FeaturedBoost: 2, JitterBase: 0.25}
	if err := adminStore.SaveRankingWeights(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err := adminStore.RankingWeights(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("ranking round trip = %#v, want %#v", got, want)
	}

	bad := want
	bad.FeaturedBoost = -1
	if err := adminStore.SaveRankingWeights(ctx, bad); err == nil {
		t.Fatal("a non-positive ranking weight was accepted")
	}
}
