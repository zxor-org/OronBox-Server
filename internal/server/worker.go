package server

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/astrobox"
	"github.com/zxor-org/OronBox-Server/internal/bandbbs"
	"github.com/zxor-org/OronBox-Server/internal/syndication"
)

// runCoordinator drives the publication state machine: it dispatches activated
// downstream publications (米坛/AB) and polls AB's external review. It replaces
// the retired Gitea webhook path.
func (a *application) runCoordinator(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if a.db == nil {
			continue
		}
		for i := 0; i < 5; i++ {
			task, ok, err := a.claimPublication(ctx)
			if err != nil || !ok {
				break
			}
			if derr := a.dispatchPublication(ctx, task); derr != nil {
				a.failPublication(ctx, task, derr)
			}
		}
		_ = a.pollAstroBox(ctx)
	}
}

type publicationTask struct {
	ID           string
	SubmissionID string
	ResourceID   string
	Provider     string
	CategoryID   int
	Config       []byte
	Attempts     int
}

const maxPublicationAttempts = 5

func (a *application) claimPublication(ctx context.Context) (publicationTask, bool, error) {
	var t publicationTask
	err := a.db.Pool.QueryRow(ctx, `
WITH candidate AS (
  SELECT p.id FROM publications p JOIN resource_submissions s ON s.id=p.submission_id
  WHERE p.provider<>'oronbox' AND p.state='pending' AND s.status IN ('merged','syndicating')
    AND (p.next_attempt_at IS NULL OR p.next_attempt_at<=now())
  ORDER BY p.created_at LIMIT 1 FOR UPDATE OF p SKIP LOCKED
), claimed AS (
  UPDATE publications p SET state='dispatching',attempts=p.attempts+1,updated_at=now() FROM candidate c WHERE p.id=c.id
  RETURNING p.id,p.submission_id,p.provider,p.category_id,p.config,p.attempts
)
SELECT c.id::text,c.submission_id::text,s.resource_id,c.provider,c.category_id,c.config,c.attempts
FROM claimed c JOIN resource_submissions s ON s.id=c.submission_id`).
		Scan(&t.ID, &t.SubmissionID, &t.ResourceID, &t.Provider, &t.CategoryID, &t.Config, &t.Attempts)
	if err != nil {
		return t, false, nil
	}
	_, _ = a.db.Pool.Exec(ctx, `INSERT INTO publication_attempts(publication_id,attempt_number,phase,state_from,state_to) VALUES($1,$2,'execute','pending','dispatching')`, t.ID, t.Attempts)
	return t, true, nil
}

func (a *application) dispatchPublication(ctx context.Context, t publicationTask) error {
	switch t.Provider {
	case "bandbbs":
		return a.dispatchBandBBS(ctx, t)
	case "astrobox":
		return a.dispatchAstroBox(ctx, t)
	default:
		return fmt.Errorf("unknown publication provider %q", t.Provider)
	}
}

// ---------- 米坛 ----------

type bandbbsPlanConfig struct {
	CategoryID      int      `json:"category_id"`
	ForumID         int      `json:"forum_id"`
	DeviceCompatIDs []string `json:"device_compat_ids"`
}

