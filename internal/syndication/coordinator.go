package syndication

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	MaxPublicationAttempts = 5
)

// reviewMetaRe matches the structured review marker carried inside Gitea PR
// comments. It is the single authoritative mechanism (bracket forms are retired):
//
//	<!-- oronbox-meta: {"role":"reviewer","tag":"NEEDFIX","id":"k7m2x9"} -->
var reviewMetaRe = regexp.MustCompile(`<!--\s*oronbox-meta:\s*(\{[^>]*?\})\s*-->`)

// ReviewMeta is a structured review action extracted from a PR comment.
type ReviewMeta struct {
	Role string `json:"role"`
	Tag  string `json:"tag"` // NEEDFIX (reviewer) | FIXED (creator)
	ID   string `json:"id"`
}

// ExtractReviewMeta parses the structured review markers out of a PR comment body.
func ExtractReviewMeta(text string) []ReviewMeta {
	out := []ReviewMeta{}
	for _, m := range reviewMetaRe.FindAllStringSubmatch(text, -1) {
		var meta ReviewMeta
		if json.Unmarshal([]byte(m[1]), &meta) == nil && meta.Tag != "" && meta.ID != "" {
			out = append(out, meta)
		}
	}
	return out
}

func Rollup(states []string) string {
	if len(states) == 0 {
		return "pending"
	}
	published, failed := 0, 0
	for _, s := range states {
		if s == "published" {
			published++
		}
		if s == "failed" || s == "external_rejected" {
			failed++
		}
	}
	if published == len(states) {
		return "published"
	}
	if published > 0 && failed > 0 {
		return "partially_published"
	}
	if failed == len(states) {
		return "syndication_failed"
	}
	return "syndicating"
}
func RetryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	d := 30 * time.Second
	for i := 1; i < attempt; i++ {
		d *= 2
	}
	if d > 10*time.Minute {
		d = 10 * time.Minute
	}
	return d
}
func CategoryIDs(downloads []struct{ CategoryID int }) []int {
	set := map[int]struct{}{}
	for _, d := range downloads {
		if d.CategoryID > 0 {
			set[d.CategoryID] = struct{}{}
		}
	}
	out := make([]int, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Ints(out)
	return out
}
func StateDetail(attempt int, err error) map[string]any {
	m := map[string]any{"attempt": attempt, "can_retry": attempt < MaxPublicationAttempts}
	if err != nil {
		m["error"] = err.Error()
		m["next_attempt_seconds"] = int(RetryDelay(attempt).Seconds())
	}
	return m
}
func ParseCategoryID(v string) int { n, _ := strconv.Atoi(strings.TrimSpace(v)); return n }

// DeviceSpec is one entry of the community devices.json (keyed by codename; `id`
// is the human-facing compatibility id used as the manifest.downloads key).
type DeviceSpec struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Vendor string `json:"vendor"`
}

// PublishConfig mirrors the central repo publish.json (bandbbs keyed by codename).
// Resource prefixes are a single global map (米坛 resource自身前缀): the OronBox
// client uses watchface=81 / quick_app=82 / firmware=85 for every category.
type PublishConfig struct {
	ResourcePrefixes map[string]int             `json:"resource_prefixes"`
	BandBBS          map[string]BandBBSCategory `json:"bandbbs"`
}
type BandBBSCategory struct {
	CategoryID     int            `json:"category_id"`
	ForumID        int            `json:"forum_id"`
	ThreadPrefixes map[string]int `json:"thread_prefixes"`
}

// PublicationPlan is one derived publication task.
type PublicationPlan struct {
	Provider   string
	CategoryID int
	Config     map[string]any
}

