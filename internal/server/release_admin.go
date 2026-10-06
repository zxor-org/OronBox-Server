package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/attestation"
)

var minVersionPattern = regexp.MustCompile(`<!--\s*min:\s*([0-9]+\.[0-9]+\.[0-9]+)\s*-->`)

type githubRelease struct {
	TagName     string `json:"tag_name"`
	Body        string `json:"body"`
	PublishedAt string `json:"published_at"`
}

func stripVersionPrefix(tag string) string {
	return strings.TrimPrefix(strings.TrimSpace(tag), "v")
}

// parseReleaseBody splits the hand-written GitHub release body into zh/en notes and
// extracts the optional `<!-- min: x.y.z -->` marker, which is removed from the notes.
func parseReleaseBody(body string) (zh, en, minimum string) {
	var zhLines, enLines []string
	inEnglish := false
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == "<!-- EN -->" {
			inEnglish = true
			continue
		}
		if m := minVersionPattern.FindStringSubmatch(line); m != nil {
			minimum = m[1]
			continue
		}
		if inEnglish {
			enLines = append(enLines, line)
		} else {
			zhLines = append(zhLines, line)
		}
	}
	return strings.TrimSpace(strings.Join(zhLines, "\n")), strings.TrimSpace(strings.Join(enLines, "\n")), minimum
}

func (a *application) fetchLatestRelease(ctx context.Context) (githubRelease, string, error) {
	url := strings.TrimRight(a.cfg.GitHub.APIURL, "/") + "/repos/zxor-org/oronbox/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return githubRelease{}, "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "OronBox-Server")
	if a.cfg.GitHubReleaseToken != "" {
		req.Header.Set("Authorization", "Bearer "+a.cfg.GitHubReleaseToken)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return githubRelease{}, "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return githubRelease{}, string(raw), fmt.Errorf("github returned %s", resp.Status)
	}
	var rel githubRelease
	if err := json.Unmarshal(raw, &rel); err != nil {
		return githubRelease{}, string(raw), err
	}
	return rel, string(raw), nil
}

// releaseSync manually pulls the latest GitHub release, parses the bilingual body and
// upserts it keyed by version, never deleting older versions.
func (a *application) releaseSync(w http.ResponseWriter, r *http.Request) {
	if a.db == nil {
		jsonError(w, http.StatusServiceUnavailable, "database_unavailable", "database is unavailable", nil)
		return
	}
	rel, raw, err := a.fetchLatestRelease(r.Context())
	if err != nil {
		message := strings.TrimSpace(raw)
		if message == "" {
			message = err.Error()
		}
		jsonError(w, http.StatusBadGateway, "github_fetch_failed", message, nil)
		return
	}
	version := stripVersionPrefix(rel.TagName)
	zh, en, minimum := parseReleaseBody(rel.Body)
	if version == "" || (zh == "" && en == "") {
		jsonError(w, http.StatusUnprocessableEntity, "release_body_empty", "release body has no renderable content", nil)
		return
	}
	published := time.Now().UTC()
	if rel.PublishedAt != "" {
		if t, e := time.Parse(time.RFC3339, rel.PublishedAt); e == nil {
			published = t
		}
	}
	now := time.Now().UTC()
	if _, err := a.db.Pool.Exec(r.Context(),
		`INSERT INTO app_releases(version,minimum_version,notes_zh,notes_en,published_at,synced_at) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT (version) DO UPDATE SET minimum_version=EXCLUDED.minimum_version,notes_zh=EXCLUDED.notes_zh,notes_en=EXCLUDED.notes_en,published_at=EXCLUDED.published_at,synced_at=EXCLUDED.synced_at`,
		version, minimum, zh, en, published, now); err != nil {
		jsonError(w, http.StatusInternalServerError, "release_sync_failed", err.Error(), nil)
		return
	}
	a.writeAudit(r, "release.sync", "success", map[string]any{"version": version})
	jsonResponse(w, http.StatusOK, map[string]any{"tag": rel.TagName, "version": version, "minimum_version": minimum, "published_at": published, "synced_at": now})
}

type revocationEntry struct {
	Fingerprint string `json:"fingerprint"`
	Tag         string `json:"tag"`
	Commit      string `json:"commit"`
	Reason      string `json:"reason"`
	RevokedAt   string `json:"revoked_at"`
}