func (a *application) dispatchBandBBS(ctx context.Context, t publicationTask) error {
	var cfg bandbbsPlanConfig
	_ = json.Unmarshal(t.Config, &cfg)
	manifest, changelog, err := a.loadSubmissionArtifact(ctx, t.ResourceID)
	if err != nil {
		return fmt.Errorf("load draft artifact: %w", err)
	}
	plan, err := a.bandbbsPlanFor(ctx, t.ResourceID, cfg.DeviceCompatIDs, manifest.Item.Restype)
	if err != nil {
		return err
	}
	token, creatorID, err := a.bandbbsPublishToken(ctx, t.SubmissionID)
	if err != nil {
		return err
	}
	_ = changelog

	previews, err := a.readPreviewImages(ctx, t.ResourceID, manifest.Item.Preview)
	if err != nil {
		return err
	}
	versionFile, versionName := a.downloadFor(ctx, t.ResourceID, manifest, cfg.DeviceCompatIDs)

	input := bandbbs.PublishInput{
		CategoryID:       cfg.CategoryID,
		CreatorID:        creatorID,
		Restype:          manifest.Item.Restype,
		ResourcePrefixID: plan.ResourcePrefix,
		ThreadPrefixID:   plan.ThreadPrefix,
		Title:            manifest.Item.Name,
		TagLine:          manifest.Item.Tagline,
		Description:      buildBandBBSDescription(manifest),
		Previews:         previews,
		Version:          manifestVersion(manifest),
		VersionFileName:  versionName,
		VersionData:      versionFile,
	}
	if p := a.purchaseFor(ctx, t.SubmissionID); p != nil {
		input.External = &bandbbs.ExternalPurchase{Link: p.Link, Price: p.Price, Currency: p.Currency}
	}

	client := bandbbs.New(a.cfg.BandBBS.APIURL)
	res, perr := client.Publish(ctx, token, input)
	if perr != nil {
		return perr
	}
	return a.completePublication(ctx, t, fmt.Sprintf("%d", res.ResourceID), bandbbsURL(res.ResourceID), map[string]any{
		"resource_id": res.ResourceID, "thread_id": res.ThreadID,
		"resource_version_id": res.VersionID, "file_ids": res.FileIDs, "update_id": res.UpdateID,
	})
}

type bandbbsPlan struct{ ResourcePrefix, ThreadPrefix int }

func (a *application) bandbbsPlanFor(ctx context.Context, resource string, compatIDs []string, restype string) (bandbbsPlan, error) {
	devices, err := a.fetchDevices(ctx)
	if err != nil {
		return bandbbsPlan{}, err
	}
	owner, catalog := splitGiteaRepo(a.cfg.Gitea.CatalogRepo)
	client := syndication.GiteaClient{BaseURL: a.cfg.Gitea.APIURL, Token: a.cfg.Gitea.ClientSecret}
	raw, err := client.ReadFile(ctx, owner, catalog, "publish.json", "main")
	if err != nil {
		return bandbbsPlan{}, fmt.Errorf("read publish.json: %w", err)
	}
	var pub syndication.PublishConfig
	if err := json.Unmarshal(raw, &pub); err != nil {
		return bandbbsPlan{}, err
	}
	byCompat := map[string]string{}
	for codename, spec := range devices {
		if spec.ID != "" {
			byCompat[spec.ID] = codename
		}
	}
	for _, compat := range compatIDs {
		if codename, ok := byCompat[compat]; ok {
			if cat, ok := pub.BandBBS[codename]; ok {
				return bandbbsPlan{ResourcePrefix: prefixFor(pub.ResourcePrefixes, restype), ThreadPrefix: prefixFor(cat.ThreadPrefixes, restype)}, nil
			}
		}
	}
	return bandbbsPlan{}, fmt.Errorf("no publish.json mapping for resource %s", resource)
}

func prefixFor(prefixes map[string]int, restype string) int {
	key := "watchface"
	if restype == "quick_app" {
		key = "app"
	}
	return prefixes[key]
}

func (a *application) readPreviewImages(ctx context.Context, resource string, paths []string) ([]bandbbs.PreviewImage, error) {
	out := make([]bandbbs.PreviewImage, 0, len(paths))
	for _, p := range paths {
		data, err := a.readResourceFile(ctx, resource, p)
		if err != nil {
			continue
		}
		out = append(out, bandbbs.PreviewImage{Name: pathBase(p), Data: data})
	}
	return out, nil
}

func (a *application) downloadFor(ctx context.Context, resource string, manifest syndication.Manifest, compatIDs []string) ([]byte, string) {
	for _, compat := range compatIDs {
		entry, ok := manifest.Downloads[compat]
		if !ok {
			continue
		}
		data, err := a.readResourceFile(ctx, resource, entry.FileName)
		if err != nil {
			continue
		}
		return data, pathBase(entry.FileName)
	}
	return nil, ""
}

