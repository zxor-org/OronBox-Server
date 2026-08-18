package astrobox

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/blob"
	"github.com/zxor-org/OronBox-Server/internal/config"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

type Client struct {
	cfg   config.AstroBoxConfig
	api   string
	http  *http.Client
	blobs blob.Store
	store *store.Store
}
type Result struct {
	PullRequest        string   `json:"pull_request"`
	PullRequestNumber  int      `json:"pull_request_number"`
	Repository         string   `json:"repository"`
	SubmissionProtocol string   `json:"submission_protocol,omitempty"`
	SubmissionPath     string   `json:"submission_path,omitempty"`
	CatalogRow         []string `json:"catalog_row,omitempty"`
	CatalogCommit      string   `json:"catalog_commit,omitempty"`
}
type PullRequestStatus struct {
	State  string `json:"state"`
	Merged bool   `json:"merged"`
	URL    string `json:"html_url"`
}

func (c *Client) PullRequest(ctx context.Context, token string, number int) (PullRequestStatus, error) {
	var status PullRequestStatus
	_, err := c.request(ctx, token, http.MethodGet, fmt.Sprintf("%s/repos/%s/%s/pulls/%d", c.api, c.cfg.RepoOwner, c.cfg.RepoName, number), nil, &status)
	return status, err
}

func New(cfg config.AstroBoxConfig, api string, blobs blob.Store, storage *store.Store) *Client {
	return &Client{cfg: cfg, api: strings.TrimRight(api, "/"), http: &http.Client{Timeout: 5 * time.Minute}, blobs: blobs, store: storage}
}

type media struct {
	Blob     string `json:"blob_sha256"`
	Role     string `json:"role"`
	Position int    `json:"position"`
}
type deviceRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type artifact struct {
	Blob     string      `json:"blob_sha256"`
	Name     string      `json:"original_name"`
	Kind     string      `json:"kind"`
	Platform string      `json:"platform"`
	Version  string      `json:"version"`
	Devices  []deviceRef `json:"devices"`
}
type snapshot struct {
	Revision struct {
		Name             string   `json:"name"`
		Summary          string   `json:"summary"`
		PurchaseLink     string   `json:"purchase_link"`
		PurchasePrice    *float64 `json:"purchase_price"`
		PurchaseCurrency string   `json:"purchase_currency"`
	} `json:"revision"`
	Media     []media    `json:"media"`
	Artifacts []artifact `json:"artifacts"`
}
type publishConfig struct {
	Agreement     bool     `json:"agreement"`
	RepoName      string   `json:"repo_name"`
	ItemID        string   `json:"item_id"`
	Tags          []string `json:"tags"`
	PaidType      string   `json:"paid_type"`
	Author        string   `json:"author"`
	BindABAccount bool     `json:"bind_ab_account"`
}

var catalogHeader = []string{"id", "name", "restype", "repo_owner", "repo_name", "repo_commit_hash", "icon", "cover", "tags", "device_vendors", "devices", "paid_type"}

const (
	submissionRootPath        = "tmp"
	submissionCSVFileName     = "resource.csv"
	submissionRequestFileName = "request.json"
	purchaseLinkIcon          = "coins"
	purchaseLinkTitle         = "购买链接"
)

func purchaseLinks(rawURL string) []map[string]string {
	purchaseURL := strings.TrimSpace(rawURL)
	if purchaseURL == "" {
		return []map[string]string{}
	}
	return []map[string]string{{
		"title": purchaseLinkTitle,
		"url":   purchaseURL,
		"icon":  purchaseLinkIcon,
	}}
}

func buildManifest(
	snap snapshot,
	cfg publishConfig,
	restype, iconPath, coverPath string,
	authors []map[string]any,
	previews []string,
	downloads map[string]map[string]string,
) map[string]any {
	return map[string]any{
		"item": map[string]any{
			"id":          cfg.ItemID,
			"restype":     restype,
			"name":        snap.Revision.Name,
			"description": snap.Revision.Summary,
			"preview":     previews,
			"icon":        iconPath,
			"cover":       coverPath,
			"author":      authors,
		},
		"links":     purchaseLinks(snap.Revision.PurchaseLink),
		"downloads": downloads,
		"ext":       map[string]any{},
	}
}

