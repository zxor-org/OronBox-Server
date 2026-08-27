package web

import (
	"regexp"
	"strings"
	"testing"
)

func templateSources(t *testing.T) map[string]string {
	t.Helper()
	entries, err := templateFS.ReadDir("templates")
	if err != nil {
		t.Fatal(err)
	}
	sources := map[string]string{}
	for _, entry := range entries {
		source, err := templateFS.ReadFile("templates/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		sources[entry.Name()] = string(source)
	}
	return sources
}

func TestEveryReferencedIconExistsInTheSprite(t *testing.T) {
	// A missing symbol renders as an invisible gap rather than an error, so
	// the mismatch has to be caught here instead of by someone noticing a
	// blank button.
	reference := regexp.MustCompile(`<use href="#i-([a-z_0-9]+)"`)
	for name, source := range templateSources(t) {
		for _, match := range reference.FindAllStringSubmatch(source, -1) {
			if !strings.Contains(iconSprite, `id="i-`+match[1]+`"`) {
				t.Errorf("%s references icon %q which is not in the sprite", name, match[1])
			}
		}
	}
}

func TestDynamicIconNamesAreAlsoInTheSprite(t *testing.T) {
	// Templates that pick an icon with {{if}} produce names the static
	// reference check cannot see, so the branches are listed explicitly.
	for _, name := range []string{"error", "open_in_new", "check_circle", "deployed_code", "article"} {
		if !strings.Contains(iconSprite, `id="i-`+name+`"`) {
			t.Errorf("conditionally rendered icon %q is not in the sprite", name)
		}
	}
}

func TestAdminConsoleLoadsNoThirdPartyAssets(t *testing.T) {
	// The console runs under a strict CSP and should keep working on a network
	// that cannot reach a font CDN, so every asset has to be first-party.
	// Only elements that fetch a subresource count. An <a href> pointing at
	// GitHub is a navigation the operator chose, not something the page loads.
	loaders := []*regexp.Regexp{
		regexp.MustCompile(`\ssrc="(https?:)?//[^"]+"`),
		regexp.MustCompile(`<link[^>]*\shref="(https?:)?//[^"]+"`),
		regexp.MustCompile(`<use[^>]*\shref="(https?:)?//[^"]+"`),
		regexp.MustCompile(`@import\s`),
		regexp.MustCompile(`<link[^>]*rel="preconnect"`),
	}
	for name, source := range templateSources(t) {
		for _, loader := range loaders {
			for _, match := range loader.FindAllString(source, -1) {
				t.Errorf("%s loads a third-party asset: %s", name, strings.TrimSpace(match))
			}
		}
	}
}

func TestNoTemplateStillUsesTheIconFont(t *testing.T) {
	for name, source := range templateSources(t) {
		if strings.Contains(source, "material-symbols-outlined") {
			t.Errorf("%s still renders an icon through the webfont", name)
		}
	}
	if strings.Contains(AdminJS, "material-symbols-outlined") {
		t.Error("admin JS still creates icons through the webfont")
	}
}