func buildBandBBSDescription(m syndication.Manifest) string {
	var b strings.Builder
	if len(m.Item.Author) > 0 {
		credits := make([]string, 0, len(m.Item.Author))
		for _, a := range m.Item.Author {
			person := a.Name
			if a.UserID != 0 {
				person = fmt.Sprintf("[URL='https://www.bandbbs.cn/members/%d/']%s[/URL]", a.UserID, a.Name)
			} else if strings.TrimSpace(a.Link) != "" {
				person = fmt.Sprintf("[URL='%s']%s[/URL]", a.Link, a.Name)
			}
			if strings.TrimSpace(a.Role) != "" {
				person += " (" + a.Role + ")"
			}
			credits = append(credits, person)
		}
		b.WriteString("[B]制作团队：[/B]")
		b.WriteString(strings.Join(credits, "、"))
		b.WriteString("\n\n")
	}
	b.WriteString(m.Item.Description)
	if len(m.Links) > 0 {
		b.WriteString("\n\n相关链接：\n")
		for _, l := range m.Links {
			if strings.TrimSpace(l.URL) != "" {
				fmt.Fprintf(&b, "- %s：%s\n", strings.TrimSpace(l.Title), strings.TrimSpace(l.URL))
			}
		}
	}
	return b.String()
}

func (a *application) purchaseFor(ctx context.Context, submissionID string) *bandbbs.ExternalPurchase {
	var raw []byte
	if err := a.db.Pool.QueryRow(ctx, `SELECT config FROM resource_submissions WHERE id=$1`, submissionID).Scan(&raw); err != nil {
		return nil
	}
	var cfg syndication.SubmissionConfig
	if json.Unmarshal(raw, &cfg) != nil || cfg.BandBBS.Purchase == nil {
		return nil
	}
	p := cfg.BandBBS.Purchase
	if strings.TrimSpace(p.Link) == "" || p.Price <= 0 {
		return nil
	}
	return &bandbbs.ExternalPurchase{Link: p.Link, Price: p.Price, Currency: p.Currency}
}

func bandbbsURL(resourceID int) string {
	return fmt.Sprintf("https://www.bandbbs.cn/resources/%d/", resourceID)
}

// ---------- AstroBox ----------

func (a *application) dispatchAstroBox(ctx context.Context, t publicationTask) error {
	manifest, _, err := a.loadSubmissionArtifact(ctx, t.ResourceID)
	if err != nil {
		return err
	}
	token, login, err := a.githubToken(ctx, t.SubmissionID)
	if err != nil {
		return err
	}
	files, err := a.buildABFiles(ctx, t.ResourceID, manifest)
	if err != nil {
		return err
	}
	client := astrobox.Client{
		GitHub:      syndication.GitHubClient{},
		RepoOwner:   a.cfg.AstroBox.RepoOwner,
		RepoName:    a.cfg.AstroBox.RepoName,
		RepoBranch:  a.cfg.AstroBox.RepoBranch,
		CatalogPath: a.cfg.AstroBox.CatalogPath,
	}
	repo, err := client.EnsureResourceRepo(ctx, token, abRepoName(t.ResourceID), manifest.Item.Name, files)
	if err != nil {
		return err
	}
	paidType := ""
	if p := a.purchaseFor(ctx, t.SubmissionID); p != nil {
		paidType = "force_paid"
	}
	vendors := ""
	if devices, derr := a.fetchDevices(ctx); derr == nil {
		vendors = syndication.DeviceVendors(manifest.Downloads, devices)
	}
	entry := astrobox.CatalogEntry{
		ID: t.ResourceID, Name: manifest.Item.Name, Restype: manifest.Item.Restype,
		RepoOwner: repo.Owner, RepoName: repo.Name, RepoCommitHash: shortSHA(repo.Commit),
		Icon: manifest.Item.Icon, Cover: manifest.Item.Cover,
		Tags: strings.Join(a.tagsFor(ctx, t.SubmissionID), ";"), DeviceVendors: vendors,
		Devices:  strings.Join(astrobox.SortedDeviceKeys(manifest.Downloads), ";"),
		PaidType: paidType,
	}
	upstreamCSV, upstreamCommit := client.UpstreamCatalog(ctx, token)
	req := astrobox.DeriveSubmission(entry, upstreamCSV, upstreamCommit)
	prNumber, prURL, _, err := client.Submit(ctx, token, login, repo, entry, req)
	if err != nil {
		return err
	}
	return a.finishExternalReview(ctx, t, fmt.Sprintf("%d", prNumber), prURL)
}