type submissionRequest struct {
	SchemaVersion     int     `json:"schema_version"`
	Mode              string  `json:"mode"`
	OriginalID        *string `json:"original_id"`
	BaseEntryDigest   *string `json:"base_entry_digest"`
	BaseCatalogCommit *string `json:"base_catalog_commit"`
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

func (c *Client) Publish(ctx context.Context, token, ownerName string, rawSnapshot, rawConfig []byte) (Result, error) {
	var snap snapshot
	var cfg publishConfig
	if err := json.Unmarshal(rawSnapshot, &snap); err != nil {
		return Result{}, err
	}
	if err := json.Unmarshal(rawConfig, &cfg); err != nil {
		return Result{}, err
	}
	if !cfg.Agreement || cfg.ItemID == "" {
		return Result{}, fmt.Errorf("AstroBox agreement and item_id are required")
	}
	if len(cfg.Tags) == 0 {
		return Result{}, fmt.Errorf("AstroBox publication requires at least one tag")
	}
	cfg.PaidType = normalizePaid(cfg.PaidType)
	if cfg.PaidType != "" && cfg.PaidType != "paid" && cfg.PaidType != "force_paid" {
		return Result{}, fmt.Errorf("invalid AstroBox paid_type %q", cfg.PaidType)
	}
	if len(snap.Artifacts) == 0 {
		return Result{}, fmt.Errorf("AstroBox publication has no artifact")
	}
	for _, item := range snap.Artifacts {
		if item.Platform != "vela_os" {
			return Result{}, fmt.Errorf("AstroBox does not support %s resources", item.Platform)
		}
	}
	if len(uniqueDevices(snap.Artifacts)) == 0 {
		return Result{}, fmt.Errorf("AstroBox publication has no device supported by devices_v2.json")
	}
	restype := ""
	for _, item := range snap.Artifacts {
		value := ""
		switch item.Kind {
		case "quickapp":
			value = "quick_app"
		case "watchface":
			value = "watchface"
		default:
			return Result{}, fmt.Errorf("AstroBox does not support resource kind %s", item.Kind)
		}
		if restype == "" {
			restype = value
		} else if restype != value {
			return Result{}, fmt.Errorf("AstroBox publication cannot mix resource kinds")
		}
	}
	repoName := strings.TrimSpace(cfg.RepoName)
	if repoName == "" {
		return Result{}, fmt.Errorf("AstroBox repo_name is required")
	}
	if err := validateRepoName(repoName); err != nil {
		return Result{}, err
	}
	if err := validateCatalogRow([]string{cfg.ItemID, snap.Revision.Name, restype, "", repoName, "", "", "", strings.Join(cfg.Tags, ";"), strings.Join(uniqueVendors(snap.Artifacts), ";"), strings.Join(uniqueDevices(snap.Artifacts), ";"), cfg.PaidType}); err != nil {
		return Result{}, err
	}
	repo, err := c.ensureRepo(ctx, token, repoName, "AstroBox resource of "+snap.Revision.Name)
	if err != nil {
		return Result{}, fmt.Errorf("prepare AstroBox resource repository: %w", err)
	}
	files := map[string][]byte{}
	var previews []string
	iconPath, coverPath := "", ""
	for index, item := range snap.Media {
		record, data, err := c.readBlob(ctx, item.Blob)
		if err != nil {
			return Result{}, err
		}
		extension := extensionFor(record.MediaType)
		filePath := "media/preview-" + strconv.Itoa(index) + extension
		switch item.Role {
		case "icon":
			filePath = "media/icon" + extension
			iconPath = filePath
		case "cover":
			filePath = "media/cover" + extension
			coverPath = filePath
		case "preview":
			previews = append(previews, filePath)
		}
		files[filePath] = data
	}
	if iconPath == "" || coverPath == "" {
		return Result{}, fmt.Errorf("AstroBox icon and cover are required")
	}
	downloads := map[string]map[string]string{}
	var entries []downloadEntry
	for index, item := range snap.Artifacts {
		devices := supportedArtifactDevices(item.Devices)
		if len(devices) == 0 {
			continue
		}
		_, data, err := c.readBlob(ctx, item.Blob)
		if err != nil {
			return Result{}, err
		}
		filePath := "downloads/" + strconv.Itoa(index) + "-" + path.Base(strings.ReplaceAll(item.Name, "\\", "/"))
		files[filePath] = data
		entries = append(entries, downloadEntry{File: filePath, Version: item.Version, SHA256: item.Blob, Devices: devices})
		for _, device := range devices {
			downloads[device.ID] = map[string]string{"version": item.Version, "file_name": filePath, "sha256": item.Blob}
		}
	}
	if len(entries) == 0 {
		return Result{}, fmt.Errorf("AstroBox publication has no device supported by devices_v2.json")
	}
	author := strings.TrimSpace(cfg.Author)
	if author == "" {
		author = ownerName
	}
	authors := []map[string]any{{"name": author, "bindABAccount": cfg.BindABAccount}}
	manifest := buildManifest(snap, cfg, restype, iconPath, coverPath, authors, previews, downloads)
	files["manifest_v2.json"], _ = json.MarshalIndent(manifest, "", "  ")
	files["README.md"] = []byte(buildREADME(snap, cfg, restype, author, coverPath, previews, entries))
	commit, err := c.uploadFiles(ctx, token, repo, "Publish "+snap.Revision.Name, files)
	if err != nil {
		return Result{}, fmt.Errorf("upload AstroBox resource repository: %w", err)
	}
	snapshot, err := c.prepareCatalogSnapshot(ctx, token)
	if err != nil {
		return Result{}, err
	}
	fork, catalog, catalogCommit, forkBaseSHA := snapshot.Fork, snapshot.Catalog, snapshot.Commit, snapshot.ForkBase
	rows, err := csv.NewReader(bytes.NewReader(catalog)).ReadAll()
	if err != nil && strings.TrimSpace(string(catalog)) != "" {
		return Result{}, fmt.Errorf("parse AstroBox catalog: %w", err)
	}
	if len(rows) == 0 {
		rows = append(rows, catalogHeader)
	}
	devices := uniqueDevices(snap.Artifacts)
	shortCommit := commit
	if len(shortCommit) > 7 {
		shortCommit = shortCommit[:7]
	}
	line := []string{cfg.ItemID, snap.Revision.Name, restype, repo.Owner, repo.Name, shortCommit, iconPath, coverPath, strings.Join(cfg.Tags, ";"), strings.Join(uniqueVendors(snap.Artifacts), ";"), strings.Join(devices, ";"), cfg.PaidType}
	if err := validateCatalogRow(line); err != nil {
		return Result{}, err
	}
	mode := "create"
	var originalID, baseDigest *string
	for index := 1; index < len(rows); index++ {
		if len(rows[index]) == 0 || rows[index][0] != cfg.ItemID {
			continue
		}
		if len(rows[index]) < len(catalogHeader) || !strings.EqualFold(rows[index][3], repo.Owner) || !strings.EqualFold(rows[index][4], repo.Name) {
			return Result{}, fmt.Errorf("AstroBox item_id %q is already bound to another repository", cfg.ItemID)
		}
		mode = "edit"
		id := cfg.ItemID
		originalID = &id
		digest, err := catalogRowDigest(rows[index])
		if err != nil {
			return Result{}, err
		}
		baseDigest = &digest
		break
	}
	request := submissionRequest{SchemaVersion: 1, Mode: mode, OriginalID: originalID, BaseEntryDigest: baseDigest}
	if mode == "edit" {
		request.BaseCatalogCommit = stringPtr(catalogCommit)
	}
	requestJSON, err := json.MarshalIndent(request, "", "  ")
	if err != nil {
		return Result{}, err
	}
	resourceCSV, err := encodeCatalogRows([][]string{catalogHeader, line})
	if err != nil {
		return Result{}, err
	}
	login, err := c.currentUser(ctx, token)
	if err != nil {
		return Result{}, fmt.Errorf("read GitHub publisher identity: %w", err)
	}
	submissionPath, err := c.submissionPath(login, repo.Name)
	if err != nil {
		return Result{}, err
	}
	branch := "oronbox-resource-" + sanitize(cfg.ItemID) + "-" + strconv.FormatInt(time.Now().UTC().Unix(), 10)
	if err := c.createRef(ctx, token, fork.Owner, fork.Name, branch, forkBaseSHA); err != nil {
		return Result{}, fmt.Errorf("create AstroBox submission branch: %w", err)
	}
	if _, err := c.uploadFiles(ctx, token, forkForBranch(fork, branch), "Submit "+snap.Revision.Name, map[string][]byte{
		path.Join(submissionPath, submissionCSVFileName):     resourceCSV,
		path.Join(submissionPath, submissionRequestFileName): requestJSON,
	}); err != nil {
		return Result{}, fmt.Errorf("upload AstroBox submission files: %w", err)
	}
	operation := "Add"
	if mode == "edit" {
		operation = "Update"
	}
	body := buildPRBody(snap, cfg, restype, repo, shortCommit, iconPath, coverPath, previews, entries)
	pr, err := c.createPR(ctx, token, "[OBCC] "+operation+" "+snap.Revision.Name, fork.Owner+":"+branch, c.cfg.RepoBranch, body)
	if err != nil {
		return Result{}, fmt.Errorf("create AstroBox pull request: %w", err)
	}
	return Result{PullRequest: pr.URL, PullRequestNumber: pr.Number, Repository: "https://github.com/" + repo.Owner + "/" + repo.Name, SubmissionProtocol: "v2", SubmissionPath: submissionPath, CatalogRow: line, CatalogCommit: catalogCommit}, nil
}

type repo struct {
	Owner  string
	Name   string
	Branch string
}

type catalogSnapshot struct {
	Fork     repo
	Catalog  []byte
	FileSHA  string
	Commit   string
	ForkBase string
}

// prepareCatalogSnapshot reads a stable upstream catalog snapshot and a
// readable fork base for the submission branch. Fork-main synchronization is
// best-effort: AstroBox Creator Console continues from the fork's current HEAD
// when GitHub refuses to update the default ref, because v2 submissions carry
// their catalog row and upstream base commit in tmp/.../request.json instead of
// rewriting the fork catalog itself.
func (c *Client) prepareCatalogSnapshot(ctx context.Context, token string) (catalogSnapshot, error) {
	fork, err := c.ensureFork(ctx, token)
	if err != nil {
		return catalogSnapshot{}, fmt.Errorf("prepare AstroBox catalog fork: %w", err)
	}
	// Match AstroBox Creator Console: default-branch synchronization is a
	// convenience, not a prerequisite for creating the staging branch.
	_ = c.syncFork(ctx, token, fork)
	for attempt := 0; attempt < 3; attempt++ {
		commit, err := c.refSHA(ctx, token, c.cfg.RepoOwner, c.cfg.RepoName, c.cfg.RepoBranch)
		if err != nil {
			return catalogSnapshot{}, fmt.Errorf("read AstroBox upstream branch: %w", err)
		}
		forkBase, err := c.refSHA(ctx, token, fork.Owner, fork.Name, fork.Branch)
		if err != nil {
			return catalogSnapshot{}, fmt.Errorf("read AstroBox catalog fork branch: %w", err)
		}
		catalog, fileSHA, err := c.getContent(ctx, token, c.cfg.RepoOwner, c.cfg.RepoName, c.cfg.CatalogPath, commit)
		if err != nil {
			return catalogSnapshot{}, fmt.Errorf("read AstroBox catalog: %w", err)
		}
		latestCommit, err := c.refSHA(ctx, token, c.cfg.RepoOwner, c.cfg.RepoName, c.cfg.RepoBranch)
		if err != nil {
			return catalogSnapshot{}, fmt.Errorf("recheck AstroBox upstream branch: %w", err)
		}
		if latestCommit != commit {
			continue
		}
		return catalogSnapshot{Fork: fork, Catalog: catalog, FileSHA: fileSHA, Commit: commit, ForkBase: forkBase}, nil
	}
	return catalogSnapshot{}, fmt.Errorf("AstroBox catalog changed while preparing a submission; retry the publication")
}

// Remove submits a pull request that drops itemID from the catalog index.
// The resource repository itself is left untouched. An item that is not
// listed is a no-op and returns an empty Result.
func (c *Client) Remove(ctx context.Context, token, itemID, name string) (Result, error) {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return Result{}, fmt.Errorf("AstroBox item_id is required")
	}
	snapshot, err := c.prepareCatalogSnapshot(ctx, token)
	if err != nil {
		return Result{}, err
	}
	fork, catalog, sha, _, forkBaseSHA := snapshot.Fork, snapshot.Catalog, snapshot.FileSHA, snapshot.Commit, snapshot.ForkBase
	rows, err := csv.NewReader(bytes.NewReader(catalog)).ReadAll()
	if err != nil {
		return Result{}, fmt.Errorf("parse AstroBox catalog: %w", err)
	}
	kept := make([][]string, 0, len(rows))
	found := false
	for index, row := range rows {
		if index > 0 && len(row) > 0 && row[0] == itemID {
			found = true
			continue
		}
		kept = append(kept, row)
	}
	if !found {
		return Result{}, nil
	}
	branch := "oronbox-remove-" + sanitize(itemID) + "-" + strconv.FormatInt(time.Now().UTC().Unix(), 10)
	if err := c.createRef(ctx, token, fork.Owner, fork.Name, branch, forkBaseSHA); err != nil {
		return Result{}, fmt.Errorf("create AstroBox removal branch: %w", err)
	}
	var encodedCatalog bytes.Buffer
	if err := csv.NewWriter(&encodedCatalog).WriteAll(kept); err != nil {
		return Result{}, err
	}
	if err := c.putContentWithSHA(ctx, token, fork.Owner, fork.Name, c.cfg.CatalogPath, branch, "Remove "+itemID, encodedCatalog.Bytes(), sha); err != nil {
		return Result{}, err
	}
	title := "[OBCC] Remove " + itemID
	if strings.TrimSpace(name) != "" {
		title = "[OBCC] Remove " + strings.TrimSpace(name)
	}
	body := fmt.Sprintf("## 删除资源\n\n- 资源名称：%s\n- 资源 ID：`%s`\n\n---\n\n此 PR 由 [OronBox 创作者中心](https://oronbox.zxor.org) 提交。Submitted through OronBox Creator Center.\n", name, itemID)
	pr, err := c.createPR(ctx, token, title, fork.Owner+":"+branch, c.cfg.RepoBranch, body)
	if err != nil {
		return Result{}, fmt.Errorf("create AstroBox removal pull request: %w", err)
	}
	return Result{PullRequest: pr.URL, PullRequestNumber: pr.Number}, nil
}