// PlanPublications derives the concrete publication tasks from the creator's
// chosen platforms. The server fans bandbbs out to one task per 米坛 category by
// resolving manifest.downloads device ids through devices.json + publish.json,
// so category_id is never silently left at 0.
func PlanPublications(targets []string, downloads map[string]DownloadEntry, devices map[string]DeviceSpec, publish PublishConfig) ([]PublicationPlan, error) {
	out := []PublicationPlan{}
	for _, t := range targets {
		switch t {
		case "bandbbs":
			byCompat := map[string]string{}
			for codename, spec := range devices {
				if spec.ID != "" {
					byCompat[spec.ID] = codename
				}
			}
			cats := map[int]BandBBSCategory{}
			compatByCat := map[int][]string{}
			for compatID := range downloads {
				codename, ok := byCompat[compatID]
				if !ok {
					return nil, fmt.Errorf("device %q is not defined in devices.json", compatID)
				}
				c, ok := publish.BandBBS[codename]
				if !ok || c.CategoryID <= 0 {
					return nil, fmt.Errorf("no bandbbs category mapped for device %q", compatID)
				}
				cats[c.CategoryID] = c
				compatByCat[c.CategoryID] = append(compatByCat[c.CategoryID], compatID)
			}
			if len(cats) == 0 {
				return nil, fmt.Errorf("bandbbs target requires at least one device download")
			}
			ids := make([]int, 0, len(cats))
			for id := range cats {
				ids = append(ids, id)
			}
			sort.Ints(ids)
			for _, id := range ids {
				compat := compatByCat[id]
				sort.Strings(compat)
				out = append(out, PublicationPlan{Provider: "bandbbs", CategoryID: id, Config: map[string]any{
					"category_id":       id,
					"forum_id":          cats[id].ForumID,
					"resource_prefixes": publish.ResourcePrefixes,
					"thread_prefixes":   cats[id].ThreadPrefixes,
					"device_compat_ids": compat,
				}})
			}
		case "oronbox", "astrobox":
			out = append(out, PublicationPlan{Provider: t, CategoryID: 0, Config: map[string]any{}})
		default:
			return nil, fmt.Errorf("unknown publication target %q", t)
		}
	}
	return out, nil
}

type SubmissionCoordinator struct {
	Gitea              GiteaClient
	Owner, CatalogRepo string
}

func (c SubmissionCoordinator) Submit(ctx context.Context, pkg, version string, manifest Manifest) (int, string, error) {
	if pkg == "" || version == "" {
		return 0, "", fmt.Errorf("package and version are required")
	}
	branch := "submission/" + pkg + "-" + version
	repo := "oronbox-resource-" + pkg
	if e := c.Gitea.CreateRepository(ctx, c.Owner, repo, true); e != nil {
		return 0, "", e
	}
	raw, _ := json.MarshalIndent(manifest, "", "  ")
	if e := c.Gitea.CreateFile(ctx, c.Owner, repo, "manifest.json", branch, "submit "+pkg, raw); e != nil {
		return 0, "", e
	}
	n, e := c.Gitea.CreatePullRequest(ctx, c.Owner, c.CatalogRepo, "Publish "+pkg+" "+version, branch, "main", "")
	return n, branch, e
}

// SubmitIndex opens a PR in the catalog repo that upserts the resource's index.csv
// row. The resource's own repo/bundle is pushed separately; this only edits the
// central index, matching community-repository-spec.md §2.3.
func (c SubmissionCoordinator) SubmitIndex(ctx context.Context, pkg, version string, row CatalogRow) (int, string, error) {
	if pkg == "" || version == "" {
		return 0, "", fmt.Errorf("package and version are required")
	}
	branch := "submission/" + pkg + "-" + version
	if e := c.Gitea.CreateBranch(ctx, c.Owner, c.CatalogRepo, branch, "main"); e != nil {
		return 0, "", e
	}
	existing, _ := c.Gitea.ReadFile(ctx, c.Owner, c.CatalogRepo, "index.csv", branch)
	next, e := UpsertCatalogRow(string(existing), row)
	if e != nil {
		return 0, "", e
	}
	if e := c.Gitea.UpsertFile(ctx, c.Owner, c.CatalogRepo, "index.csv", branch, "update index.csv for "+pkg+" "+version, []byte(next)); e != nil {
		return 0, "", e
	}
	n, e := c.Gitea.CreatePullRequest(ctx, c.Owner, c.CatalogRepo, "Publish "+pkg+" "+version, branch, "main", "")
	return n, branch, e
}