func (a *application) revocationsGet(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, http.StatusOK, map[string]any{"minimum_version": a.settingString("auth_minimum_version"), "revoked": a.revocationEntries()})
}

func (a *application) revocationsPost(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Tag    string `json:"tag"`
		Commit string `json:"commit"`
		Reason string `json:"reason"`
	}
	if !readJSON(w, r, &in) || strings.TrimSpace(in.Tag) == "" || strings.TrimSpace(in.Commit) == "" {
		jsonError(w, http.StatusBadRequest, "invalid_request", "tag and commit are required", nil)
		return
	}
	key := attestation.DeriveReleaseKey(a.cfg.AttestationMasterKey, in.Tag, in.Commit)
	entry := revocationEntry{Fingerprint: attestation.Fingerprint(key), Tag: in.Tag, Commit: in.Commit, Reason: in.Reason, RevokedAt: time.Now().UTC().Format(time.RFC3339)}
	entries := a.revocationEntries()
	for _, e := range entries {
		if strings.EqualFold(e.Fingerprint, entry.Fingerprint) {
			jsonResponse(w, http.StatusOK, entry)
			return
		}
	}
	entries = append(entries, entry)
	if err := a.saveSetting("auth_revoked_keys", entries); err != nil {
		jsonError(w, http.StatusInternalServerError, "revocation_save_failed", err.Error(), nil)
		return
	}
	a.refreshRevocation()
	a.writeAudit(r, "auth.revoke", "success", map[string]any{"fingerprint": entry.Fingerprint, "tag": in.Tag})
	jsonResponse(w, http.StatusCreated, entry)
}

func (a *application) revocationsDelete(w http.ResponseWriter, r *http.Request) {
	fp := strings.ToLower(strings.TrimSpace(r.PathValue("fingerprint")))
	entries := a.revocationEntries()
	kept := make([]revocationEntry, 0, len(entries))
	for _, e := range entries {
		if !strings.EqualFold(e.Fingerprint, fp) {
			kept = append(kept, e)
		}
	}
	if err := a.saveSetting("auth_revoked_keys", kept); err != nil {
		jsonError(w, http.StatusInternalServerError, "revocation_save_failed", err.Error(), nil)
		return
	}
	a.refreshRevocation()
	a.writeAudit(r, "auth.unrevoke", "success", map[string]any{"fingerprint": fp})
	w.WriteHeader(http.StatusNoContent)
}

func (a *application) minimumVersionPut(w http.ResponseWriter, r *http.Request) {
	var in struct {
		MinimumVersion string `json:"minimum_version"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	value := strings.TrimSpace(in.MinimumVersion)
	if err := a.saveSetting("auth_minimum_version", value); err != nil {
		jsonError(w, http.StatusInternalServerError, "setting_save_failed", err.Error(), nil)
		return
	}
	a.refreshRevocation()
	a.writeAudit(r, "auth.minimum_version", "success", map[string]any{"minimum_version": value})
	jsonResponse(w, http.StatusOK, map[string]string{"minimum_version": value})
}

func (a *application) revocationEntries() []revocationEntry {
	entries := []revocationEntry{}
	if raw, err := a.settingRaw("auth_revoked_keys"); err == nil && len(raw) > 0 {
		_ = json.Unmarshal(raw, &entries)
	}
	return entries
}

func (a *application) settingString(key string) string {
	raw, err := a.settingRaw(key)
	if err != nil || len(raw) == 0 {
		return ""
	}
	var value string
	_ = json.Unmarshal(raw, &value)
	return value
}

func (a *application) settingInt(key string, fallback int) int {
	raw, err := a.settingRaw(key)
	if err != nil || len(raw) == 0 {
		return fallback
	}
	var value int
	if json.Unmarshal(raw, &value) != nil {
		return fallback
	}
	return value
}

func (a *application) saveSetting(key string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = a.db.Pool.Exec(context.Background(), `INSERT INTO server_settings(key,value,updated_at) VALUES($1,$2,now()) ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value,updated_at=now()`, key, raw)
	return err
}

func (a *application) writeAudit(r *http.Request, action, result string, target any) {
	if a.db == nil {
		return
	}
	raw, _ := json.Marshal(target)
	_, _ = a.db.Pool.Exec(r.Context(), `INSERT INTO audit_logs(actor_user_id,action,result,ip,target_data) VALUES(NULL,$1,$2,$3,$4)`, action, result, clientIP(r), raw)
}