type downloadEntry struct {
	File    string
	Version string
	SHA256  string
	Devices []deviceRef
}

func paidLabel(paidType string) string {
	if paidType == "" {
		return "免费"
	}
	return paidType
}

func restypeLabel(restype string) string {
	switch restype {
	case "quick_app":
		return "快应用（quick_app）"
	case "watchface":
		return "表盘（watchface）"
	default:
		return restype
	}
}

func deviceLabel(device deviceRef) string {
	if device.Name == "" || device.Name == device.ID {
		return device.ID
	}
	return device.Name + "（" + device.ID + "）"
}

func uniqueDeviceRefs(entries []downloadEntry) []deviceRef {
	seen := map[string]bool{}
	var result []deviceRef
	for _, entry := range entries {
		for _, device := range entry.Devices {
			if !seen[device.ID] {
				seen[device.ID] = true
				result = append(result, device)
			}
		}
	}
	return result
}

func buildREADME(snap snapshot, cfg publishConfig, restype, author, coverPath string, previews []string, entries []downloadEntry) string {
	var output strings.Builder
	fmt.Fprintf(&output, "# %s\n\n", snap.Revision.Name)
	if summary := strings.TrimSpace(snap.Revision.Summary); summary != "" {
		fmt.Fprintf(&output, "%s\n\n", summary)
	}
	fmt.Fprintf(&output, "![Cover](%s)\n\n", coverPath)
	output.WriteString("|  |  |\n| --- | --- |\n")
	fmt.Fprintf(&output, "| 资源 ID | `%s` |\n", cfg.ItemID)
	fmt.Fprintf(&output, "| 类型 | %s |\n", restypeLabel(restype))
	fmt.Fprintf(&output, "| 作者 | %s |\n", author)
	fmt.Fprintf(&output, "| 付费类型 | %s |\n", paidLabel(cfg.PaidType))
	if purchaseLink := strings.TrimSpace(snap.Revision.PurchaseLink); purchaseLink != "" {
		fmt.Fprintf(&output, "| 购买链接 | [%s](%s) |\n", purchaseLink, purchaseLink)
	}
	if len(cfg.Tags) > 0 {
		fmt.Fprintf(&output, "| 标签 | %s |\n", strings.Join(cfg.Tags, " / "))
	}
	output.WriteString("\n## 支持设备\n\n")
	for _, device := range uniqueDeviceRefs(entries) {
		fmt.Fprintf(&output, "- %s\n", deviceLabel(device))
	}
	output.WriteString("\n## 下载\n\n| 文件 | 版本 | 设备 |\n| --- | --- | --- |\n")
	for _, entry := range entries {
		devices := make([]string, 0, len(entry.Devices))
		for _, device := range entry.Devices {
			devices = append(devices, deviceLabel(device))
		}
		fmt.Fprintf(&output, "| [%s](%s) | %s | %s |\n", path.Base(entry.File), entry.File, entry.Version, strings.Join(devices, "<br>"))
	}
	if len(previews) > 0 {
		output.WriteString("\n## 预览\n\n")
		for _, preview := range previews {
			fmt.Fprintf(&output, "![Preview](%s)\n", preview)
		}
	}
	output.WriteString("\n---\n\n本仓库由 [OronBox 创作者中心](https://oronbox.zxor.org) 创建和维护。This repository is created and maintained by OronBox Creator Center.\n")
	return output.String()
}

