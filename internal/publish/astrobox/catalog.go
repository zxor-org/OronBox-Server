package astrobox

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"fmt"
	"path"
	"strings"
)

var catalogHeader = []string{"id", "name", "restype", "repo_owner", "repo_name", "repo_commit_hash", "icon", "cover", "tags", "device_vendors", "devices", "paid_type"}

const (
	submissionRootPath        = "tmp"
	submissionCSVFileName     = "resource.csv"
	submissionRequestFileName = "request.json"
	purchaseLinkIcon          = "coins"
	purchaseLinkTitle         = "购买链接"
)

func purchaseLinks(rawURL string, extras ...[]struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}) []map[string]string {
	purchaseURL := strings.TrimSpace(rawURL)
	result := []map[string]string{}
	if purchaseURL != "" {
		result = append(result, map[string]string{
			"title": purchaseLinkTitle,
			"url":   purchaseURL,
			"icon":  purchaseLinkIcon,
		})
	}
	for _, group := range extras {
		for _, link := range group {
			if strings.TrimSpace(link.URL) != "" {
				result = append(result, map[string]string{"title": strings.TrimSpace(link.Title), "url": strings.TrimSpace(link.URL), "icon": "Link"})
			}
		}
	}
	return result
}

func buildManifest(
	snap snapshot,
	cfg publishConfig,
	restype, iconPath, coverPath string,
	authors []map[string]any,
	previews []string,
	downloads any,
) map[string]any {
	return map[string]any{
		"item": map[string]any{
			"id":          cfg.ItemID,
			"restype":     restype,
			"name":        snap.Revision.Name,
			"description": snap.Revision.Description,
			"preview":     previews,
			"icon":        iconPath,
			"cover":       coverPath,
			"author":      authors,
		},
		"links":     purchaseLinks(snap.Revision.PurchaseLink, snap.Revision.Links),
		"downloads": downloads,
		"ext":       map[string]any{},
	}
}

// supportedDeviceIDs mirrors the device IDs currently declared by
// AstroBox-Repo/devices_v2.json. Keep this list intentionally narrower than
// OronBox's device catalog: AstroBox rejects catalog entries that reference an
// unknown V2 device ID.
var supportedDeviceIDs = map[string]struct{}{
	"xmb9":       {},
	"xmb9p":      {},
	"xmb10":      {},
	"xmb10nfc":   {},
	"xmb10p":     {},
	"xmws3":      {},
	"xmws4":      {},
	"xmws4xring": {},
	"xmws441":    {},
	"xmws5":      {},
	"xmrw5":      {},
	"xmrw5xring": {},
	"xmrw6":      {},
}

// IsSupportedDeviceID reports whether an ID is present in AstroBox-Repo's
// current V2 device catalog.
func IsSupportedDeviceID(id string) bool {
	_, ok := supportedDeviceIDs[strings.TrimSpace(id)]
	return ok
}

type CatalogCheck struct {
	Found   bool
	Matches bool
	Row     []string
}

func (c *Client) CheckCatalogEntry(ctx context.Context, token, itemID string, expected []string) (CatalogCheck, error) {
	data, _, err := c.getContent(ctx, token, c.cfg.RepoOwner, c.cfg.RepoName, c.cfg.CatalogPath, c.cfg.RepoBranch)
	if err != nil {
		return CatalogCheck{}, err
	}
	rows, err := csv.NewReader(bytes.NewReader(data)).ReadAll()
	if err != nil {
		return CatalogCheck{}, fmt.Errorf("parse AstroBox catalog: %w", err)
	}
	check := CatalogCheck{}
	for index := 1; index < len(rows); index++ {
		if len(rows[index]) == 0 || rows[index][0] != itemID {
			continue
		}
		check.Found = true
		if catalogRowsEqual(rows[index], expected) {
			check.Matches = true
			check.Row = rows[index]
			return check, nil
		}
		if check.Row == nil {
			check.Row = rows[index]
		}
	}
	return check, nil
}

func (c *Client) submissionPath(login, repoName string) (string, error) {
	login = strings.ToLower(strings.TrimSpace(login))
	repoName = strings.ToLower(strings.TrimSpace(repoName))
	if err := validateSubmissionSegment(login, "GitHub login"); err != nil {
		return "", err
	}
	if err := validateSubmissionSegment(repoName, "GitHub repository name"); err != nil {
		return "", err
	}
	return path.Join(submissionRootPath, login, repoName), nil
}

func validateSubmissionSegment(value, label string) error {
	if value == "" || value == "." || value == ".." {
		return fmt.Errorf("%s is required", label)
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			continue
		}
		return fmt.Errorf("invalid %s %q", label, value)
	}
	return nil
}

func encodeCatalogRows(rows [][]string) ([]byte, error) {
	var encoded bytes.Buffer
	writer := csv.NewWriter(&encoded)
	if err := writer.WriteAll(rows); err != nil {
		return nil, err
	}
	return encoded.Bytes(), nil
}

func catalogRowDigest(row []string) (string, error) {
	canonical := make([]string, len(row))
	for index, value := range row {
		canonical[index] = strings.TrimSpace(value)
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(canonical, ",")))), nil
}

func catalogRowsEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func supportedArtifactDevices(devices []deviceRef) []deviceRef {
	result := make([]deviceRef, 0, len(devices))
	for _, device := range devices {
		if IsSupportedDeviceID(device.ID) {
			result = append(result, device)
		}
	}
	return result
}

func uniqueVendors(artifacts []artifact) []string {
	seen := map[string]bool{}
	var result []string
	for _, item := range artifacts {
		for range supportedArtifactDevices(item.Devices) {
			vendor := "xiaomi"
			if !seen[vendor] {
				seen[vendor] = true
				result = append(result, vendor)
			}
		}
	}
	return result
}

func validateCatalogRow(values []string) error {
	columns := []string{"id", "name", "restype", "repo_owner", "repo_name", "repo_commit_hash", "icon", "cover", "tags", "device_vendors", "devices", "paid_type"}
	if len(values) != len(columns) {
		return fmt.Errorf("AstroBox catalog row must contain %d fields", len(columns))
	}
	for index, value := range values {
		if strings.ContainsAny(value, ",\r\n\x00") {
			return fmt.Errorf("AstroBox catalog field %s contains a structural CSV character", columns[index])
		}
	}
	for _, vendor := range strings.Split(values[9], ";") {
		if vendor != "xiaomi" {
			return fmt.Errorf("unsupported AstroBox catalog vendor %q", vendor)
		}
	}
	for _, device := range strings.Split(values[10], ";") {
		if device != "" && !IsSupportedDeviceID(device) {
			return fmt.Errorf("unsupported AstroBox catalog device %q", device)
		}
	}
	return nil
}

func normalizePaid(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "free") {
		return ""
	}
	return strings.TrimSpace(value)
}
