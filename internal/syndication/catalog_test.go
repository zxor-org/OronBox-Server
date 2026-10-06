package syndication

import (
	"strings"
	"testing"
)

func TestUpsertCatalogRow(t *testing.T) {
	header := CatalogCSVHeader + "\n"
	first := CatalogRow{ID: "com.a", Name: "A", Restype: "quick_app", Author: "Alice", Repo: "OronBoxBot/oronbox-resource-com-a", RepoCommitHash: "abc1234", Icon: "media/icon.webp", Devices: "xmb9p"}
	out, err := UpsertCatalogRow("", first)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, header) || strings.Count(out, "com.a") != 1 {
		t.Fatalf("insert failed:\n%s", out)
	}

	second := CatalogRow{ID: "com.b", Name: "B", Restype: "watchface", Author: "Bob", Repo: "OronBoxBot/oronbox-resource-com-b", Devices: "xmrw6"}
	out, err = UpsertCatalogRow(out, second)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "com.a") || !strings.Contains(out, "com.b") {
		t.Fatalf("append failed:\n%s", out)
	}

	updated := first
	updated.Name = "A2"
	out, err = UpsertCatalogRow(out, updated)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(out, "com.a") != 1 || !strings.Contains(out, "A2") {
		t.Fatalf("update failed:\n%s", out)
	}
}
