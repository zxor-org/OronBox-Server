package syndication

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

type AppRelease struct {
	Version        string
	MinimumVersion string
	NotesZH        string
	NotesEN        string
	PublishedAt    time.Time
}

var appVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
var minimumVersionPattern = regexp.MustCompile(`(?im)<!--\s*min:\s*([0-9]+\.[0-9]+\.[0-9]+)\s*-->`)

// ParseAppRelease converts the documented GitHub Release format into the
// server's client update record. The release tag is the authoritative version;
// draft and prerelease filtering happens before parsing.
func ParseAppRelease(release GitHubRelease) (AppRelease, error) {
	version := strings.TrimPrefix(strings.TrimSpace(release.TagName), "v")
	if !appVersionPattern.MatchString(version) {
		return AppRelease{}, fmt.Errorf("invalid release tag %q", release.TagName)
	}
	publishedAt, err := time.Parse(time.RFC3339, strings.TrimSpace(release.PublishedAt))
	if err != nil {
		return AppRelease{}, fmt.Errorf("invalid release publication time: %w", err)
	}
	body := strings.TrimSpace(release.Body)
	minimumVersion := ""
	if match := minimumVersionPattern.FindStringSubmatch(body); len(match) == 2 {
		minimumVersion = match[1]
		body = minimumVersionPattern.ReplaceAllString(body, "")
	}
	parts := strings.SplitN(body, "<!-- EN -->", 2)
	notesZH := strings.TrimSpace(parts[0])
	notesEN := ""
	if len(parts) == 2 {
		notesEN = strings.TrimSpace(parts[1])
	}
	return AppRelease{
		Version:        version,
		MinimumVersion: minimumVersion,
		NotesZH:        notesZH,
		NotesEN:        notesEN,
		PublishedAt:    publishedAt,
	}, nil
}
