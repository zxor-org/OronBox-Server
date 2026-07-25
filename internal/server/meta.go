package server

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/zxor-org/OronBox-Server/internal/legal"
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
	asset := a.cfg.Releases.DownloadURL
	if asset != "" {
		asset = strings.NewReplacer("{platform}", platform, "{arch}", arch, "{channel}", channel, "{version}", a.cfg.Releases.LatestVersion).Replace(asset)
	}
	notes := a.cfg.Releases.ReleaseNotesZH
	if preferredLanguage(r) == "en" {
		notes = a.cfg.Releases.ReleaseNotesEN
	}
	writeJSON(w, http.StatusOK, map[string]any{"latest_version": a.cfg.Releases.LatestVersion, "minimum_version": a.cfg.Releases.MinimumVersion, "mandatory": a.cfg.Releases.MinimumVersion != "" && a.cfg.Releases.MinimumVersion == a.cfg.Releases.LatestVersion, "release_notes": notes, "published_at": a.cfg.Releases.PublishedAt, "download_url": asset, "source_url": fmt.Sprintf("https://github.com/zxor-org/OronBox/releases/tag/%s", a.cfg.Releases.LatestVersion), "channel": channel, "platform": platform, "arch": arch})
}