func buildPRBody(snap snapshot, cfg publishConfig, restype string, repo repo, shortCommit, iconPath, coverPath string, previews []string, entries []downloadEntry) string {
	rawURL := func(filePath string) string {
		return "https://raw.githubusercontent.com/" + repo.Owner + "/" + repo.Name + "/refs/heads/" + repo.Branch + "/" + filePath
	}
	var output strings.Builder
	output.WriteString("## 资源信息\n\n")
	fmt.Fprintf(&output, "- 资源名称：%s\n", snap.Revision.Name)
	fmt.Fprintf(&output, "- 资源 ID：%s\n", cfg.ItemID)
	fmt.Fprintf(&output, "- 资源类型：%s\n", restypeLabel(restype))
	output.WriteString("- 提交清单：manifest_v2\n")
	fmt.Fprintf(&output, "- 付费类型：%s\n", paidLabel(cfg.PaidType))
	if purchaseLink := strings.TrimSpace(snap.Revision.PurchaseLink); purchaseLink != "" {
		fmt.Fprintf(&output, "- 购买链接：%s\n", purchaseLink)
	}
	if len(cfg.Tags) > 0 {
		fmt.Fprintf(&output, "- 标签：%s\n", strings.Join(cfg.Tags, " / "))
	}
	output.WriteString("\n## 支持设备\n\n")
	for _, device := range uniqueDeviceRefs(entries) {
		if device.Name == "" || device.Name == device.ID {
			fmt.Fprintf(&output, "- %s\n", device.ID)
		} else {
			fmt.Fprintf(&output, "- %s（device_id: %s）\n", device.Name, device.ID)
		}
	}
	fmt.Fprintf(&output, "\n## 仓库信息\n\n- 资源仓库：https://github.com/%s/%s\n- 提交短哈希：%s\n", repo.Owner, repo.Name, shortCommit)
	output.WriteString("\n## 图片资源\n\n")
	fmt.Fprintf(&output, "- Icon：%s\n  %s\n", iconPath, rawURL(iconPath))
	fmt.Fprintf(&output, "- Cover：%s\n  %s\n", coverPath, rawURL(coverPath))
	if len(previews) > 0 {
		output.WriteString("- Preview：\n")
		for _, preview := range previews {
			fmt.Fprintf(&output, "  - %s\n    %s\n", preview, rawURL(preview))
		}
	}
	output.WriteString("\n## 下载资源（downloads）\n\n")
	for _, entry := range entries {
		fmt.Fprintf(&output, "- %s\n", entry.File)
		fmt.Fprintf(&output, "  - version: %s\n", entry.Version)
		devices := make([]string, 0, len(entry.Devices))
		for _, device := range entry.Devices {
			devices = append(devices, deviceLabel(device))
		}
		fmt.Fprintf(&output, "  - devices: %s\n", strings.Join(devices, "、"))
		fmt.Fprintf(&output, "  - raw: %s\n", rawURL(entry.File))
		fmt.Fprintf(&output, "  - sha256: %s\n", entry.SHA256)
	}
	output.WriteString("\n---\n\n此 PR 由 [OronBox 创作者中心](https://oronbox.zxor.org) 提交。Submitted through OronBox Creator Center.\n")
	return output.String()
}

