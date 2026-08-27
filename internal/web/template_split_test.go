package web

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

// partials are rendered through another template rather than by a route, so
// they have no standalone case in renderableTemplateCases.
var partials = map[string]bool{
	"csrf": true, "head": true, "tail": true, "admin_nav": true,
	"admin_open": true, "admin_close": true, "page_header": true,
	"pagination": true, "empty_state": true, "review_history": true,
	"events_table": true, "clients_table": true,
}

func definedTemplateNames(t *testing.T) []string {
	t.Helper()
	pattern := regexp.MustCompile(`\{\{define "([a-z_0-9]+)"\}\}`)
	entries, err := templateFS.ReadDir("templates")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("no template group files are embedded")
	}
	var names []string
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".gohtml") {
			continue
		}
		source, err := templateFS.ReadFile("templates/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range pattern.FindAllStringSubmatch(string(source), -1) {
			names = append(names, match[1])
		}
	}
	sort.Strings(names)
	return names
}

func TestEveryTemplateIsDefinedExactlyOnce(t *testing.T) {
	// Two files defining the same name silently overwrite each other, so a
	// page could start rendering another page's markup after a bad split.
	seen := map[string]bool{}
	for _, name := range definedTemplateNames(t) {
		if seen[name] {
			t.Errorf("template %q is defined in more than one group file", name)
		}
		seen[name] = true
	}
}

func TestEveryPageTemplateHasARenderCase(t *testing.T) {
	// Guards the split: a page that lost its definition, or one added without
	// a render case, would otherwise only fail in production.
	cases := renderableTemplateCases()
	for _, name := range definedTemplateNames(t) {
		if partials[name] {
			continue
		}
		if _, ok := cases[name]; !ok {
			t.Errorf("template %q has no render case, add one to renderableTemplateCases", name)
		}
	}
}

func TestRenderCasesOnlyNamePresentTemplates(t *testing.T) {
	defined := map[string]bool{}
	for _, name := range definedTemplateNames(t) {
		defined[name] = true
	}
	for name := range renderableTemplateCases() {
		if !defined[name] {
			t.Errorf("render case %q names a template that no longer exists", name)
		}
	}
}

func TestLegacyReviewTemplatesAreGone(t *testing.T) {
	for _, name := range definedTemplateNames(t) {
		if strings.HasSuffix(name, "_legacy") {
			t.Errorf("legacy template %q is still defined", name)
		}
	}
}
