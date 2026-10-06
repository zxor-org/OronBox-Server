// Package astrobox implements the AstroBox (AB) staging submission protocol as a
// server-side worker: it mirrors the creator's resource repo on GitHub and opens
// a staging PR (tmp/<login>/<repo>/{resource.csv,request.json}) against
// AstralSightStudios/AstroBox-Repo. It never touches AB-private crypto/commerce.
package astrobox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/syndication"
)

const (
	manifestV2Name  = "manifest_v2.json"
	submissionRoot  = "tmp"
	purchaseTitle   = "购买链接"
	purchaseIcon    = "coins"
	resourceCSVHead = "id,name,restype,repo_owner,repo_name,repo_commit_hash,icon,cover,tags,device_vendors,devices,paid_type"
)

type Client struct {
	GitHub      syndication.GitHubClient
	RepoOwner   string // upstream AB repo owner, e.g. AstralSightStudios
	RepoName    string // e.g. AstroBox-Repo
	RepoBranch  string // e.g. main
	CatalogPath string // e.g. index_v2.csv
}

// Repo is the creator's AB resource repo state after a mirror commit.
type Repo struct {
	Owner, Name, Commit string
}

// ---------- AB manifest / catalog / request builders ----------

type abAuthor struct {
	Name          string `json:"name"`
	BindABAccount bool   `json:"bindABAccount"`
}

type abLink struct {
	Title string `json:"title"`
	URL   string `json:"url"`
	Icon  string `json:"icon,omitempty"`
}

// BuildManifestV2 converts the OronBox manifest + changelog into AB's
// manifest_v2.json. Author keeps only name/bindABAccount and each download gets
// its applicable updatelogs.
func BuildManifestV2(m syndication.Manifest, ch syndication.Changelog, purchaseLink string) ([]byte, error) {
	out := map[string]any{}
	item := map[string]any{
		"id":          m.Item.ID,
		"restype":     m.Item.Restype,
		"name":        m.Item.Name,
		"description": m.Item.Description,
		"preview":     m.Item.Preview,
		"icon":        m.Item.Icon,
		"cover":       m.Item.Cover,
	}
	authors := make([]abAuthor, 0, len(m.Item.Author))
	for _, a := range m.Item.Author {
		authors = append(authors, abAuthor{Name: a.Name, BindABAccount: a.BindABAccount})
	}
	item["author"] = authors
	out["item"] = item

	links := make([]abLink, 0, len(m.Links)+1)
	if strings.TrimSpace(purchaseLink) != "" {
		links = append(links, abLink{Title: purchaseTitle, URL: strings.TrimSpace(purchaseLink), Icon: purchaseIcon})
	}
	for _, l := range m.Links {
		if strings.TrimSpace(l.URL) == "" {
			continue
		}
		links = append(links, abLink{Title: strings.TrimSpace(l.Title), URL: strings.TrimSpace(l.URL), Icon: strings.TrimSpace(l.Icon)})
	}
	out["links"] = links

	downloads := map[string]any{}
	for dev, entry := range m.Downloads {
		d := map[string]any{"version": entry.Version, "file_name": entry.FileName}
		if entry.VersionCode != 0 {
			d["versionCode"] = entry.VersionCode
		}
		if logs := updatelogsFor(ch, dev); len(logs) > 0 {
			d["updatelogs"] = logs
		}
		downloads[dev] = d
	}
	out["downloads"] = downloads
	out["ext"] = map[string]any{}
	return json.MarshalIndent(out, "", "  ")
}