func (c *Client) ensureRepo(ctx context.Context, token, name, description string) (repo, error) {
	var response struct {
		Name          string `json:"name"`
		DefaultBranch string `json:"default_branch"`
		Owner         struct {
			Login string `json:"login"`
		} `json:"owner"`
	}
	status, err := c.request(ctx, token, http.MethodPost, c.api+"/user/repos", map[string]any{"name": name, "description": description, "auto_init": true}, &response)
	if err != nil && status != 422 {
		return repo{}, err
	}
	if status == 422 {
		user, err := c.currentUser(ctx, token)
		if err != nil {
			return repo{}, err
		}
		_, err = c.request(ctx, token, http.MethodGet, c.api+"/repos/"+user+"/"+name, nil, &response)
		if err != nil {
			return repo{}, err
		}
	}
	if response.DefaultBranch == "" {
		response.DefaultBranch = "main"
	}
	return repo{Owner: response.Owner.Login, Name: response.Name, Branch: response.DefaultBranch}, nil
}
func (c *Client) currentUser(ctx context.Context, token string) (string, error) {
	var response struct {
		Login string `json:"login"`
	}
	_, err := c.request(ctx, token, http.MethodGet, c.api+"/user", nil, &response)
	return response.Login, err
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
	encoded, err := encodeCatalogRows([][]string{row})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(encoded)), nil
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