func (a *application) buildABFiles(ctx context.Context, resource string, manifest syndication.Manifest) (map[string][]byte, error) {
	files := map[string][]byte{}
	changelog, err := a.readChangelog(ctx, resource)
	if err != nil {
		changelog = syndication.Changelog{}
	}
	raw, err := astrobox.BuildManifestV2(manifest, changelog, "")
	if err != nil {
		return nil, err
	}
	files["manifest_v2.json"] = raw
	paths := []string{manifest.Item.Icon, manifest.Item.Cover}
	paths = append(paths, manifest.Item.Preview...)
	for _, e := range manifest.Downloads {
		paths = append(paths, e.FileName)
	}
	for _, p := range paths {
		if strings.TrimSpace(p) == "" {
			continue
		}
		data, err := a.readResourceFile(ctx, resource, p)
		if err != nil {
			continue
		}
		files[p] = data
	}
	return files, nil
}

func (a *application) pollAstroBox(ctx context.Context) error {
	var id, externalID, url string
	err := a.db.Pool.QueryRow(ctx, `
WITH candidate AS (
  SELECT p.id FROM publications p WHERE p.provider='astrobox' AND p.state='waiting_external_review'
    AND (p.next_attempt_at IS NULL OR p.next_attempt_at<=now())
  ORDER BY p.next_attempt_at NULLS FIRST LIMIT 1 FOR UPDATE OF p SKIP LOCKED
)
SELECT p.id::text,COALESCE(p.external_id,''),COALESCE(p.external_url,'') FROM publications p JOIN candidate c ON c.id=p.id`).Scan(&id, &externalID, &url)
	if err != nil {
		return nil
	}
	number, _ := strconv.Atoi(strings.TrimSpace(externalID))
	if number == 0 {
		return a.deferPoll(ctx, id, fmt.Errorf("publication has no PR number"))
	}
	var subID string
	if err := a.db.Pool.QueryRow(ctx, `SELECT submission_id::text FROM publications WHERE id=$1`, id).Scan(&subID); err != nil {
		return nil
	}
	token, _, err := a.githubToken(ctx, subID)
	if err != nil {
		return a.deferPoll(ctx, id, err)
	}
	client := astrobox.Client{
		RepoOwner: a.cfg.AstroBox.RepoOwner, RepoName: a.cfg.AstroBox.RepoName,
		RepoBranch: a.cfg.AstroBox.RepoBranch, CatalogPath: a.cfg.AstroBox.CatalogPath,
	}
	merged, stateName, htmlURL, err := client.PRStatus(ctx, token, number)
	if err != nil {
		return a.deferPoll(ctx, id, err)
	}
	commentBodies, _ := client.ReviewComments(ctx, token, number)
	reviewState, _ := astrobox.DeriveReviewStatus(commentBodies)
	switch {
	case merged:
		_, _ = a.db.Pool.Exec(ctx, `UPDATE publications SET state='published',external_url=$2,error_message='',updated_at=now() WHERE id=$1`, id, htmlURL)
		return a.rollupSubmissionForPublication(ctx, id)
	case stateName == "closed":
		_, _ = a.db.Pool.Exec(ctx, `UPDATE publications SET state='failed',error_message='外部审核不通过',external_url=$2,updated_at=now() WHERE id=$1`, id, htmlURL)
		return a.rollupSubmissionForPublication(ctx, id)
	default:
		nextState := "waiting_external_review"
		if reviewState == "changes_requested" {
			nextState = "external_changes_requested"
		} else if reviewState == "fixed_waiting" {
			nextState = "external_fixed_waiting"
		}
		_, _ = a.db.Pool.Exec(ctx, `UPDATE publications SET state=$2,external_url=$3,next_attempt_at=now()+interval '60 seconds',updated_at=now() WHERE id=$1`, id, nextState, htmlURL)
		return nil
	}
}

func (a *application) deferPoll(ctx context.Context, id string, err error) error {
	_, _ = a.db.Pool.Exec(ctx, `UPDATE publications SET error_message=$2,next_attempt_at=now()+interval '60 seconds',updated_at=now() WHERE id=$1`, id, err.Error())
	return nil
}

