package server

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/zxor-org/OronBox-Server/internal/httpx"
	"github.com/zxor-org/OronBox-Server/internal/legal"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

var legalDocuments = map[string]string{
	"terms": "terms", "privacy": "privacy", "resource-publishing": "resource-publishing", "review-rules": "review-rules",
}

func (a *App) handleLegalDocument(w http.ResponseWriter, r *http.Request) {
	name, ok := legalDocuments[canonicalLegalID(r.PathValue("document"))]
	if !ok {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.ErrorBody("document_not_found", "legal document was not found"))
		return
	}
	a.content.mu.RLock()
	online := a.content.legal
	a.content.mu.RUnlock()
	for _, doc := range online.Documents {
		if doc.ID == name && doc.Revision >= bundledLegalRevision(name) {
			if requested := r.URL.Query().Get("revision"); requested != "" && requested != strconv.Itoa(doc.Revision) {
				httpx.WriteJSON(w, 409, httpx.ErrorBody("legal_revision_changed", "reload legal manifest"))
				return
			}
			lang := preferredLanguage(r)
			body := doc.Bodies[lang]
			if body == "" {
				httpx.WriteJSON(w, 503, httpx.ErrorBody("legal_language_unavailable", "document translation unavailable"))
				return
			}
			w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
			w.Header().Set("X-Legal-Revision", strconv.Itoa(doc.Revision))
			w.Header().Set("ETag", fmt.Sprintf("\"%s-%d-%s\"", doc.ID, doc.Revision, lang))
			io.WriteString(w, body)
			return
		}
	}
	revision := bundledLegalRevision(name)
	if r.URL.Query().Get("revision") != "" && r.URL.Query().Get("revision") != strconv.Itoa(revision) {
		httpx.WriteJSON(w, 503, httpx.ErrorBody("legal_unavailable", "requested legal revision unavailable"))
		return
	}
	w.Header().Set("X-Legal-Revision", strconv.Itoa(revision))
	language := preferredLanguage(r)
	data, err := legal.Documents.ReadFile("docs/" + name + "." + language + ".md")
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("document_read_failed", err.Error()))
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Language", language)
	w.Header().Set("Vary", "Accept-Language")
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = w.Write(data)
}

func bundledLegalRevision(name string) int {
	if name == "terms" || name == "privacy" {
		return 2
	}
	return 1
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
		httpx.WriteJSON(w, http.StatusNotFound, httpx.ErrorBody("release_not_found", "no release is published for this channel"))
		return
	}
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("release_read_failed", err.Error()))
		return
	}
	notes := release.NotesZH
	if preferredLanguage(r) == "en" {
		notes = release.NotesEN
	}
	if notes == "" {
		notes = release.NotesEN
	}
	currentVersion := strings.TrimSpace(r.URL.Query().Get("version"))
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"latest_version": release.Version, "minimum_version": release.MinimumVersion, "mandatory": currentVersion != "" && release.MinimumVersion != "" && versionLess(currentVersion, release.MinimumVersion), "release_notes": notes, "published_at": release.PublishedAt, "tag": release.Tag, "channel": channel, "platform": platform, "arch": arch})
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