func stringPtr(value string) *string {
	return &value
}

func forkForBranch(value repo, branch string) repo {
	value.Branch = branch
	return value
}

func (c *Client) ensureFork(ctx context.Context, token string) (repo, error) {
	var response struct {
		Name          string `json:"name"`
		DefaultBranch string `json:"default_branch"`
		Owner         struct {
			Login string `json:"login"`
		} `json:"owner"`
	}
	status, err := c.request(ctx, token, http.MethodPost, fmt.Sprintf("%s/repos/%s/%s/forks", c.api, c.cfg.RepoOwner, c.cfg.RepoName), map[string]any{}, &response)
	if err != nil && status != http.StatusUnprocessableEntity {
		return repo{}, err
	}
	if status == http.StatusUnprocessableEntity {
		owner, err := c.currentUser(ctx, token)
		if err != nil {
			return repo{}, err
		}
		if _, err := c.request(ctx, token, http.MethodGet, fmt.Sprintf("%s/repos/%s/%s", c.api, owner, c.cfg.RepoName), nil, &response); err != nil {
			return repo{}, err
		}
	}
	if response.DefaultBranch == "" {
		response.DefaultBranch = c.cfg.RepoBranch
	}
	fork := repo{Owner: response.Owner.Login, Name: response.Name, Branch: response.DefaultBranch}
	// The fork is synchronized immediately before every submission. Creating
	// the temporary branch from the fork HEAD keeps the commit in the fork's
	// object database and avoids GitHub's 404 for an upstream-only SHA.
	var lastErr error
	for i := 0; i < 10; i++ {
		if _, err := c.refSHA(ctx, token, fork.Owner, fork.Name, fork.Branch); err == nil {
			return fork, nil
		} else {
			status, ok := githubStatus(err)
			if !ok || (status != http.StatusNotFound && status != http.StatusConflict) {
				return repo{}, fmt.Errorf("wait for AstroBox catalog fork: %w", err)
			}
			lastErr = err
		}
		time.Sleep(1500 * time.Millisecond)
	}
	if lastErr != nil {
		return repo{}, fmt.Errorf("AstroBox catalog fork did not become ready: %w", lastErr)
	}
	return repo{}, fmt.Errorf("AstroBox catalog fork did not become ready")
}

func (c *Client) syncFork(ctx context.Context, token string, fork repo) error {
	// Match AstroBox Creator Console: try the regular GitHub sync first, but
	// continue to the explicit ref alignment when GitHub reports a conflict or
	// the fork has diverged.
	_, mergeErr := c.request(ctx, token, http.MethodPost, fmt.Sprintf("%s/repos/%s/%s/merge-upstream", c.api, fork.Owner, fork.Name), map[string]string{"branch": fork.Branch}, nil)
	upstreamSHA, err := c.refSHA(ctx, token, c.cfg.RepoOwner, c.cfg.RepoName, c.cfg.RepoBranch)
	if err != nil {
		return fmt.Errorf("read AstroBox upstream branch after synchronization: %w", err)
	}
	forkSHA, err := c.refSHA(ctx, token, fork.Owner, fork.Name, fork.Branch)
	if err != nil {
		if status, ok := githubStatus(err); ok && status == http.StatusNotFound {
			if createErr := c.createRef(ctx, token, fork.Owner, fork.Name, fork.Branch, upstreamSHA); createErr == nil {
				return nil
			} else {
				return fmt.Errorf("read AstroBox fork branch after synchronization: %w; create missing ref: %v", err, createErr)
			}
		}
		return fmt.Errorf("read AstroBox fork branch after synchronization: %w", err)
	}
	if forkSHA == upstreamSHA {
		return nil
	}

	if err := c.forceAlignForkRef(ctx, token, fork, upstreamSHA); err != nil {
		if mergeErr != nil {
			return fmt.Errorf("force-align AstroBox catalog fork after merge-upstream failed: %w", err)
		}
		return fmt.Errorf("force-align AstroBox catalog fork: %w", err)
	}
	alignedSHA, err := c.refSHA(ctx, token, fork.Owner, fork.Name, fork.Branch)
	if err != nil {
		return fmt.Errorf("verify AstroBox fork alignment: %w", err)
	}
	if alignedSHA != upstreamSHA {
		return fmt.Errorf("AstroBox catalog fork remains stale after force alignment")
	}
	return nil
}

// forceAlignForkRef updates the fork's default branch to the upstream commit.
// GitHub may briefly return 404 while a fork synchronization is being
// materialized, and an old fork may genuinely be missing the configured ref.
// Re-read the ref once before creating it so a transient response does not
// create a duplicate branch, then use the Git refs API as the missing-ref
// recovery path.
func (c *Client) forceAlignForkRef(ctx context.Context, token string, fork repo, sha string) error {
	updateErr := c.updateRef(ctx, token, fork.Owner, fork.Name, fork.Branch, sha)
	if updateErr == nil {
		return nil
	}
	status, ok := githubStatus(updateErr)
	if !ok || status != http.StatusNotFound {
		return updateErr
	}

	if currentSHA, err := c.refSHA(ctx, token, fork.Owner, fork.Name, fork.Branch); err == nil {
		if currentSHA == sha {
			return nil
		}
		if retryErr := c.updateRef(ctx, token, fork.Owner, fork.Name, fork.Branch, sha); retryErr == nil {
			return nil
		} else {
			updateErr = fmt.Errorf("initial update failed: %w; retry failed: %v", updateErr, retryErr)
		}
	} else {
		refStatus, refKnown := githubStatus(err)
		if !refKnown || refStatus != http.StatusNotFound {
			return fmt.Errorf("update fork ref: %w; re-read ref: %v", updateErr, err)
		}
	}

	if err := c.createRef(ctx, token, fork.Owner, fork.Name, fork.Branch, sha); err != nil {
		return fmt.Errorf("update fork ref: %w; create missing ref: %v", updateErr, err)
	}
	return nil
}