// ---------- shared helpers ----------

func (a *application) completePublication(ctx context.Context, t publicationTask, externalID, externalURL string, detail map[string]any) error {
	raw, _ := json.Marshal(detail)
	_, err := a.db.Pool.Exec(ctx, `UPDATE publications SET state='published',external_id=$2,external_url=$3,error_message='',status_detail=$4,updated_at=now() WHERE id=$1`, t.ID, externalID, externalURL, raw)
	if err != nil {
		return err
	}
	_, _ = a.db.Pool.Exec(ctx, `INSERT INTO publication_attempts(publication_id,attempt_number,phase,state_from,state_to,detail) VALUES($1,$2,'execute','dispatching','published',$3)`, t.ID, t.Attempts, raw)
	if t.Provider == "bandbbs" {
		_, _ = a.db.Pool.Exec(ctx, `INSERT INTO external_bindings(resource_id,provider,category_id,external_id,external_url,meta) VALUES($1,'bandbbs',$2,$3,$4,$5) ON CONFLICT(resource_id,provider,category_id) DO UPDATE SET external_id=EXCLUDED.external_id,external_url=EXCLUDED.external_url,meta=EXCLUDED.meta,updated_at=now()`, t.ResourceID, t.CategoryID, externalID, externalURL, raw)
	}
	return a.rollupSubmission(t.SubmissionID, ctx)
}

func (a *application) finishExternalReview(ctx context.Context, t publicationTask, externalID, externalURL string) error {
	_, err := a.db.Pool.Exec(ctx, `UPDATE publications SET state='waiting_external_review',external_id=$2,external_url=$3,error_message='',next_attempt_at=now()+interval '60 seconds',updated_at=now() WHERE id=$1`, t.ID, externalID, externalURL)
	if err != nil {
		return err
	}
	if _, err := a.db.Pool.Exec(ctx, `INSERT INTO external_bindings(resource_id,provider,category_id,external_id,external_url) VALUES($1,'astrobox',0,$2,$3) ON CONFLICT(resource_id,provider,category_id) DO UPDATE SET external_id=EXCLUDED.external_id,external_url=EXCLUDED.external_url,updated_at=now()`, t.ResourceID, externalID, externalURL); err != nil {
		return err
	}
	return a.rollupSubmission(t.SubmissionID, ctx)
}

func (a *application) failPublication(ctx context.Context, t publicationTask, cause error) {
	state := "pending"
	var next any
	if t.Attempts >= maxPublicationAttempts {
		state = "failed"
	} else {
		next = time.Now().Add(syndication.RetryDelay(t.Attempts))
	}
	_, _ = a.db.Pool.Exec(ctx, `UPDATE publications SET state=$2,error_message=$3,next_attempt_at=$4,updated_at=now() WHERE id=$1`, t.ID, state, cause.Error(), next)
	_, _ = a.db.Pool.Exec(ctx, `INSERT INTO publication_attempts(publication_id,attempt_number,phase,state_from,state_to,error_message) VALUES($1,$2,'execute','dispatching',$3,$4)`, t.ID, t.Attempts, state, cause.Error())
	_ = a.rollupSubmission(t.SubmissionID, ctx)
}

func (a *application) rollupSubmissionForPublication(ctx context.Context, publicationID string) error {
	var submissionID string
	if err := a.db.Pool.QueryRow(ctx, `SELECT submission_id::text FROM publications WHERE id=$1`, publicationID).Scan(&submissionID); err != nil {
		return nil
	}
	return a.rollupSubmission(submissionID, ctx)
}

func (a *application) rollupSubmission(submissionID string, ctx context.Context) error {
	rows, err := a.db.Pool.Query(ctx, `SELECT state FROM publications WHERE submission_id=$1 AND provider<>'oronbox'`, submissionID)
	if err != nil {
		return err
	}
	states := []string{}
	for rows.Next() {
		var s string
		if rows.Scan(&s) == nil {
			states = append(states, s)
		}
	}
	rows.Close()
	if len(states) == 0 {
		return nil
	}
	status := syndication.Rollup(states)
	_, _ = a.db.Pool.Exec(ctx, `UPDATE resource_submissions SET status=$2,updated_at=now() WHERE id=$1 AND status NOT IN ('rejected','changes_requested','fixed_waiting','waiting_review')`, submissionID, status)
	return nil
}

