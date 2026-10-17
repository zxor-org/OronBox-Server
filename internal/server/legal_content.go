package server

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/gitea"
	"github.com/zxor-org/OronBox-Server/internal/httpx"
)

type LegalDocument struct {
	ID               string            `json:"id"`
	Category         string            `json:"category"`
	Revision         int               `json:"revision"`
	RequireAgreement bool              `json:"require_agreement"`
	Titles           map[string]string `json:"titles"`
	Files            map[string]string `json:"files"`
	Bodies           map[string]string `json:"bodies,omitempty"`
}

type LegalContent struct {
	Documents []LegalDocument `json:"documents"`
	Commit    string          `json:"commit"`
}

type contentCache struct {
	mu    sync.RWMutex
	legal LegalContent
}

func canonicalLegalID(id string) string {
	switch id {
	case "publishing":
		return "resource-publishing"
	case "review_rules":
		return "review-rules"
	}
	return id
}

func (a *App) syncLegal(ctx context.Context) error {
	client := gitea.Client{Config: a.cfg.DeviceCatalog}
	head, err := client.Head(ctx)
	if err != nil {
		return err
	}
	raw, _, err := client.File(ctx, "legal/manifest.json", head)
	if err != nil {
		return err
	}
	var content LegalContent
	if json.Unmarshal(raw, &content) != nil || len(content.Documents) == 0 || len(content.Documents) > 30 {
		return fmt.Errorf("invalid legal manifest")
	}
	seen := map[string]bool{}
	for i := range content.Documents {
		d := &content.Documents[i]
		d.ID = canonicalLegalID(d.ID)
		if _, ok := legalDocuments[d.ID]; !ok || seen[d.ID] || d.Revision < 1 || len(d.Files) == 0 {
			return fmt.Errorf("invalid legal document")
		}
		seen[d.ID] = true
		d.Bodies = map[string]string{}
		for lang, p := range d.Files {
			if lang != "zh" && lang != "en" || !gitea.ValidPath(p) || strings.Contains(p, "/") {
				return fmt.Errorf("invalid legal file")
			}
			body, _, err := client.File(ctx, "legal/"+p, head)
			if err != nil || len(body) == 0 || len(body) > 1<<20 {
				return fmt.Errorf("invalid legal document body")
			}
			d.Bodies[lang] = string(body)
		}
	}
	for _, id := range []string{"terms", "privacy", "resource-publishing"} {
		if !seen[id] {
			return fmt.Errorf("required legal document absent")
		}
	}
	a.content.mu.RLock()
	previous := a.content.legal
	a.content.mu.RUnlock()
	for _, next := range content.Documents {
		for _, old := range previous.Documents {
			if next.ID != old.ID {
				continue
			}
			if next.Revision < old.Revision || (next.Revision == old.Revision && !maps.Equal(next.Bodies, old.Bodies)) {
				return fmt.Errorf("legal document %s changed without a newer revision", next.ID)
			}
		}
	}
	content.Commit = head
	raw, _ = json.Marshal(content)
	if err = a.store.SetSetting(ctx, "content.legal.cache", string(raw)); err != nil {
		return err
	}
	a.content.mu.Lock()
	a.content.legal = content
	a.content.mu.Unlock()
	return nil
}

func (a *App) handleLegalManifest(w http.ResponseWriter, r *http.Request) {
	a.content.mu.RLock()
	value := a.content.legal
	a.content.mu.RUnlock()
	if len(value.Documents) == 0 {
		httpx.WriteJSON(w, 503, httpx.ErrorBody("legal_unavailable", "no verified legal content available"))
		return
	}
	out := make([]LegalDocument, len(value.Documents))
	copy(out, value.Documents)
	for i := range out {
		out[i].Bodies = nil
	}
	httpx.WriteJSON(w, 200, map[string]any{"documents": out, "commit": value.Commit})
}

func (a *App) RunContentSync(ctx context.Context) {
	if raw, err := a.store.Setting(ctx, "content.legal.cache", ""); err == nil && raw != "" {
		var cached LegalContent
		if json.Unmarshal([]byte(raw), &cached) == nil {
			a.content.mu.Lock()
			a.content.legal = cached
			a.content.mu.Unlock()
		}
	}
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for {
		if err := a.syncLegal(ctx); err != nil && ctx.Err() == nil {
			oauthLog(ctx).Warn("legal content sync failed", "error", err)
		}
		if err := a.syncBlogContent(ctx); err != nil && ctx.Err() == nil {
			oauthLog(ctx).Warn("blog content sync failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