func (c *Client) refSHA(ctx context.Context, token, owner, repoName, branch string) (string, error) {
	var response struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	_, err := c.request(ctx, token, http.MethodGet, fmt.Sprintf("%s/repos/%s/%s/git/ref/heads/%s", c.api, owner, repoName, url.PathEscape(branch)), nil, &response)
	return response.Object.SHA, err
}
func (c *Client) createRef(ctx context.Context, token, owner, repoName, branch, sha string) error {
	_, err := c.request(ctx, token, http.MethodPost, fmt.Sprintf("%s/repos/%s/%s/git/refs", c.api, owner, repoName), map[string]any{"ref": "refs/heads/" + branch, "sha": sha}, nil)
	return err
}

// uploadFiles lands every file in one Git Data API commit, the same way
// AstroBox Creator Console publishes (blobs -> tree -> commit -> ref update).
func (c *Client) uploadFiles(ctx context.Context, token string, target repo, message string, files map[string][]byte) (string, error) {
	parentSHA, err := c.refSHA(ctx, token, target.Owner, target.Name, target.Branch)
	if err != nil {
		first := ""
		var firstData []byte
		for filePath, data := range files {
			first, firstData = filePath, data
			break
		}
		if err := c.putContent(ctx, token, target.Owner, target.Name, first, target.Branch, "Initialize repository", firstData); err != nil {
			return "", err
		}
		delete(files, first)
		if parentSHA, err = c.refSHA(ctx, token, target.Owner, target.Name, target.Branch); err != nil {
			return "", err
		}
	}
	baseTree, err := c.commitTree(ctx, token, target.Owner, target.Name, parentSHA)
	if err != nil {
		return "", err
	}
	paths := make([]string, 0, len(files))
	for filePath := range files {
		paths = append(paths, filePath)
	}
	sort.Strings(paths)
	entries := make([]treeEntry, 0, len(files))
	for _, filePath := range paths {
		sha, err := c.createBlob(ctx, token, target.Owner, target.Name, files[filePath])
		if err != nil {
			return "", err
		}
		entries = append(entries, treeEntry{Path: filePath, Mode: "100644", Type: "blob", SHA: sha})
	}
	treeSHA, err := c.createTree(ctx, token, target.Owner, target.Name, baseTree, entries)
	if err != nil {
		return "", err
	}
	commitSHA, err := c.createCommit(ctx, token, target.Owner, target.Name, message, treeSHA, parentSHA)
	if err != nil {
		return "", err
	}
	if err := c.updateRef(ctx, token, target.Owner, target.Name, target.Branch, commitSHA); err != nil {
		return "", err
	}
	return commitSHA, nil
}

type treeEntry struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
}

func (c *Client) createBlob(ctx context.Context, token, owner, repoName string, data []byte) (string, error) {
	var response struct {
		SHA string `json:"sha"`
	}
	_, err := c.request(ctx, token, http.MethodPost, fmt.Sprintf("%s/repos/%s/%s/git/blobs", c.api, owner, repoName), map[string]any{"content": base64.StdEncoding.EncodeToString(data), "encoding": "base64"}, &response)
	return response.SHA, err
}
func (c *Client) createTree(ctx context.Context, token, owner, repoName, baseTree string, entries []treeEntry) (string, error) {
	var response struct {
		SHA string `json:"sha"`
	}
	body := map[string]any{"tree": entries}
	if baseTree != "" {
		body["base_tree"] = baseTree
	}
	_, err := c.request(ctx, token, http.MethodPost, fmt.Sprintf("%s/repos/%s/%s/git/trees", c.api, owner, repoName), body, &response)
	return response.SHA, err
}
func (c *Client) createCommit(ctx context.Context, token, owner, repoName, message, tree, parent string) (string, error) {
	var response struct {
		SHA string `json:"sha"`
	}
	parents := []string{}
	if parent != "" {
		parents = []string{parent}
	}
	_, err := c.request(ctx, token, http.MethodPost, fmt.Sprintf("%s/repos/%s/%s/git/commits", c.api, owner, repoName), map[string]any{"message": message, "tree": tree, "parents": parents}, &response)
	return response.SHA, err
}
func (c *Client) updateRef(ctx context.Context, token, owner, repoName, branch, sha string) error {
	_, err := c.request(ctx, token, http.MethodPatch, fmt.Sprintf("%s/repos/%s/%s/git/refs/heads/%s", c.api, owner, repoName, url.PathEscape(branch)), map[string]any{"sha": sha, "force": true}, nil)
	return err
}
func (c *Client) commitTree(ctx context.Context, token, owner, repoName, commitSHA string) (string, error) {
	var response struct {
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	}
	_, err := c.request(ctx, token, http.MethodGet, fmt.Sprintf("%s/repos/%s/%s/git/commits/%s", c.api, owner, repoName, commitSHA), nil, &response)
	return response.Tree.SHA, err
}

