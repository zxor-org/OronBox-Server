package web

import (
	"regexp"
	"strings"
	"testing"
)

var (
	getFormPattern  = regexp.MustCompile(`(?s)<form[^>]*method="get"[^>]*>(.*?)</form>`)
	selectPattern   = regexp.MustCompile(`(?s)<select name="([^"]+)"[^>]*>(.*?)</select>`)
	optionPattern   = regexp.MustCompile(`<option value="([^"]*)"([^>]*)>`)
	inputPattern    = regexp.MustCompile(`<input\b[^>]*>`)
	nameAttrPattern = regexp.MustCompile(`name="([^"]+)"`)
)

// These rules are checked against the template markup rather than rendered
// output, because a render case only ever exercises the one fixture it happens
// to carry and would miss most of the controls on the page.
func TestFilterSelectsEchoTheCurrentChoice(t *testing.T) {
	t.Parallel()
	// A filter control that does not mark its selected option silently resets
	// to the first entry after the page reloads, so the operator sees a filter
	// that disagrees with the results underneath it.
	for name, source := range templateSources(t) {
		for _, form := range getFormPattern.FindAllStringSubmatch(source, -1) {
			for _, field := range selectPattern.FindAllStringSubmatch(form[1], -1) {
				for _, option := range optionPattern.FindAllStringSubmatch(field[2], -1) {
					if option[1] == "" || strings.Contains(option[2], "selected") {
						continue
					}
					t.Errorf("%s: filter select %q does not echo the option %q", name, field[1], option[1])
				}
			}
		}
	}
}

func TestEveryFilterBarSharesTheSameSubmitParadigm(t *testing.T) {
	t.Parallel()
	// Filter bars used to end in whichever button the page author felt like,
	// so the same control sat in a different place with a different label on
	// every list page. They all end in a filter-actions group now.
	for name, source := range templateSources(t) {
		for _, form := range getFormPattern.FindAllStringSubmatch(source, -1) {
			if strings.Contains(form[1], `class="filter-actions"`) {
				continue
			}
			action := "unknown"
			if match := regexp.MustCompile(`action="([^"]*)"`).FindStringSubmatch(form[0]); match != nil {
				action = match[1]
			}
			t.Errorf("%s: the filter bar for %s does not use a filter-actions group", name, action)
		}
	}
}

// Every captioned control is written as <label><span>Caption</span><control>.
// The CSS field primitive keys off that shape, so a label that puts its caption
// as bare text silently opts out of the whole form system and renders with
// browser defaults, which is how the review decision form ended up looking
// unlike every other form in the console.
func TestCommentRowActionsAreNotNestedInsideTheBulkForm(t *testing.T) {
	t.Parallel()
	source, ok := templateSources(t)["community.gohtml"]
	if !ok {
		t.Skip("legacy comment templates have been removed")
	}
	start := strings.Index(source, `action="/admin/comments/bulk"`)
	if start < 0 {
		t.Fatal("comment bulk form is missing")
	}
	end := strings.Index(source[start:], `</form>`)
	if end < 0 {
		t.Fatal("comment bulk form is unclosed")
	}
	if strings.Contains(source[start:start+end], `<form`) {
		t.Fatal("the comment bulk form still nests the per-row action form, so the browser drops the inner form and the row buttons submit the batch")
	}
}

func TestEveryFieldUsesTheSameLabelShape(t *testing.T) {
	t.Parallel()
	bareCaption := regexp.MustCompile(`<label([^>]*)>([^<]+)<(input|select|textarea)`)
	for name, source := range templateSources(t) {
		for _, match := range bareCaption.FindAllStringSubmatch(source, -1) {
			if strings.TrimSpace(match[2]) == "" {
				continue
			}
			t.Errorf("%s: the field %q puts its caption outside a <span>, so it opts out of the form system", name, strings.TrimSpace(match[2]))
		}
	}
}

// Unmapped states fall through statusLabel as raw English, which is how the
// comment console ended up showing "hidden" next to Chinese labels. Every
// state a template can render has to be named.
func TestEveryModerationStateHasAChineseLabel(t *testing.T) {
	t.Parallel()
	for _, state := range []string{
		"visible", "hidden", "deleted", "review",
		"pending", "approved", "rejected", "superseded",
		"listed", "delisted", "draft", "submitted",
	} {
		if label := statusLabel(state); label == state {
			t.Errorf("the state %q renders as raw English", state)
		}
	}
	for _, verdict := range []string{"approve", "hide", "review", ""} {
		if label := moderationVerdict(verdict); label == verdict {
			t.Errorf("the verdict %q renders as raw English", verdict)
		}
	}
}

func TestFilterInputsEchoTheCurrentValue(t *testing.T) {
	t.Parallel()
	for name, source := range templateSources(t) {
		for _, form := range getFormPattern.FindAllStringSubmatch(source, -1) {
			for _, tag := range inputPattern.FindAllString(form[1], -1) {
				switch {
				case strings.Contains(tag, `type="hidden"`),
					strings.Contains(tag, `type="checkbox"`),
					strings.Contains(tag, `type="submit"`),
					!strings.Contains(tag, `name="`),
					strings.Contains(tag, `value="`):
					continue
				}
				t.Errorf("%s: filter input %q does not echo its value", name, nameAttrPattern.FindStringSubmatch(tag)[1])
			}
		}
	}
}
