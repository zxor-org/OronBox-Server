package creator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/zxor-org/OronBox-Server/internal/config"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type PublicationCatalog struct {
	ResourcePrefixes map[string]int `json:"resource_prefixes"`
	BandBBS          map[string]struct {
		CategoryID     int            `json:"category_id"`
		ForumID        int            `json:"forum_id"`
		ThreadPrefixes map[string]int `json:"thread_prefixes"`
	} `json:"bandbbs"`
	Version string `json:"version"`
}
type publicationCatalogState struct {
	mu    sync.RWMutex
	value *PublicationCatalog
}

func (s *Service) SyncPublicationCatalog(ctx context.Context, c config.DeviceCatalogConfig) error {
	parts := strings.Split(c.Repository, "/")
	if len(parts) != 2 {
		return fmt.Errorf("invalid publication repository")
	}
	endpoint := strings.TrimRight(c.BaseURL, "/") + "/api/v1/repos/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1]) + "/raw/publish.json?ref=" + url.QueryEscape(c.Ref)
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return err
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "token "+c.Token)
	}
	client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("Gitea publication config unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("Gitea publication config HTTP %d", res.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return fmt.Errorf("invalid publication config body")
	}
	var cfg PublicationCatalog
	if json.Unmarshal(raw, &cfg) != nil || len(cfg.BandBBS) == 0 {
		return fmt.Errorf("invalid publication config")
	}
	if cfg.ResourcePrefixes["quickapp"] == 0 {
		cfg.ResourcePrefixes["quickapp"] = cfg.ResourcePrefixes["app"]
	}
	if cfg.ResourcePrefixes["quickapp"] <= 0 || cfg.ResourcePrefixes["watchface"] <= 0 {
		return fmt.Errorf("missing resource prefixes")
	}
	for _, v := range cfg.BandBBS {
		if v.CategoryID <= 0 {
			return fmt.Errorf("invalid BandBBS category")
		}
	}
	hash := sha256.Sum256(raw)
	cfg.Version = hex.EncodeToString(hash[:])
	s.publicationCatalog.mu.Lock()
	s.publicationCatalog.value = &cfg
	s.publicationCatalog.mu.Unlock()
	return nil
}
func (s *Service) PublicationOptions() *PublicationCatalog {
	s.publicationCatalog.mu.RLock()
	defer s.publicationCatalog.mu.RUnlock()
	return s.publicationCatalog.value
}

// Client category/prefix claims are replaced by the operator-owned mapping.
func (s *Service) mapPublications(ctx context.Context, kind ResourceKind, artifacts []verifiedArtifact, publications []PublicationRequest, strict bool) error {
	cfg := s.PublicationOptions()
	for i := range publications {
		p := &publications[i]
		if p.Target != PublishBandBBS {
			continue
		}
		if cfg == nil {
			if strict && s.RequirePublicationCatalog {
				return fmt.Errorf("%w: publication config unavailable", ErrInvalid)
			}
			continue
		}
		byCategory := map[int]string{}
		targets := []any{}
		for _, a := range artifacts {
			for _, id := range unique(a.deviceIDs) {
				var code string
				if err := s.db.QueryRowContext(ctx, `SELECT codename FROM devices WHERE id=$1 AND enabled`, id).Scan(&code); err != nil {
					return err
				}
				v, ok := cfg.BandBBS[code]
				if !ok {
					return fmt.Errorf("%w: device has no publication category", ErrInvalid)
				}
				if prior, exists := byCategory[v.CategoryID]; exists {
					if prior != a.sha256 {
						return fmt.Errorf("%w: multiple packages share one category", ErrInvalid)
					}
					continue
				}
				byCategory[v.CategoryID] = a.sha256
				threadPrefixID := 0
				if v.ThreadPrefixes != nil {
					threadPrefixID = v.ThreadPrefixes[string(kind)]
					if threadPrefixID == 0 {
						threadPrefixID = v.ThreadPrefixes["watchface"]
					}
				}
				targetMap := map[string]any{
					"category_id": v.CategoryID,
					"prefix_id":   cfg.ResourcePrefixes[string(kind)],
					"package_id":  a.analysis.PackageID,
				}
				if threadPrefixID > 0 {
					targetMap["thread_prefix_id"] = threadPrefixID
				}
				if v.ForumID > 0 {
					targetMap["forum_id"] = v.ForumID
				}
				targets = append(targets, targetMap)
			}
		}
		if p.Config == nil {
			p.Config = map[string]any{}
		}
		p.Config["targets"] = targets
		p.Config["config_version"] = cfg.Version
	}
	return nil
}
