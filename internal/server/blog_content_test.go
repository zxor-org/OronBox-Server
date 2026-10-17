package server

import (
	"testing"

	"github.com/zxor-org/OronBox-Server/internal/config"
)

func TestContentImageReferencesStayOnGiteaAndInsideArticlePaths(t *testing.T) {
	a := &App{cfg: config.Config{DeviceCatalog: config.DeviceCatalogConfig{BaseURL: "https://git.example", Repository: "Org/Content"}}}
	for _, value := range []string{"https://evil.example/a.png", "//evil.example/a.png", "../../../secret.png", "https://user@git.example/a.png", "https://git.example/a.png?token=secret", "javascript:alert(1)"} {
		if _, err := a.giteaImageURL(value, "commit", "articles/post.md"); err == nil {
			t.Errorf("unsafe image accepted: %s", value)
		}
	}
	got, err := a.giteaImageURL("media/photo.webp", "commit", "articles/post.md")
	if err != nil || got != "https://git.example/Org/Content/raw/commit/commit/articles/media/photo.webp" {
		t.Fatal(got, err)
	}
}
