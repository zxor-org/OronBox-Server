package creator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zxor-org/OronBox-Server/internal/config"
)

// SyncDeviceCatalog imports only devices.json. Gitea is not a resource source.
// Existing UUIDs are retained by codename, including all artifact foreign keys.
func (s *Service) SyncDeviceCatalog(ctx context.Context, c config.DeviceCatalogConfig) error {
	parts := strings.Split(c.Repository, "/")
	valid := regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
	if len(parts) != 2 || !valid.MatchString(parts[0]) || !valid.MatchString(parts[1]) {
		return fmt.Errorf("invalid Gitea catalog repository")
	}
	base, err := url.Parse(c.BaseURL)
	if err != nil || base.Host == "" || (base.Scheme != "https" && base.Scheme != "http") {
		return fmt.Errorf("invalid Gitea URL")
	}
	ref := c.Ref
	if ref == "" {
		ref = "main"
	}
	endpoint := strings.TrimRight(c.BaseURL, "/") + "/api/v1/repos/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1]) + "/raw/devices.json?ref=" + url.QueryEscape(ref)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "token "+c.Token)
	}
	client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch Gitea device catalog failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Gitea device catalog HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return fmt.Errorf("invalid Gitea device catalog body")
	}
	var catalog map[string]struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Vendor string `json:"vendor"`
	}
	if json.Unmarshal(raw, &catalog) != nil || len(catalog) == 0 {
		return fmt.Errorf("invalid or empty Gitea device catalog")
	}
	seen := map[string]bool{}
	for code, item := range catalog {
		if !valid.MatchString(code) || strings.TrimSpace(item.Name) == "" || !valid.MatchString(item.ID) || seen[item.ID] {
			return fmt.Errorf("invalid or duplicate Gitea device entry")
		}
		seen[item.ID] = true
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Clear external mappings in the same transaction, allowing renamed mappings
	// without transient unique-index conflicts. Absent devices remain as history.
	if _, err = tx.ExecContext(ctx, `UPDATE devices SET enabled=false,astrobox_id='' WHERE platform='vela_os'`); err != nil {
		return err
	}
	for code, item := range catalog {
		id := uuid.NewSHA1(uuid.NameSpaceURL, []byte("https://oronbox.org/devices/"+code))
		_, err = tx.ExecContext(ctx, `INSERT INTO devices(id,codename,display_name,platform,astrobox_id,vendor) VALUES($1,$2,$3,'vela_os',$4,$5)
  ON CONFLICT(codename) DO UPDATE SET display_name=excluded.display_name,astrobox_id=excluded.astrobox_id,vendor=excluded.vendor,enabled=true,updated_at=now() WHERE devices.platform='vela_os'`, id, code, item.Name, item.ID, item.Vendor)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}