func updatelogsFor(ch syndication.Changelog, dev string) []map[string]string {
	out := []map[string]string{}
	for _, rel := range ch.Releases {
		if len(rel.Devices) > 0 && !contains(rel.Devices, dev) {
			continue
		}
		out = append(out, map[string]string{"version": rel.Version, "content": rel.Content})
	}
	return out
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// CatalogEntry is one resource.csv row (a subset of AB index_v2.csv columns).
type CatalogEntry struct {
	ID, Name, Restype, RepoOwner, RepoName, RepoCommitHash, Icon, Cover, Tags, DeviceVendors, Devices, PaidType string
}

func (e CatalogEntry) row() []string {
	return []string{e.ID, e.Name, e.Restype, e.RepoOwner, e.RepoName, e.RepoCommitHash, e.Icon, e.Cover, e.Tags, e.DeviceVendors, e.Devices, syndication.NormalizePaidType(e.PaidType)}
}

// BuildResourceCSV renders the 1-row submission CSV (header + data).
func BuildResourceCSV(e CatalogEntry) []byte {
	return []byte(resourceCSVHead + "\n" + strings.Join(e.row(), ",") + "\n")
}

// SubmissionRequest mirrors ABCC request.json.
type SubmissionRequest struct {
	SchemaVersion     int             `json:"schema_version"`
	Mode              string          `json:"mode"`
	OriginalID        *string         `json:"original_id"`
	BaseEntryDigest   *string         `json:"base_entry_digest"`
	BaseCatalogCommit *string         `json:"base_catalog_commit"`
	Client            json.RawMessage `json:"client"`
}

// BuildRequestJSON renders request.json. client may be nil.
func BuildRequestJSON(req SubmissionRequest) []byte {
	if req.SchemaVersion == 0 {
		req.SchemaVersion = 1
	}
	if len(req.Client) == 0 {
		req.Client = json.RawMessage("null")
	}
	raw, _ := json.MarshalIndent(req, "", "  ")
	return append(raw, '\n')
}

// DeriveSubmission decides create vs edit against the upstream catalog.
func DeriveSubmission(entry CatalogEntry, upstreamCSV, upstreamCommit string) SubmissionRequest {
	req := SubmissionRequest{SchemaVersion: 1, Mode: "create"}
	if strings.TrimSpace(upstreamCommit) != "" {
		c := upstreamCommit
		req.BaseCatalogCommit = &c
	}
	rows := strings.Split(strings.ReplaceAll(upstreamCSV, "\r\n", "\n"), "\n")
	for i, line := range rows {
		if i == 0 || strings.TrimSpace(line) == "" {
			continue
		}
		cols := strings.Split(line, ",")
		if len(cols) < 12 || cols[0] != entry.ID {
			continue
		}
		id := entry.ID
		digest := rowDigest(cols[:12])
		req.Mode = "edit"
		req.OriginalID = &id
		req.BaseEntryDigest = &digest
		break
	}
	return req
}

func rowDigest(cols []string) string {
	canonical := make([]string, len(cols))
	for i, c := range cols {
		canonical[i] = strings.TrimSpace(c)
	}
	sum := sha256.Sum256([]byte(strings.Join(canonical, ",")))
	return hex.EncodeToString(sum[:])
}

// ---------- review status (ABCC [ABCC_*] tags) ----------

var abccTagRe = regexp.MustCompile(`(?is)^\s*\[ABCC_(NEEDFIX|FIXED|CLOSE|REOPEN|REFUSE)(?:_([^\]]+))?\]\s*([\s\S]*)$`)

type ReviewItem struct {
	ID      string `json:"id"`
	Message string `json:"message"`
	Fixed   bool   `json:"fixed"`
}

// DeriveReviewStatus replicates ABCC's algorithm: NEEDFIX opens an item, FIXED
// resolves it (only when a NEEDFIX exists). No NEEDFIX → waiting_review.
func DeriveReviewStatus(bodies []string) (state string, items []ReviewItem) {
	need := map[string]string{}
	fixed := map[string]bool{}
	var order []string
	for _, body := range bodies {
		m := abccTagRe.FindStringSubmatch(strings.TrimSpace(body))
		if m == nil {
			continue
		}
		kind := strings.ToUpper(m[1])
		id := strings.TrimSpace(m[2])
		msg := strings.TrimSpace(m[3])
		switch kind {
		case "NEEDFIX":
			if _, ok := need[id]; !ok {
				order = append(order, id)
			}
			need[id] = msg
			delete(fixed, id)
		case "FIXED":
			if _, ok := need[id]; ok {
				fixed[id] = true
			}
		}
	}
	if len(need) == 0 {
		return "waiting_review", nil
	}
	items = make([]ReviewItem, 0, len(need))
	unresolved := false
	for _, id := range order {
		it := ReviewItem{ID: id, Message: need[id], Fixed: fixed[id]}
		if !it.Fixed {
			unresolved = true
		}
		items = append(items, it)
	}
	if unresolved {
		return "changes_requested", items
	}
	return "fixed_waiting", items
}

// ---------- GitHub operations ----------

func (c Client) gh(token string) syndication.GitHubClient {
	gh := c.GitHub
	gh.Token = token
	return gh
}

// EnsureResourceRepo creates/updates the creator's astrobox-resource-<slug> repo
// with the given files in a single mirror commit.
func (c Client) EnsureResourceRepo(ctx context.Context, token, repoName, title string, files map[string][]byte) (Repo, error) {
	gh := c.gh(token)
	owner, name, branch, err := gh.EnsureUserRepo(ctx, repoName, "AstroBox resource of "+title)
	if err != nil {
		return Repo{}, fmt.Errorf("ensure resource repo: %w", err)
	}
	commit, err := gh.CommitFilesTree(ctx, owner, name, branch, "publish: "+title, files)
	if err != nil {
		return Repo{}, fmt.Errorf("mirror resource bundle: %w", err)
	}
	return Repo{Owner: owner, Name: name, Commit: commit}, nil
}

// Submit forks the upstream AB repo, syncs it, commits the two staging files on a
// new branch and opens the PR. Returns (prNumber, prURL, submissionPath).
func (c Client) Submit(ctx context.Context, token, login string, ri Repo, entry CatalogEntry, req SubmissionRequest) (int, string, string, error) {
	gh := c.gh(token)
	submissionPath := submissionRoot + "/" + strings.ToLower(strings.TrimSpace(login)) + "/" + strings.ToLower(ri.Name)

	forkOwner, forkRepo, forkBranch, err := gh.ForkRepo(ctx, c.RepoOwner, c.RepoName)
	if err != nil {
		return 0, "", "", fmt.Errorf("fork AB repo: %w", err)
	}
	if forkBranch == "" {
		forkBranch = c.RepoBranch
	}
	gh.SyncForkDefaultBranch(ctx, forkOwner, forkRepo, forkBranch, c.RepoOwner, c.RepoName)
	forkHEAD, err := gh.GetRefSHA(ctx, forkOwner, forkRepo, "heads/"+forkBranch)
	if err != nil {
		return 0, "", "", fmt.Errorf("read fork head: %w", err)
	}
	upstreamHEAD, err := gh.GetRefSHA(ctx, c.RepoOwner, c.RepoName, "heads/"+c.RepoBranch)
	if err != nil {
		return 0, "", "", fmt.Errorf("read upstream head: %w", err)
	}
	if forkHEAD != upstreamHEAD {
		return 0, "", "", fmt.Errorf("fork %s/%s is not synced to upstream (upstream %s, fork %s)", forkOwner, forkRepo, short(upstreamHEAD), short(forkHEAD))
	}
	branch := fmt.Sprintf("astrobox-submit-%d", time.Now().Unix())
	if err := gh.CreateBranch(ctx, forkOwner, forkRepo, branch, forkHEAD); err != nil {
		return 0, "", "", fmt.Errorf("create submission branch: %w", err)
	}
	baseTree, err := gh.CommitTree(ctx, forkOwner, forkRepo, forkHEAD)
	if err != nil {
		return 0, "", "", fmt.Errorf("read fork tree: %w", err)
	}
	files := map[string][]byte{
		submissionPath + "/resource.csv": BuildResourceCSV(entry),
		submissionPath + "/request.json": BuildRequestJSON(req),
	}
	if _, err := gh.CommitFilesTreeBase(ctx, forkOwner, forkRepo, branch, "submit: "+entry.ID, baseTree, files); err != nil {
		return 0, "", "", fmt.Errorf("commit submission files: %w", err)
	}
	prNumber, prURL, err := gh.CreatePullRequest(ctx, c.RepoOwner, c.RepoName, "[ABCC] Add new resource "+entry.Name, forkOwner, branch, c.RepoBranch, "")
	if err != nil {
		return 0, "", "", fmt.Errorf("open AstroBox PR: %w", err)
	}
	return prNumber, prURL, submissionPath, nil
}

// UpstreamCatalog reads index_v2.csv from the upstream repo and returns (content, commit).
func (c Client) UpstreamCatalog(ctx context.Context, token string) (string, string) {
	gh := c.gh(token)
	commit, _ := gh.GetRefSHA(ctx, c.RepoOwner, c.RepoName, "heads/"+c.RepoBranch)
	raw, err := gh.ReadFile(ctx, c.RepoOwner, c.RepoName, c.CatalogPath, c.RepoBranch)
	if err != nil {
		return "", commit
	}
	return string(raw), commit
}

// ReviewComments fetches issue-comment bodies for ABCC tag derivation.
func (c Client) ReviewComments(ctx context.Context, token string, number int) ([]string, error) {
	gh := c.gh(token)
	comments, err := gh.ListIssueComments(ctx, c.RepoOwner, c.RepoName, number)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(comments))
	for _, c := range comments {
		out = append(out, c.Body)
	}
	return out, nil
}

// PRStatus returns (merged, state, url).
func (c Client) PRStatus(ctx context.Context, token string, number int) (bool, string, string, error) {
	gh := c.gh(token)
	pr, err := gh.GetPullRequest(ctx, c.RepoOwner, c.RepoName, number)
	if err != nil {
		return false, "", "", err
	}
	return pr.Merged, pr.State, pr.HTMLURL, nil
}

// SortedDeviceKeys is a small stable-order helper for devices.
func SortedDeviceKeys(m map[string]syndication.DownloadEntry) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
