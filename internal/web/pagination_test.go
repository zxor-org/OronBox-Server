package web

import (
	"net/url"
	"strings"
	"testing"
)

func TestNewPaginationPreservesFiltersAndBuildsPageWindow(t *testing.T) {
	t.Parallel()
	pager := NewPagination("/admin/resources", url.Values{
		"q": {"watch face"}, "status": {"pending"}, "page": {"99"}, "per_page": {"10"},
	}, 5, 10, 123)

	if pager.From != 41 || pager.To != 50 || pager.TotalPages != 13 {
		t.Fatalf("pagination range = %d-%d/%d pages, want 41-50/13", pager.From, pager.To, pager.TotalPages)
	}
	wantPages := []int{3, 4, 5, 6, 7}
	if len(pager.Pages) != len(wantPages) {
		t.Fatalf("page window = %#v, want %#v", pager.Pages, wantPages)
	}
	for index := range wantPages {
		if pager.Pages[index] != wantPages[index] {
			t.Fatalf("page window = %#v, want %#v", pager.Pages, wantPages)
		}
	}
	pageURL := pager.URL(6)
	for _, value := range []string{"/admin/resources?", "page=6", "per_page=10", "q=watch+face", "status=pending"} {
		if !strings.Contains(pageURL, value) {
			t.Fatalf("page URL %q is missing %q", pageURL, value)
		}
	}
	if got := pager.PerPageURL(50); !strings.Contains(got, "page=1") || !strings.Contains(got, "per_page=50") || !strings.Contains(got, "status=pending") {
		t.Fatalf("per-page URL did not preserve filters: %q", got)
	}
}

func TestNewPaginationClampsInvalidPageAndHandlesEmptyResults(t *testing.T) {
	t.Parallel()
	empty := NewPagination("/admin/audit", nil, 9, 0, 0)
	if empty.Page != 1 || empty.PerPage != 25 || empty.From != 0 || empty.To != 0 || empty.TotalPages != 0 {
		t.Fatalf("empty pagination = %#v", empty)
	}

	last := NewPagination("/admin/audit", nil, 99, 25, 51)
	if last.Page != 3 || last.From != 51 || last.To != 51 {
		t.Fatalf("clamped pagination = %#v", last)
	}
}
