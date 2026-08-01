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

// TestHomeAndBlogLifecycle exercises the blog, banner, section and card CRUD
// plus ordering swaps against a real schema.
func TestHomeAndBlogLifecycle(t *testing.T) {
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
	t.Cleanup(func() {
		_, _ = adminDB.ExecContext(context.Background(), `DROP DATABASE `+databaseName)
	})
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
	s := store.New(db)

	post := store.BlogPost{Slug: "hello-world", Type: "announcement", Title: "Hello", Subtitle: "first", Author: "team", Body: "body"}
	if err := s.UpsertBlogPost(ctx, post); err != nil {
		t.Fatal(err)
	}
	if posts, err := s.ListPublishedBlogPosts(ctx); err != nil || len(posts) != 0 {
		t.Fatalf("unpublished post must not be listed: %v %d", err, len(posts))
	}
	if err := s.SetBlogPostPublished(ctx, post.Slug, true); err != nil {
		t.Fatal(err)
	}
	posts, err := s.ListPublishedBlogPosts(ctx)
	if err != nil || len(posts) != 1 || posts[0].PublishedAt == nil {
		t.Fatalf("published post missing: %v %+v", err, posts)
	}
	loaded, err := s.BlogPost(ctx, post.Slug)
	if err != nil || loaded.Title != "Hello" || !loaded.Published {
		t.Fatalf("load post: %v %+v", err, loaded)
	}

	bannerA, bannerB := uuid.NewString(), uuid.NewString()
	if err := s.CreateHomeBanner(ctx, store.HomeBanner{ID: bannerA, Type: "link", Title: "A", LinkURL: "https://example.com", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateHomeBanner(ctx, store.HomeBanner{ID: bannerB, Type: "blog", Title: "B", BlogSlug: post.Slug, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	banners, err := s.ListHomeBanners(ctx, true)
	if err != nil || len(banners) != 2 || banners[0].ID != bannerA {
		t.Fatalf("list banners: %v %+v", err, banners)
	}
	if err := s.MoveHomeBanner(ctx, bannerB, -1); err != nil {
		t.Fatal(err)
	}
	banners, err = s.ListHomeBanners(ctx, true)
	if err != nil || banners[0].ID != bannerB {
		t.Fatalf("move banner: %v %+v", err, banners)
	}
	if err := s.UpdateHomeBanner(ctx, store.HomeBanner{ID: bannerA, Type: "link", Title: "A2", LinkURL: "https://example.com/2"}); err != nil {
		t.Fatal(err)
	}
	banners, err = s.ListHomeBanners(ctx, true)
	if err != nil || len(banners) != 1 {
		t.Fatalf("disabled banner must be hidden: %v %+v", err, banners)
	}

	if err := s.CreateHomeSection(ctx, store.HomeSection{ID: "editors-pick", Name: "编辑精选", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	cardA, cardB := uuid.NewString(), uuid.NewString()
	if err := s.CreateHomeSectionCard(ctx, store.HomeSectionCard{ID: cardA, SectionID: "editors-pick", Type: "blog", BlogSlug: post.Slug}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateHomeSectionCard(ctx, store.HomeSectionCard{ID: cardB, SectionID: "editors-pick", Type: "blog", BlogSlug: post.Slug}); err != nil {
		t.Fatal(err)
	}
	cards, err := s.ListHomeSectionCards(ctx, "editors-pick")
	if err != nil || len(cards) != 2 || cards[0].ID != cardA {
		t.Fatalf("list cards: %v %+v", err, cards)
	}
	if err := s.MoveHomeSectionCard(ctx, cardB, "editors-pick", -1); err != nil {
		t.Fatal(err)
	}
	cards, err = s.ListHomeSectionCards(ctx, "editors-pick")
	if err != nil || cards[0].ID != cardB {
		t.Fatalf("move card: %v %+v", err, cards)
	}
	if err := s.DeleteHomeSectionCard(ctx, cardA); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteHomeSection(ctx, "editors-pick"); err != nil {
		t.Fatal(err)
	}
	if cards, err = s.ListHomeSectionCards(ctx, "editors-pick"); err != nil || len(cards) != 0 {
		t.Fatalf("section delete must cascade cards: %v %+v", err, cards)
	}
	if err := s.DeleteHomeBanner(ctx, bannerA); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteBlogPost(ctx, post.Slug); err != nil {
		t.Fatal(err)
	}
}
