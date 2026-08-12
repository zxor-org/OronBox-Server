package bandbbs

import (
	"strings"
	"testing"
)

func TestTagLineFitsBandBBSCharacterLimit(t *testing.T) {
	value := truncateRunes(strings.Repeat("测", 101), 100)
	if got := len([]rune(value)); got != 100 {
		t.Fatalf("tag line length = %d, want 100", got)
	}
	if got := truncateRunes("short", 100); got != "short" {
		t.Fatalf("short tag line = %q", got)
	}
}

func TestFindMatchingVersionRequiresVersionAndFilename(t *testing.T) {
	versions := []versionSummary{
		{ID: 11, VersionString: "1.0", Files: []struct {
			Filename string `json:"filename"`
			Size     int64  `json:"size"`
		}{{Filename: "old.rpk", Size: 10}}},
		{ID: 12, VersionString: "1.0", Files: []struct {
			Filename string `json:"filename"`
			Size     int64  `json:"size"`
		}{{Filename: "new.rpk", Size: 20}}},
	}
	if got := findMatchingVersion(versions, "1.0", "new.rpk", 20); got == nil || got.ID != 12 {
		t.Fatalf("matched version = %#v, want version 12", got)
	}
	if got := findMatchingVersion(versions, "1.0", "missing.rpk", 20); got != nil {
		t.Fatalf("matched version = %#v, want no match", got)
	}
	if got := findMatchingVersion(versions, "", "new.rpk", 20); got != nil {
		t.Fatalf("matched version = %#v, want no match without version string", got)
	}
}

func TestFindMatchingUpdateRequiresExactContent(t *testing.T) {
	updates := []updateSummary{
		{ID: 21, Title: "更新", Message: "修复问题"},
	}
	if got := findMatchingUpdate(updates, "更新", "修复问题"); got == nil || got.ID != 21 {
		t.Fatalf("matched update = %#v, want update 21", got)
	}
	if got := findMatchingUpdate(updates, "更新", "其他内容"); got != nil {
		t.Fatalf("matched update = %#v, want no match", got)
	}
}