func (a *application) loadSubmissionArtifact(ctx context.Context, resource string) (syndication.Manifest, syndication.Changelog, error) {
	manifest, err := a.loadDraftManifest(ctx, resource)
	if err != nil {
		return syndication.Manifest{}, syndication.Changelog{}, err
	}
	changelog, _ := a.readChangelog(ctx, resource)
	return manifest, changelog, nil
}

func (a *application) readChangelog(ctx context.Context, resource string) (syndication.Changelog, error) {
	var cl syndication.Changelog
	raw, err := a.readResourceFile(ctx, resource, "changelog.json")
	if err != nil {
		return cl, err
	}
	if err := json.Unmarshal(raw, &cl); err != nil {
		return cl, err
	}
	return cl, nil
}

func (a *application) readResourceFile(ctx context.Context, resource, path string) ([]byte, error) {
	if a.cfg.Gitea.APIURL == "" || a.cfg.Gitea.ClientSecret == "" {
		return nil, fmt.Errorf("gitea is not configured")
	}
	owner, _ := splitGiteaRepo(a.cfg.Gitea.CatalogRepo)
	client := syndication.GiteaClient{BaseURL: a.cfg.Gitea.APIURL, Token: a.cfg.Gitea.ClientSecret}
	return client.ReadFile(ctx, owner, "oronbox-resource-"+resource, path, "main")
}

func (a *application) bandbbsPublishToken(ctx context.Context, submissionID string) (string, string, error) {
	var creatorID string
	if err := a.db.Pool.QueryRow(ctx, `SELECT creator_id::text FROM resource_submissions WHERE id=$1`, submissionID).Scan(&creatorID); err != nil {
		return "", "", err
	}
	var cipher []byte
	var subject string
	if err := a.db.Pool.QueryRow(ctx, `SELECT access_token_cipher,subject FROM oauth_grants WHERE user_id=$1 AND provider='bandbbs_publish'`, creatorID).Scan(&cipher, &subject); err != nil {
		return "", "", fmt.Errorf("BandBBS publishing permission is not authorized")
	}
	if a.cipher == nil {
		return "", "", fmt.Errorf("token decryption is unavailable")
	}
	token, err := a.cipher.DecryptString(string(cipher))
	if err != nil {
		return "", "", err
	}
	return token, subject, nil
}

func (a *application) githubToken(ctx context.Context, submissionID string) (string, string, error) {
	var creatorID string
	if err := a.db.Pool.QueryRow(ctx, `SELECT creator_id::text FROM resource_submissions WHERE id=$1`, submissionID).Scan(&creatorID); err != nil {
		return "", "", err
	}
	var cipher []byte
	var login string
	if err := a.db.Pool.QueryRow(ctx, `SELECT access_token_cipher,login FROM github_grants WHERE user_id=$1`, creatorID).Scan(&cipher, &login); err != nil {
		return "", "", fmt.Errorf("GitHub publishing permission is not authorized")
	}
	if a.cipher == nil {
		return "", "", fmt.Errorf("token decryption is unavailable")
	}
	token, err := a.cipher.DecryptString(string(cipher))
	if err != nil {
		return "", "", err
	}
	return token, login, nil
}

var abSlugRe = regexp.MustCompile(`[^a-z0-9_-]+`)

func abRepoName(resource string) string {
	slug := abSlugRe.ReplaceAllString(strings.ToLower(resource), "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		slug = "submission"
	}
	return "astrobox-resource-" + slug
}

func pathBase(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func (a *application) tagsFor(ctx context.Context, submissionID string) []string {
	var raw []byte
	if err := a.db.Pool.QueryRow(ctx, `SELECT config FROM resource_submissions WHERE id=$1`, submissionID).Scan(&raw); err != nil {
		return nil
	}
	var cfg syndication.SubmissionConfig
	if json.Unmarshal(raw, &cfg) != nil {
		return nil
	}
	return cfg.Catalog.Tags
}
