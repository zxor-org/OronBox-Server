package store

import "testing"

func TestAdminDeviceQueryNormalization(t *testing.T) {
	t.Parallel()

	query := (AdminDeviceQuery{
		Search:   "  Smart Band  ",
		Platform: "unsupported",
		Vendor:   "  Xiaomi  ",
		State:    "unknown",
		Sort:     "random",
		Page:     -2,
		PerPage:  1000,
	}).normalized()

	if query.Search != "Smart Band" {
		t.Fatalf("search = %q, want trimmed value", query.Search)
	}
	if query.Platform != "" {
		t.Fatalf("platform = %q, want invalid platform removed", query.Platform)
	}
	if query.Vendor != "xiaomi" {
		t.Fatalf("vendor = %q, want normalized lowercase value", query.Vendor)
	}
	if query.State != "" || query.Sort != "name" || query.Page != 1 || query.PerPage != 100 {
		t.Fatalf("pagination/sort were not normalized: %#v", query)
	}
}

func TestAdminDeviceQueryKeepsSupportedFiltersAndSorts(t *testing.T) {
	t.Parallel()

	for _, sort := range []string{"name", "codename", "platform", "vendor", "resources_desc", "artifacts_desc"} {
		query := (AdminDeviceQuery{
			Platform: "zepp_os",
			Vendor:   " amazfit ",
			State:    "disabled",
			Sort:     sort,
			Page:     3,
			PerPage:  50,
		}).normalized()
		if query.Platform != "zepp_os" || query.Vendor != "amazfit" || query.State != "disabled" || query.Sort != sort || query.Page != 3 || query.PerPage != 50 {
			t.Fatalf("valid query changed for sort %q: %#v", sort, query)
		}
	}
}

func TestAdminDeviceInputNormalization(t *testing.T) {
	t.Parallel()
	input, err := (AdminDeviceInput{DisplayName: " Band 11 ", Codename: " P66 ", Platform: " VELA_OS ", Vendor: " Xiaomi ", AstroBoxID: " xmb11 ", Enabled: true}).normalized()
	if err != nil {
		t.Fatal(err)
	}
	if input.DisplayName != "Band 11" || input.Codename != "p66" || input.Platform != "vela_os" || input.Vendor != "xiaomi" || input.AstroBoxID != "xmb11" || !input.Enabled {
		t.Fatalf("input was not normalized: %#v", input)
	}
	for _, invalid := range []AdminDeviceInput{
		{DisplayName: "", Codename: "p66", Platform: "vela_os"},
		{DisplayName: "Band", Codename: "bad code", Platform: "vela_os"},
		{DisplayName: "Band", Codename: "p66", Platform: "android"},
	} {
		if _, err := invalid.normalized(); err == nil {
			t.Fatalf("invalid input accepted: %#v", invalid)
		}
	}
}

func TestAdminDeviceDTOCarriesCatalogAndBindingCounts(t *testing.T) {
	t.Parallel()

	item := AdminDeviceItem{
		ID:            "device-id",
		DisplayName:   "Xiaomi Smart Band 8 Pro",
		Codename:      "m67",
		Platform:      "vela_os",
		Vendor:        "xiaomi",
		AstroBoxID:    "m67",
		ResourceCount: 4,
		ArtifactCount: 7,
	}
	if item.DisplayName == "" || item.Codename == "" || item.Platform == "" || item.Vendor == "" || item.AstroBoxID == "" {
		t.Fatalf("catalog fields are incomplete: %#v", item)
	}
	if item.ResourceCount != 4 || item.ArtifactCount != 7 {
		t.Fatalf("binding counts are incomplete: %#v", item)
	}
}
