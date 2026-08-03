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
