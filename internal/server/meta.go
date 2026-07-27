package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/zxor-org/OronBox-Server/internal/legal"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

var legalDocuments = map[string]string{
	"terms": "terms", "privacy": "privacy", "resource-publishing": "resource-publishing", "review-rules": "review-rules",
}

func (a *App) handleLegalDocument(w http.ResponseWriter, r *http.Request) {
	name, ok := legalDocuments[r.PathValue("document")]
	if !ok {
		writeJSON(w, http.StatusNotFound, errorBody("document_not_found", "legal document was not found"))
		return
	}
	language := preferredLanguage(r)
	data, err := legal.Documents.ReadFile("docs/" + name + "." + language + ".md")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("document_read_failed", err.Error()))
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Language", language)
	w.Header().Set("Vary", "Accept-Language")
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = w.Write(data)
}

func preferredLanguage(r *http.Request) string {
	language := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("lang")))
	if language == "en" || (language == "" && strings.HasPrefix(strings.ToLower(r.Header.Get("Accept-Language")), "en")) {
		return "en"
	}
	return "zh"
}

func (a *App) handleAppRelease(w http.ResponseWriter, r *http.Request) {
	platform := strings.TrimSpace(r.URL.Query().Get("platform"))
	arch := strings.TrimSpace(r.URL.Query().Get("arch"))
	channel := strings.TrimSpace(r.URL.Query().Get("channel"))
	if channel == "" {
		channel = "stable"
	}
	release, err := a.store.LatestAppRelease(r.Context(), channel, platform, arch)
	if errors.Is(err, store.ErrReleaseNotFound) {
		writeJSON(w, http.StatusNotFound, errorBody("release_not_found", "no release is published for this channel"))
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("release_read_failed", err.Error()))
		return
	}
	asset := release.DownloadURL
	if asset != "" {
		asset = strings.NewReplacer("{platform}", platform, "{arch}", arch, "{channel}", channel, "{version}", release.Version).Replace(asset)
	}
	notes := release.NotesZH
	if preferredLanguage(r) == "en" {
		notes = release.NotesEN
	}
	currentVersion := strings.TrimSpace(r.URL.Query().Get("version"))
	writeJSON(w, http.StatusOK, map[string]any{"latest_version": release.Version, "minimum_version": release.MinimumVersion, "mandatory": currentVersion != "" && release.MinimumVersion != "" && versionLess(currentVersion, release.MinimumVersion), "release_notes": notes, "published_at": release.PublishedAt, "download_url": asset, "source_url": "https://github.com/zxor-org/OronBox/releases/tag/" + release.Version, "channel": channel, "platform": platform, "arch": arch})
}

func versionLess(left, right string) bool {
	left = strings.TrimPrefix(strings.SplitN(left, "+", 2)[0], "v")
	right = strings.TrimPrefix(strings.SplitN(right, "+", 2)[0], "v")
	a, b := strings.Split(left, "."), strings.Split(right, ".")
	for index := 0; index < len(a) || index < len(b); index++ {
		var av, bv int
		if index < len(a) {
			av, _ = strconv.Atoi(a[index])
		}
		if index < len(b) {
			bv, _ = strconv.Atoi(b[index])
		}
		if av != bv {
			return av < bv
		}
	}
	return false
}
