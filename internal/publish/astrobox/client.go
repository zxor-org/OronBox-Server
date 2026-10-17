package astrobox

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"path"
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
		Description      string   `json:"description"`
		PurchaseLink     string   `json:"purchase_link"`
		PurchasePrice    *float64 `json:"purchase_price"`
		PurchaseCurrency string   `json:"purchase_currency"`
		Links            []struct {
			Title string `json:"title"`
			URL   string `json:"url"`
		} `json:"links"`
	} `json:"revision"`
	Media     []media    `json:"media"`
	Artifacts []artifact `json:"artifacts"`
}

type publishConfig struct {
	Agreement         bool     `json:"agreement"`
	RepoName          string   `json:"repo_name"`
	ItemID            string   `json:"item_id"`
	Tags              []string `json:"tags"`
	PaidType          string   `json:"paid_type"`
	Author            string   `json:"author"`
	BindABAccount     bool     `json:"bind_ab_account"`
	UpdateExisting    bool     `json:"update_existing"`
	PullRequestNumber int      `json:"pull_request_number"`
}

type existingPullRequest struct {
	Number int    `json:"number"`
	URL    string `json:"html_url"`
	State  string `json:"state"`
	Merged bool   `json:"merged"`
	Head   struct {
		Ref  string `json:"ref"`
		Repo struct {
			Owner struct {
				Login string `json:"login"`
			} `json:"owner"`
			Name string `json:"name"`
		} `json:"repo"`
	} `json:"head"`
}

type submissionRequest struct {
	SchemaVersion     int     `json:"schema_version"`
	Mode              string  `json:"mode"`
	OriginalID        *string `json:"original_id"`
	BaseEntryDigest   *string `json:"base_entry_digest"`
	BaseCatalogCommit *string `json:"base_catalog_commit"`
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
	if cfg.UpdateExisting && cfg.PullRequestNumber > 0 {
		if _, err := c.writableSubmissionPR(ctx, token, cfg.PullRequestNumber); err != nil {
			return Result{}, err
		}
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
	resourceRepo, err := c.ensureRepo(ctx, token, repoName, "AstroBox resource of "+snap.Revision.Name)
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
	commit, err := c.uploadFiles(ctx, token, resourceRepo, "Publish "+snap.Revision.Name, files)
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
	line := []string{cfg.ItemID, snap.Revision.Name, restype, resourceRepo.Owner, resourceRepo.Name, shortCommit, iconPath, coverPath, strings.Join(cfg.Tags, ";"), strings.Join(uniqueVendors(snap.Artifacts), ";"), strings.Join(devices, ";"), cfg.PaidType}
	if err := validateCatalogRow(line); err != nil {
		return Result{}, err
	}
	mode := "create"
	var originalID, baseDigest *string
	for index := 1; index < len(rows); index++ {
		if len(rows[index]) == 0 || rows[index][0] != cfg.ItemID {
			continue
		}
		if len(rows[index]) < len(catalogHeader) || !strings.EqualFold(rows[index][3], resourceRepo.Owner) || !strings.EqualFold(rows[index][4], resourceRepo.Name) {
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
	// Both create and edit submissions must identify the upstream catalog
	// snapshot they were prepared against.  The digest and original ID remain
	// edit-only fields, but create requests still need base_catalog_commit so
	// ABCC can validate the submission against the same index_v2.csv revision.
	request := submissionRequest{
		SchemaVersion:     1,
		Mode:              mode,
		OriginalID:        originalID,
		BaseEntryDigest:   baseDigest,
		BaseCatalogCommit: stringPtr(catalogCommit),
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
	submissionPath, err := c.submissionPath(login, resourceRepo.Name)
	if err != nil {
		return Result{}, err
	}
	branch := "oronbox-resource-" + sanitize(cfg.ItemID) + "-" + strconv.FormatInt(time.Now().UTC().Unix(), 10)
	var existing *existingPullRequest
	if cfg.UpdateExisting && cfg.PullRequestNumber > 0 {
		current, err := c.writableSubmissionPR(ctx, token, cfg.PullRequestNumber)
		if err != nil {
			return Result{}, err
		}
		existing = &current
		fork = repo{Owner: current.Head.Repo.Owner.Login, Name: current.Head.Repo.Name, Branch: current.Head.Ref}
		branch = current.Head.Ref
	} else if err := c.createRef(ctx, token, fork.Owner, fork.Name, branch, forkBaseSHA); err != nil {
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
	body := buildPRBody(snap, cfg, restype, resourceRepo, shortCommit, iconPath, coverPath, previews, entries)
	if existing != nil {
		return Result{PullRequest: existing.URL, PullRequestNumber: existing.Number, Repository: "https://github.com/" + resourceRepo.Owner + "/" + resourceRepo.Name, SubmissionProtocol: "v2", SubmissionPath: submissionPath, CatalogRow: line, CatalogCommit: catalogCommit}, nil
	}
	pr, err := c.createPR(ctx, token, "[OBCC] "+operation+" "+snap.Revision.Name, fork.Owner+":"+branch, c.cfg.RepoBranch, body)
	if err != nil {
		return Result{}, fmt.Errorf("create AstroBox pull request: %w", err)
	}
	return Result{PullRequest: pr.URL, PullRequestNumber: pr.Number, Repository: "https://github.com/" + resourceRepo.Owner + "/" + resourceRepo.Name, SubmissionProtocol: "v2", SubmissionPath: submissionPath, CatalogRow: line, CatalogCommit: catalogCommit}, nil
}
