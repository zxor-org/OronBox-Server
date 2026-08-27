package web

import (
	"regexp"
	"strings"
	"testing"
)

func TestDesignSystemDefinesEveryTokenGroup(t *testing.T) {
	t.Parallel()
	// Shape, type, motion, elevation and state are the five scales the
	// components below are allowed to reach for. If one disappears the rules
	// that consume it silently fall back to nothing.
	for _, token := range []string{
		"--shape-xs:", "--shape-sm:", "--shape-md:", "--shape-lg:", "--shape-xl:", "--shape-full:",
		"--type-display:", "--type-headline:", "--type-title:", "--type-body:", "--type-label:",
		"--ease-emphasized:", "--ease-standard:", "--duration-short:", "--duration-medium:",
		"--elevation-1:", "--elevation-2:", "--elevation-3:",
		"--state-hover:", "--state-focus:", "--state-pressed:",
		"--font-sans:", "--font-mono:",
	} {
		if !strings.Contains(CSS, token) {
			t.Errorf("design token %q is not defined", token)
		}
	}
}

func TestEveryReferencedTokenIsDefined(t *testing.T) {
	t.Parallel()
	// A typo in var(--shape-large) produces no error, just an unstyled corner,
	// so the references are checked against the definitions.
	defined := map[string]bool{}
	for _, match := range regexp.MustCompile(`(--[a-z0-9-]+)\s*:`).FindAllStringSubmatch(CSS, -1) {
		defined[match[1]] = true
	}
	seen := map[string]bool{}
	for _, match := range regexp.MustCompile(`var\((--[a-z0-9-]+)`).FindAllStringSubmatch(CSS, -1) {
		name := match[1]
		if defined[name] || seen[name] {
			continue
		}
		seen[name] = true
		t.Errorf("CSS uses %q which is never defined", name)
	}
}

func TestInteractiveSurfacesCarryAStateLayer(t *testing.T) {
	t.Parallel()
	// Material's hover, focus and press feedback is a state layer rather than a
	// per-component colour, which is what keeps the console consistent.
	for _, selector := range []string{
		".filled-button::before", ".outlined-button::before",
		".icon-button::before", ".nav-link::before",
	} {
		if !strings.Contains(CSS, selector) {
			t.Errorf("%s has no state layer", selector)
		}
	}
	if !strings.Contains(CSS, "opacity: var(--state-hover)") {
		t.Error("the state layer does not use the hover opacity token")
	}
	if !strings.Contains(CSS, "opacity: var(--state-pressed)") {
		t.Error("the state layer does not use the pressed opacity token")
	}
}

func TestNoComponentHardcodesAColour(t *testing.T) {
	t.Parallel()
	// Colour belongs in the token block. A literal hex further down means one
	// theme will get it wrong, which is exactly how the console drifted before.
	tokenBlockEnd := strings.Index(CSS, "* { box-sizing: border-box; }")
	if tokenBlockEnd < 0 {
		t.Fatal("cannot locate the end of the token block")
	}
	components := CSS[tokenBlockEnd:]
	for _, match := range regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b`).FindAllString(components, -1) {
		t.Errorf("a component rule hardcodes the colour %s instead of using a token", match)
	}
}
