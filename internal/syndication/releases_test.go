package syndication

import "testing"

func TestParseAppRelease(t *testing.T) {
	release, err := ParseAppRelease(GitHubRelease{
		TagName:     "v1.2.3",
		Body:        "修复蓝牙同步\n\n<!-- min: 1.1.0 -->\n<!-- EN -->\nFix Bluetooth sync",
		PublishedAt: "2026-10-12T08:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if release.Version != "1.2.3" || release.MinimumVersion != "1.1.0" || release.NotesZH != "修复蓝牙同步" || release.NotesEN != "Fix Bluetooth sync" {
		t.Fatalf("unexpected release: %#v", release)
	}
}

func TestParseAppReleaseRejectsInvalidTag(t *testing.T) {
	if _, err := ParseAppRelease(GitHubRelease{TagName: "latest", PublishedAt: "2026-10-12T08:00:00Z"}); err == nil {
		t.Fatal("invalid release tag accepted")
	}
}
