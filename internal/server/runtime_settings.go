package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/attestation"
	"github.com/zxor-org/OronBox-Server/internal/httpx"
	"github.com/zxor-org/OronBox-Server/internal/moderation"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

type buildPolicy struct {
	MinimumVersion string            `json:"minimum_version"`
	Revoked        []buildRevocation `json:"revoked"`
}

type buildRevocation struct {
	Fingerprint string `json:"fingerprint"`
	Tag         string `json:"tag"`
	Commit      string `json:"commit"`
	Reason      string `json:"reason"`
}

type runtimeState struct {
	mu       sync.RWMutex
	updateMu sync.Mutex
}

var policyVersion = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+(?:[-+][A-Za-z0-9.\-]+)?$`)

func (a *App) defaultBuildPolicy() buildPolicy {
	p := buildPolicy{MinimumVersion: a.cfg.MinimumClientVersion, Revoked: []buildRevocation{}}
	for _, fp := range a.cfg.RevokedClientKeys {
		p.Revoked = append(p.Revoked, buildRevocation{Fingerprint: fp})
	}
	return p
}

func (a *App) applyBuildPolicy(p buildPolicy) {
	fps := []string{}
	for _, r := range p.Revoked {
		fps = append(fps, r.Fingerprint)
	}
	a.runtime.mu.Lock()
	a.attestationOptions.MinimumVersion = p.MinimumVersion
	a.attestationOptions.Revocation = attestation.NewRevocation(fps)
	a.runtime.mu.Unlock()
}

func (a *App) handleBuildPolicy(w http.ResponseWriter, r *http.Request) {
	a.runtime.updateMu.Lock()
	defer a.runtime.updateMu.Unlock()
	p := a.defaultBuildPolicy()
	raw, err := a.store.Setting(r.Context(), "policy.build", "")
	if err != nil {
		httpx.WriteJSON(w, 500, httpx.ErrorBody("settings_failed", "could not read build policy"))
		return
	}
	if raw != "" && json.Unmarshal([]byte(raw), &p) != nil {
		httpx.WriteJSON(w, 500, httpx.ErrorBody("settings_failed", "stored build policy invalid"))
		return
	}
	if r.Method == http.MethodGet {
		httpx.WriteJSON(w, 200, p)
		return
	}
	var in struct {
		Tag            string `json:"tag"`
		Commit         string `json:"commit"`
		Reason         string `json:"reason"`
		MinimumVersion string `json:"minimum_version"`
	}
	if r.Method != http.MethodDelete {
		if err = httpx.DecodeJSON(r, &in); err != nil {
			httpx.WriteJSON(w, 400, httpx.ErrorBody("invalid_request", "invalid build policy"))
			return
		}
	}
	err = a.store.UpdateSetting(r.Context(), "policy.build", func(old string) (string, error) {
		p = a.defaultBuildPolicy()
		if old != "" {
			if err := json.Unmarshal([]byte(old), &p); err != nil {
				return "", err
			}
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "minimum-version"):
			if in.MinimumVersion != "" && !policyVersion.MatchString(in.MinimumVersion) {
				return "", fmt.Errorf("invalid minimum version")
			}
			p.MinimumVersion = in.MinimumVersion
		case r.Method == http.MethodDelete:
			fp := r.PathValue("fingerprint")
			kept := p.Revoked[:0]
			for _, v := range p.Revoked {
				if v.Fingerprint != fp {
					kept = append(kept, v)
				}
			}
			p.Revoked = kept
		default:
			if !policyVersion.MatchString(in.Tag) || !regexp.MustCompile(`^[a-fA-F0-9]{7,64}$`).MatchString(in.Commit) || len(in.Reason) > 1000 {
				return "", fmt.Errorf("invalid build identity")
			}
			fp := attestation.Fingerprint(attestation.DeriveReleaseKey(a.cfg.AttestationMasterKey, in.Tag, in.Commit))
			for _, v := range p.Revoked {
				if v.Fingerprint == fp {
					return store.EncodeSetting(p)
				}
			}
			p.Revoked = append(p.Revoked, buildRevocation{Fingerprint: fp, Tag: in.Tag, Commit: in.Commit, Reason: in.Reason})
		}
		return store.EncodeSetting(p)
	})
	if err != nil {
		httpx.WriteJSON(w, 400, httpx.ErrorBody("invalid_policy", err.Error()))
		return
	}
	a.applyBuildPolicy(p)
	a.audit(r, "policy.build.update", "success", "build policy updated")
	httpx.WriteJSON(w, 200, p)
}

type aiPolicy struct {
	Enabled       bool   `json:"enabled"`
	PrimaryURL    string `json:"primary_url"`
	PrimaryModel  string `json:"primary_model"`
	PrimaryKey    string `json:"primary_key,omitempty"`
	FallbackURL   string `json:"fallback_url"`
	FallbackModel string `json:"fallback_model"`
	FallbackKey   string `json:"fallback_key,omitempty"`
	Prompt        string `json:"prompt"`
}

func (a *App) applyAIPolicy(p aiPolicy) error {
	primary, fallback := "", ""
	var err error
	if p.PrimaryKey != "" {
		var plain []byte
		if err = json.Unmarshal([]byte(p.PrimaryKey), &plain); err != nil {
			return err
		}
		primary, err = a.secrets.Decrypt(plain)
		if err != nil {
			return err
		}
	}
	if p.FallbackKey != "" {
		var raw []byte
		if err = json.Unmarshal([]byte(p.FallbackKey), &raw); err != nil {
			return err
		}
		fallback, err = a.secrets.Decrypt(raw)
		if err != nil {
			return err
		}
	}
	var service *moderation.Service
	if p.Enabled {
		service = moderation.New(moderation.Endpoint{Name: "primary", BaseURL: p.PrimaryURL, Model: p.PrimaryModel, APIKey: primary}, moderation.Endpoint{Name: "fallback", BaseURL: p.FallbackURL, Model: p.FallbackModel, APIKey: fallback}, a.cfg.Moderation.Timeout)
	}
	a.runtime.mu.Lock()
	a.moderation = service
	a.runtime.mu.Unlock()
	return nil
}

func (a *App) handleAIPolicy(w http.ResponseWriter, r *http.Request) {
	a.runtime.updateMu.Lock()
	defer a.runtime.updateMu.Unlock()
	raw, err := a.store.Setting(r.Context(), "policy.ai", "")
	if err != nil {
		httpx.WriteJSON(w, 500, httpx.ErrorBody("settings_failed", "AI settings unavailable"))
		return
	}
	p, err := a.defaultAIPolicy()
	if err != nil {
		httpx.WriteJSON(w, 500, httpx.ErrorBody("settings_failed", "AI defaults unavailable"))
		return
	}
	if raw != "" {
		if json.Unmarshal([]byte(raw), &p) != nil {
			httpx.WriteJSON(w, 500, httpx.ErrorBody("settings_failed", "AI settings invalid"))
			return
		}
	}
	if r.Method == http.MethodGet {
		p.Prompt, err = a.store.Setting(r.Context(), "moderation.prompt", p.Prompt)
		if err != nil {
			httpx.WriteJSON(w, 500, httpx.ErrorBody("settings_failed", "AI prompt unavailable"))
			return
		}
		p.PrimaryKey = map[bool]string{true: "configured"}[p.PrimaryKey != ""]
		p.FallbackKey = map[bool]string{true: "configured"}[p.FallbackKey != ""]
		httpx.WriteJSON(w, 200, p)
		return
	}
	var in aiPolicy
	if err = httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteJSON(w, 400, httpx.ErrorBody("invalid_request", "invalid AI settings"))
		return
	}
	for _, v := range []string{in.PrimaryURL, in.FallbackURL} {
		if v == "" {
			continue
		}
		u, err := url.Parse(v)
		if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil {
			httpx.WriteJSON(w, 400, httpx.ErrorBody("invalid_endpoint", "invalid AI endpoint"))
			return
		}
	}
	encrypt := func(next, old string) (string, error) {
		if next == "" || next == "configured" {
			return old, nil
		}
		raw, err := a.secrets.Encrypt(next)
		if err != nil {
			return "", err
		}
		encoded, err := json.Marshal(raw)
		return string(encoded), err
	}
	in.PrimaryKey, err = encrypt(in.PrimaryKey, p.PrimaryKey)
	if err == nil {
		in.FallbackKey, err = encrypt(in.FallbackKey, p.FallbackKey)
	}
	if err != nil {
		httpx.WriteJSON(w, 500, httpx.ErrorBody("settings_failed", "could not encrypt AI key"))
		return
	}
	if in.Enabled && (in.PrimaryKey == "" || in.PrimaryURL == "" || in.PrimaryModel == "") {
		httpx.WriteJSON(w, 400, httpx.ErrorBody("invalid_settings", "enabled AI requires endpoint, model and key"))
		return
	}
	encoded, _ := json.Marshal(in)
	err = a.store.SetSettings(r.Context(), map[string]string{"policy.ai": string(encoded), "moderation.prompt": in.Prompt})
	if err == nil {
		err = a.applyAIPolicy(in)
	}
	if err != nil {
		httpx.WriteJSON(w, 500, httpx.ErrorBody("settings_failed", "could not apply AI settings"))
		return
	}
	a.audit(r, "policy.ai.update", "success", "AI policy updated; secrets omitted")
	httpx.WriteJSON(w, 200, map[string]bool{"saved": true})
}

func (a *App) RunPolicyRefresh(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		a.RefreshPolicies(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (a *App) handleReasonTemplates(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("template")
	if r.Method == http.MethodGet {
		scope := r.URL.Query().Get("scope")
		if scope == "review" {
			scope = "resource_review"
		}
		items, err := a.store.ReasonTemplates(r.Context(), scope)
		if err != nil {
			httpx.WriteJSON(w, 500, httpx.ErrorBody("templates_failed", "could not read templates"))
			return
		}
		httpx.WriteJSON(w, 200, map[string]any{"items": items})
		return
	}
	var err error
	if r.Method == http.MethodDelete {
		err = a.store.DisableReasonTemplate(r.Context(), id)
	} else {
		var in store.ReasonTemplate
		if err = httpx.DecodeJSON(r, &in); err == nil {
			if id != "" {
				in.ID = id
			}
			err = a.store.SaveReasonTemplate(r.Context(), in, currentAdmin(r).UserID)
		}
	}
	if err != nil {
		httpx.WriteJSON(w, 400, httpx.ErrorBody("template_invalid", err.Error()))
		return
	}
	a.audit(r, "template.update", "success", id)
	httpx.WriteJSON(w, 200, map[string]bool{"saved": true})
}

func (a *App) RefreshPolicies(ctx context.Context) {
	a.runtime.updateMu.Lock()
	defer a.runtime.updateMu.Unlock()
	if raw, err := a.store.Setting(ctx, "policy.build", ""); err == nil && raw != "" {
		var p buildPolicy
		if json.Unmarshal([]byte(raw), &p) == nil {
			a.applyBuildPolicy(p)
		}
	}
	if raw, err := a.store.Setting(ctx, "policy.ai", ""); err == nil && raw != "" {
		var p aiPolicy
		if json.Unmarshal([]byte(raw), &p) == nil {
			if err = a.applyAIPolicy(p); err != nil {
				oauthLog(ctx).Error("AI policy apply failed")
			}
		}
	}
}

func (a *App) defaultAIPolicy() (aiPolicy, error) {
	c := a.cfg.Moderation
	p := aiPolicy{Enabled: c.Primary.APIKey != "" && c.Primary.BaseURL != "", PrimaryURL: c.Primary.BaseURL, PrimaryModel: c.Primary.Model, FallbackURL: c.Fallback.BaseURL, FallbackModel: c.Fallback.Model, Prompt: moderation.DefaultPrompt}
	encode := func(key string) (string, error) {
		if key == "" {
			return "", nil
		}
		cipher, err := a.secrets.Encrypt(key)
		if err != nil {
			return "", err
		}
		raw, err := json.Marshal(cipher)
		return string(raw), err
	}
	var err error
	if p.PrimaryKey, err = encode(c.Primary.APIKey); err != nil {
		return p, err
	}
	p.FallbackKey, err = encode(c.Fallback.APIKey)
	return p, err
}

func (a *App) handleAdminAPIModerationPrompt(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Prompt string `json:"prompt"`
		Text   string `json:"text"`
		Test   bool   `json:"test"`
	}
	if err := httpx.ReadJSON(r, &body); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", "invalid json"))
		return
	}
	if body.Test {
		prompt := strings.TrimSpace(body.Prompt)
		if prompt == "" {
			prompt, _ = a.store.Setting(r.Context(), "moderation.prompt", moderation.DefaultPrompt)
		}
		a.runtime.mu.RLock()
		moderator := a.moderation
		a.runtime.mu.RUnlock()
		if moderator == nil || !moderator.Enabled() {
			httpx.WriteJSON(w, http.StatusServiceUnavailable, httpx.ErrorBody("moderation_unavailable", "审核模型未启用"))
			return
		}
		verdict, err := moderator.Review(r.Context(), prompt, body.Text)
		if err != nil {
			httpx.WriteJSON(w, http.StatusServiceUnavailable, httpx.ErrorBody("moderation_unavailable", err.Error()))
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "verdict": verdict})
		return
	}
	prompt := strings.TrimSpace(body.Prompt)
	if prompt == "" {
		prompt = moderation.DefaultPrompt
	}
	if err := a.store.SetSetting(r.Context(), "moderation.prompt", prompt); err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("settings_failed", err.Error()))
		return
	}
	a.audit(r, "moderation.prompt", "success", "")
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}