func (c *Client) getContent(ctx context.Context, token, owner, repoName, filePath, branch string) ([]byte, string, error) {
	var response struct {
		Content string `json:"content"`
		SHA     string `json:"sha"`
	}
	endpoint := fmt.Sprintf("%s/repos/%s/%s/contents/%s?ref=%s", c.api, owner, repoName, filePath, url.QueryEscape(branch))
	_, err := c.request(ctx, token, http.MethodGet, endpoint, nil, &response)
	if err != nil {
		return nil, "", err
	}
	data, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(response.Content, "\n", ""))
	return data, response.SHA, err
}
func (c *Client) putContent(ctx context.Context, token, owner, repoName, filePath, branch, message string, data []byte) error {
	_, sha, err := c.getContent(ctx, token, owner, repoName, filePath, branch)
	if err != nil {
		status, ok := githubStatus(err)
		if !ok || status != http.StatusNotFound {
			return err
		}
	}
	return c.putContentWithSHA(ctx, token, owner, repoName, filePath, branch, message, data, sha)
}
func (c *Client) putContentWithSHA(ctx context.Context, token, owner, repoName, filePath, branch, message string, data []byte, sha string) error {
	body := map[string]any{"message": message, "content": base64.StdEncoding.EncodeToString(data), "branch": branch}
	if sha != "" {
		body["sha"] = sha
	}
	_, err := c.request(ctx, token, http.MethodPut, fmt.Sprintf("%s/repos/%s/%s/contents/%s", c.api, owner, repoName, filePath), body, nil)
	return err
}

type pr struct {
	Number int    `json:"number"`
	URL    string `json:"html_url"`
}

func (c *Client) createPR(ctx context.Context, token, title, head, base, body string) (pr, error) {
	var response pr
	_, err := c.request(ctx, token, http.MethodPost, fmt.Sprintf("%s/repos/%s/%s/pulls", c.api, c.cfg.RepoOwner, c.cfg.RepoName), map[string]any{"title": title, "head": head, "base": base, "body": body}, &response)
	return response, err
}
func (c *Client) readBlob(ctx context.Context, sha string) (store.BlobRecord, []byte, error) {
	record, err := c.store.Blob(ctx, sha)
	if err != nil {
		return record, nil, err
	}
	reader, err := c.blobs.Open(ctx, record.LocalKey)
	if err != nil {
		return record, nil, err
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	return record, data, err
}
func (c *Client) request(ctx context.Context, token, method, endpoint string, body any, destination any) (int, error) {
	var stream io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		stream = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, stream)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		return resp.StatusCode, &githubRequestError{
			status:   resp.StatusCode,
			method:   method,
			endpoint: endpoint,
			body:     string(body),
		}
	}
	if destination != nil {
		return resp.StatusCode, json.NewDecoder(resp.Body).Decode(destination)
	}
	return resp.StatusCode, nil
}
func sanitize(value string) string {
	value = strings.ToLower(value)
	var output strings.Builder
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			output.WriteRune(r)
		} else {
			output.WriteByte('-')
		}
	}
	return strings.Trim(output.String(), "-")
}

func validateRepoName(value string) error {
	if len(value) > 100 {
		return fmt.Errorf("GitHub repository name cannot exceed 100 characters")
	}
	for index, r := range value {
		valid := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-'
		if !valid || index == 0 && !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return fmt.Errorf("invalid GitHub repository name %q", value)
		}
	}
	last := value[len(value)-1]
	if !(last >= 'a' && last <= 'z' || last >= 'A' && last <= 'Z' || last >= '0' && last <= '9') {
		return fmt.Errorf("invalid GitHub repository name %q", value)
	}
	return nil
}
func extensionFor(mediaType string) string {
	switch mediaType {
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	default:
		return ".png"
	}
}
func uniqueDevices(artifacts []artifact) []string {
	seen := map[string]bool{}
	var result []string
	for _, item := range artifacts {
		for _, device := range supportedArtifactDevices(item.Devices) {
			if !seen[device.ID] {
				seen[device.ID] = true
				result = append(result, device.ID)
			}
		}
	}
	return result
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

type githubRequestError struct {
	status   int
	method   string
	endpoint string
	body     string
}

func (e *githubRequestError) Error() string {
	displayEndpoint := e.endpoint
	if parsed, err := url.Parse(e.endpoint); err == nil {
		displayEndpoint = parsed.Path
		if parsed.RawQuery != "" {
			displayEndpoint += "?" + parsed.RawQuery
		}
	}
	detail := strings.TrimSpace(e.body)
	if detail == "" {
		detail = "no response body"
	}
	if len(detail) > 512 {
		detail = detail[:512] + "..."
	}
	return fmt.Sprintf("GitHub API %s %s returned HTTP %d: %s", e.method, displayEndpoint, e.status, detail)
}

func githubStatus(err error) (int, bool) {
	var requestError *githubRequestError
	if !errors.As(err, &requestError) {
		return 0, false
	}
	return requestError.status, true
}
func normalizePaid(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "free") {
		return ""
	}
	return strings.TrimSpace(value)
}
