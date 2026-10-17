package bandbbs

import (
	"strings"
	"testing"
)

func TestMarkdownBBCodeEscapesMarkupAndUnsafeURLs(t *testing.T) {
	got := MarkdownToBBCode("# 标题\n**粗体** *斜体*\n- 项目\n`[IMG]x[/IMG]`\n[x](https://example.com)\n![图](https://example.com/p.png)\n[恶意](javascript:alert(1))")
	for _, want := range []string{"[B]标题[/B]", "[B]粗体[/B]", "[I]斜体[/I]", "[LIST][*]项目[/LIST]", "[CODE]&#91;IMG&#93;x&#91;/IMG&#93;[/CODE]", "[URL=https://example.com]x[/URL]", "[IMG]https://example.com/p.png[/IMG]"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s in %s", want, got)
		}
	}
	if strings.Contains(got, "[URL=javascript:") {
		t.Fatal("unsafe URL")
	}
	if safeBBCodeURL("https://example.com/a]b") {
		t.Fatal("bracket injection accepted")
	}
}
