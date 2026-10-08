package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/zxor-org/OronBox-Server/internal/syndication"
)

var packageIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{2,127}$`)

func (a *application) creatorContract(w http.ResponseWriter, r *http.Request) {
	u, ok := userOf(r)
	if !ok || !creatorRole(u.Role) {
		jsonError(w, http.StatusForbidden, "creator_required", "creator permission required", nil)
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/creator/"), "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		jsonError(w, http.StatusNotFound, "not_found", "creator endpoint not found", nil)
		return
	}
	switch parts[0] {
	case "resources":
		a.creatorResources(w, r, u, parts[1:])
	case "reviews":
		a.creatorReview(w, r, u, parts[1:])
	case "collaborations":
		a.creatorCollaboration(w, r, u, parts[1:])
	case "collections":
		jsonResponse(w, 200, map[string]any{"collections": []any{}, "items": []any{}})
	default:
		jsonError(w, http.StatusNotFound, "not_found", "creator endpoint not found", nil)
	}
}

func creatorRole(role string) bool {
	return role == "creator" || role == "moderator" || role == "admin" || role == "owner" || role == "user" || role == ""
}

func (a *application) creatorResources(w http.ResponseWriter, r *http.Request, u userContext, parts []string) {
	if len(parts) == 0 {
		if r.Method == http.MethodPost {
			a.createCreatorResource(w, r, u)
			return
		}
		if r.Method != http.MethodGet {
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
			return
		}
		a.listCreatorResources(w, r, u)
		return
	}
	resource := parts[0]
	if resource == "" {
		jsonError(w, 400, "invalid_resource_id", "resource_id is required", nil)
		return
	}
	if len(parts) == 1 && r.Method == http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use /creator/resources without a resource id to create a draft", nil)
		return
	}
	if len(parts) == 1 && r.Method == http.MethodGet {
		a.getCreatorResource(w, r, resource, u)
		return
	}
	if len(parts) == 1 {
		jsonError(w, http.StatusNotFound, "not_found", "creator resource endpoint not found", nil)
		return
	}
	if !a.resourceAccess(r, resource, u.ID, false) {
		jsonError(w, http.StatusForbidden, "creator_access_denied", "you do not have access to this resource", nil)
		return
	}
	switch parts[1] {
	case "draft":
		switch r.Method {
		case http.MethodGet:
			a.getDraft(w, r, resource)
		case http.MethodPut:
			a.saveDraft(w, r, resource)
		default:
			jsonError(w, 405, "method_not_allowed", "method not allowed", nil)
		}
	case "publish":
		if r.Method != http.MethodPost {
			jsonError(w, 405, "method_not_allowed", "method not allowed", nil)
			return
		}
		a.publishResource(w, r, resource, u)
	case "syndications":
		if r.Method != http.MethodGet {
			jsonError(w, 405, "method_not_allowed", "method not allowed", nil)
			return
		}
		a.listSyndications(w, r, resource)
	case "publications":
		if len(parts) != 4 || parts[2] == "" || parts[3] != "retry" || r.Method != http.MethodPost {
			jsonError(w, 404, "not_found", "publication endpoint not found", nil)
			return
		}
		a.retryPublication(w, r, resource, parts[2])
	case "fixed":
		if r.Method != http.MethodPost {
			jsonError(w, 405, "method_not_allowed", "method not allowed", nil)
			return
		}
		a.submitFixed(w, r, resource)
	case "takedown":
		if r.Method != http.MethodPost {
			jsonError(w, 405, "method_not_allowed", "method not allowed", nil)
			return
		}
		a.takedown(w, r, resource, u)
	case "collaborators":
		a.collaborators(w, r, resource, u, parts[2:])
	default:
		jsonError(w, 404, "not_found", "creator resource endpoint not found", nil)
	}
}

func (a *application) listCreatorResources(w http.ResponseWriter, r *http.Request, u userContext) {
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	query := `SELECT ri.resource_id,ri.title,ri.latest_version,ri.restype,ri.download_count,ri.coin_count,ri.rating,ri.updated_at,COALESCE(s.status,'published')
		FROM resource_interactions ri 
		LEFT JOIN LATERAL (SELECT status FROM resource_submissions WHERE resource_id=ri.resource_id ORDER BY updated_at DESC LIMIT 1) s ON true 
		WHERE ri.owner_id=$1 OR ri.resource_id IN (SELECT resource_id FROM resource_submissions WHERE creator_id=$1) OR ri.resource_id IN (SELECT resource_id FROM resource_collaborators WHERE user_id=$1)`
	args := []any{u.ID}
	if status != "" {
		query += ` AND COALESCE(s.status,'published')=$2`
		args = append(args, status)
	}
	query += ` ORDER BY ri.updated_at DESC`
	rows, err := a.db.Pool.Query(r.Context(), query, args...)
	if err != nil {
		jsonError(w, 500, "creator_resources_failed", err.Error(), nil)
		return
	}
	defer rows.Close()
	items := []any{}
	for rows.Next() {
		var id, title, version, restype, state string
		var downloads, coins int64
		var rating float64
		var updated time.Time
		if err := rows.Scan(&id, &title, &version, &restype, &downloads, &coins, &rating, &updated, &state); err != nil {
			jsonError(w, 500, "creator_resources_failed", err.Error(), nil)
			return
		}
		if restype == "" {
			restype = "quick_app"
		}
		revisions := []map[string]any{}
		subRows, err := a.db.Pool.Query(r.Context(), `SELECT id,title,version,status,created_at,updated_at,config FROM resource_submissions WHERE resource_id=$1 ORDER BY created_at DESC`, id)
		if err == nil {
			for subRows.Next() {
				var subID, subTitle, subVer, subStat string
				var subCreatedAt, subUpdatedAt time.Time
				var subCfg []byte
				if err := subRows.Scan(&subID, &subTitle, &subVer, &subStat, &subCreatedAt, &subUpdatedAt, &subCfg); err == nil {
					var cfg syndication.SubmissionConfig
					_ = json.Unmarshal(subCfg, &cfg)
					var rawCfg map[string]any
					_ = json.Unmarshal(subCfg, &rawCfg)
					summary := ""
					if oron, ok := rawCfg["oronbox"].(map[string]any); ok {
						if s, ok := oron["summary"].(string); ok {
							summary = s
						}
					}
					var pPrice *float64
					pLink := ""
					pType := "free"
					if cfg.BandBBS.Purchase != nil {
						pLink = cfg.BandBBS.Purchase.Link
						pPrice = &cfg.BandBBS.Purchase.Price
						pType = "paid"
					}
					revState := subStat
					if revState == "waiting_review" || revState == "fixed_waiting" {
						revState = "pending"
					} else if revState == "merged" || revState == "published" {
						revState = "approved"
					}
					revisions = append(revisions, map[string]any{
						"id":             subID,
						"number":         len(revisions) + 1,
						"name":           subTitle,
						"summary":        summary,
						"state":          revState,
						"paid_type":      pType,
						"purchase_link":  pLink,
						"purchase_price": pPrice,
					})
				}
			}
			subRows.Close()
		}
		if len(revisions) == 0 && version != "" {
			revisions = append(revisions, map[string]any{
				"id":        id + "-" + version,
				"number":    1,
				"name":      title,
				"summary":   "",
				"state":     state,
				"paid_type": "free",
			})
		}
		bindings := []map[string]any{}
		bRows, err := a.db.Pool.Query(r.Context(), `SELECT provider,category_id,external_id,external_url,meta FROM external_bindings WHERE resource_id=$1`, id)
		if err == nil {
			for bRows.Next() {
				var prov, extID, extURL string
				var catID int
				var metaRaw []byte
				if bRows.Scan(&prov, &catID, &extID, &extURL, &metaRaw) == nil {
					var metaObj map[string]any
					if len(metaRaw) > 0 {
						_ = json.Unmarshal(metaRaw, &metaObj)
					}
					bindings = append(bindings, map[string]any{
						"provider":    prov,
						"category_id": catID,
						"external_id": extID,
						"external_url": extURL,
						"meta":        metaObj,
					})
				}
			}
			bRows.Close()
		}
		publications := []map[string]any{}
		pRows, err := a.db.Pool.Query(r.Context(), `SELECT p.id,p.provider,p.category_id,p.state,COALESCE(p.external_id,''),COALESCE(p.external_url,''),COALESCE(p.error_message,'') FROM publications p JOIN resource_submissions s ON s.id=p.submission_id WHERE s.resource_id=$1 ORDER BY p.provider,p.category_id`, id)
		if err == nil {
			for pRows.Next() {
				var pubID, pubProv, pubState, pubExtID, pubExtURL, pubMsg string
				var pubCat int
				if pRows.Scan(&pubID, &pubProv, &pubCat, &pubState, &pubExtID, &pubExtURL, &pubMsg) == nil {
					publications = append(publications, map[string]any{
						"id":           pubID,
						"provider":     pubProv,
						"target":       pubProv,
						"category_id":  pubCat,
						"state":        pubState,
						"external_id":  pubExtID,
						"external_url": pubExtURL,
						"error":        pubMsg,
					})
				}
			}
			pRows.Close()
		}
		var latestRev map[string]any
		if len(revisions) > 0 {
			latestRev = revisions[0]
		}
		item := map[string]any{
			"resource_id":      id,
			"title":            title,
			"latest_version":   version,
			"restype":          restype,
			"status":           state,
			"download_count":   downloads,
			"coin_count":       coins,
			"rating":           rating,
			"updated_at":       updated,
			"revisions":        revisions,
			"current_revision": latestRev,
			"bindings":         bindings,
			"publications":     publications,
			"resource": map[string]any{
				"id":               id,
				"slug":             id,
				"draft_name":       title,
				"kind":             restype,
				"moderation_state": "visible",
				"download_count":   downloads,
				"updated_at":       updated.Format(time.RFC3339),
			},
			"review": map[string]any{
				"state": state,
			},
		}
		items = append(items, item)
	}
	jsonResponse(w, 200, map[string]any{"items": items, "resources": items, "total": len(items)})
}

func (a *application) getCreatorResource(w http.ResponseWriter, r *http.Request, resourceID string, u userContext) {
	var title, version, restype, state string
	var downloads, coins int64
	var rating float64
	var updated time.Time
	err := a.db.Pool.QueryRow(r.Context(), `SELECT ri.title,ri.latest_version,ri.restype,ri.download_count,ri.coin_count,ri.rating,ri.updated_at,COALESCE(s.status,'published')
		FROM resource_interactions ri LEFT JOIN LATERAL (SELECT status FROM resource_submissions WHERE resource_id=ri.resource_id ORDER BY updated_at DESC LIMIT 1) s ON true WHERE ri.resource_id=$1`, resourceID).Scan(&title, &version, &restype, &downloads, &coins, &rating, &updated, &state)
	if err != nil {
		jsonError(w, 404, "resource_not_found", "resource not found", nil)
		return
	}
	if restype == "" {
		restype = "quick_app"
	}
	revisions := []map[string]any{}
	subRows, err := a.db.Pool.Query(r.Context(), `SELECT id,title,version,status,created_at,updated_at,config FROM resource_submissions WHERE resource_id=$1 ORDER BY created_at DESC`, resourceID)
	if err == nil {
		for subRows.Next() {
			var subID, subTitle, subVer, subStat string
			var subCreatedAt, subUpdatedAt time.Time
			var subCfg []byte
			if err := subRows.Scan(&subID, &subTitle, &subVer, &subStat, &subCreatedAt, &subUpdatedAt, &subCfg); err == nil {
				var cfg syndication.SubmissionConfig
				_ = json.Unmarshal(subCfg, &cfg)
				var rawCfg map[string]any
				_ = json.Unmarshal(subCfg, &rawCfg)
				summary := ""
				if oron, ok := rawCfg["oronbox"].(map[string]any); ok {
					if s, ok := oron["summary"].(string); ok {
						summary = s
					}
				}
				var pPrice *float64
				pLink := ""
				pType := "free"
				if cfg.BandBBS.Purchase != nil {
					pLink = cfg.BandBBS.Purchase.Link
					pPrice = &cfg.BandBBS.Purchase.Price
					pType = "paid"
				}
				revState := subStat
				if revState == "waiting_review" || revState == "fixed_waiting" {
					revState = "pending"
				} else if revState == "merged" || revState == "published" {
					revState = "approved"
				}
				revisions = append(revisions, map[string]any{
					"id":             subID,
					"number":         len(revisions) + 1,
					"name":           subTitle,
					"summary":        summary,
					"state":          revState,
					"paid_type":      pType,
					"purchase_link":  pLink,
					"purchase_price": pPrice,
				})
			}
		}
		subRows.Close()
	}
	if len(revisions) == 0 && version != "" {
		revisions = append(revisions, map[string]any{
			"id":        resourceID + "-" + version,
			"number":    1,
			"name":      title,
			"summary":   "",
			"state":     state,
			"paid_type": "free",
		})
	}
	bindings := []map[string]any{}
	bRows, err := a.db.Pool.Query(r.Context(), `SELECT provider,category_id,external_id,external_url,meta FROM external_bindings WHERE resource_id=$1`, resourceID)
	if err == nil {
		for bRows.Next() {
			var prov, extID, extURL string
			var catID int
			var metaRaw []byte
			if bRows.Scan(&prov, &catID, &extID, &extURL, &metaRaw) == nil {
				var metaObj map[string]any
				if len(metaRaw) > 0 {
					_ = json.Unmarshal(metaRaw, &metaObj)
				}
				bindings = append(bindings, map[string]any{
					"provider":    prov,
					"category_id": catID,
					"external_id": extID,
					"external_url": extURL,
					"meta":        metaObj,
				})
			}
		}
		bRows.Close()
	}
	publications := []map[string]any{}
	pRows, err := a.db.Pool.Query(r.Context(), `SELECT p.id,p.provider,p.category_id,p.state,COALESCE(p.external_id,''),COALESCE(p.external_url,''),COALESCE(p.error_message,'') FROM publications p JOIN resource_submissions s ON s.id=p.submission_id WHERE s.resource_id=$1 ORDER BY p.provider,p.category_id`, resourceID)
	if err == nil {
		for pRows.Next() {
			var pubID, pubProv, pubState, pubExtID, pubExtURL, pubMsg string
			var pubCat int
			if pRows.Scan(&pubID, &pubProv, &pubCat, &pubState, &pubExtID, &pubExtURL, &pubMsg) == nil {
				publications = append(publications, map[string]any{
					"id":           pubID,
					"provider":     pubProv,
					"target":       pubProv,
					"category_id":  pubCat,
					"state":        pubState,
					"external_id":  pubExtID,
					"external_url": pubExtURL,
					"error":        pubMsg,
				})
			}
		}
		pRows.Close()
	}
	var latestRev map[string]any
	if len(revisions) > 0 {
		latestRev = revisions[0]
	}
	jsonResponse(w, 200, map[string]any{
		"resource_id":      resourceID,
		"title":            title,
		"latest_version":   version,
		"restype":          restype,
		"status":           state,
		"download_count":   downloads,
		"coin_count":       coins,
		"rating":           rating,
		"updated_at":       updated,
		"revisions":        revisions,
		"current_revision": latestRev,
		"bindings":         bindings,
		"publications":     publications,
		"resource": map[string]any{
			"id":               resourceID,
			"slug":             resourceID,
			"draft_name":       title,
			"kind":             restype,
			"moderation_state": "visible",
			"download_count":   downloads,
			"updated_at":       updated.Format(time.RFC3339),
		},
		"review": map[string]any{
			"state": state,
		},
	})
}

func (a *application) createCreatorResource(w http.ResponseWriter, r *http.Request, u userContext) {
	var in struct {
		PackageID string `json:"resource_id"`
		Title     string `json:"title"`
		Restype   string `json:"restype"`
	}
	if !readJSON(w, r, &in) || !packageIDPattern.MatchString(in.PackageID) {
		if in.PackageID == "" || !packageIDPattern.MatchString(in.PackageID) {
			jsonError(w, 400, "invalid_resource_id", "resource_id has an invalid format", nil)
		}
		return
	}
	if in.Title == "" {
		in.Title = in.PackageID
	}
	if in.Restype == "" {
		in.Restype = "quick_app"
	}
	err := a.db.WithTx(r.Context(), func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(), `INSERT INTO resource_interactions(resource_id,title,restype,owner_id) VALUES($1,$2,$3,$4)`, in.PackageID, in.Title, in.Restype, u.ID); err != nil {
			return err
		}
		_, err := tx.Exec(r.Context(), `INSERT INTO resource_submissions(resource_id,title,version,creator_id,status) VALUES($1,$2,'0.0.1',$3,'draft')`, in.PackageID, in.Title, u.ID)
		return err
	})
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "duplicate") || strings.Contains(strings.ToLower(err.Error()), "unique") {
			jsonError(w, 409, "resource_exists", "resource already exists", nil)
			return
		}
		jsonError(w, 500, "resource_create_failed", err.Error(), nil)
		return
	}
	jsonResponse(w, 201, map[string]any{"resource_id": in.PackageID, "title": in.Title, "restype": in.Restype, "status": "draft", "version": "0.0.1"})
}

func (a *application) resourceAccess(r *http.Request, resource, userID string, ownerOnly bool) bool {
	var owner string
	if err := a.db.Pool.QueryRow(r.Context(), `SELECT owner_id FROM resource_interactions WHERE resource_id=$1`, resource).Scan(&owner); err != nil {
		return false
	}
	if owner == userID {
		return true
	}
	if ownerOnly {
		return false
	}
	var ok bool
	_ = a.db.Pool.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM resource_collaborators WHERE resource_id=$1 AND user_id=$2 AND status='accepted')`, resource, userID).Scan(&ok)
	return ok
}

func (a *application) getDraft(w http.ResponseWriter, r *http.Request, resource string) {
	var title, version, status string
	var updated time.Time
	err := a.db.Pool.QueryRow(r.Context(), `SELECT title,version,status,updated_at FROM resource_submissions WHERE resource_id=$1 ORDER BY updated_at DESC LIMIT 1`, resource).Scan(&title, &version, &status, &updated)
	if err != nil {
		jsonError(w, 404, "draft_not_found", "draft not found", nil)
		return
	}
	jsonResponse(w, 200, map[string]any{"resource_id": resource, "title": title, "version": version, "status": status, "manifest": map[string]any{}, "changelog": map[string]any{}, "files": map[string]any{}, "updated_at": updated})
}

func (a *application) saveDraft(w http.ResponseWriter, r *http.Request, resource string) {
	data, cfgRaw, err := readBundleRequest(w, r)
	if err != nil {
		jsonError(w, 400, "invalid_bundle", err.Error(), nil)
		return
	}
	bundle, err := syndication.ValidateBundle(data, resource)
	if err != nil {
		jsonError(w, 400, "invalid_bundle", err.Error(), nil)
		return
	}
	cfg := parseSubmissionConfig(cfgRaw)
	if err := a.pushBundleToGitea(r.Context(), resource, "main", bundle, true); err != nil {
		jsonError(w, 502, "gitea_draft_failed", err.Error(), nil)
		return
	}
	version := manifestVersion(bundle.Manifest)
	title := bundle.Manifest.Item.Name
	if strings.TrimSpace(title) == "" {
		title = bundle.Manifest.Item.Tagline
	}
	cfgJSON, _ := json.Marshal(cfg)
	_, err = a.db.Pool.Exec(r.Context(), `INSERT INTO resource_submissions(resource_id,title,version,creator_id,status,config) SELECT $1,$2,$3,owner_id,'draft',$4 FROM resource_interactions WHERE resource_id=$1 ON CONFLICT(resource_id,version) DO UPDATE SET title=EXCLUDED.title,status='draft',config=EXCLUDED.config,updated_at=now()`, resource, title, version, cfgJSON)
	if err != nil {
		jsonError(w, 500, "draft_save_failed", err.Error(), nil)
		return
	}
	jsonResponse(w, 200, map[string]any{"status": "draft_saved", "updated_at": time.Now().UTC(), "version": version})
}

// readBundleRequest accepts either a multipart form (`bundle` file + `config`
// text) or a raw zip body (legacy client), returning the bundle bytes and the
// optional submission config.
func readBundleRequest(w http.ResponseWriter, r *http.Request) ([]byte, []byte, error) {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		r.Body = http.MaxBytesReader(w, r.Body, syndication.MaxUploadBytes+(1<<20))
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			return nil, nil, err
		}
		file, _, err := r.FormFile("bundle")
		if err != nil {
			return nil, nil, fmt.Errorf("missing bundle part: %w", err)
		}
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, syndication.MaxUploadBytes+1))
		if err != nil {
			return nil, nil, err
		}
		if len(data) > syndication.MaxUploadBytes {
			return nil, nil, fmt.Errorf("bundle exceeds upload limit")
		}
		return data, []byte(r.FormValue("config")), nil
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, syndication.MaxUploadBytes+1))
	if err != nil {
		return nil, nil, err
	}
	if len(data) > syndication.MaxUploadBytes {
		return nil, nil, fmt.Errorf("bundle exceeds upload limit")
	}
	return data, nil, nil
}

func parseSubmissionConfig(raw []byte) syndication.SubmissionConfig {
	var cfg syndication.SubmissionConfig
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &cfg)
	}
	return cfg
}

// publishResource is the lightweight "提审" step. The bundle was already saved to
// the private resource repo by saveDraft; this only reads the saved manifest,
// builds the publication plan and opens the central index.csv PR.
func (a *application) publishResource(w http.ResponseWriter, r *http.Request, resource string, u userContext) {
	rawCfg, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	cfg := parseSubmissionConfig(rawCfg)
	if len(cfg.Targets) == 0 {
		cfg.Targets = []string{"oronbox"}
	}
	paid := syndication.NormalizePaidType(cfg.Catalog.PaidType)
	if err := validateSubmissionConfig(cfg, paid); err != nil {
		jsonError(w, 400, "invalid_config", err.Error(), nil)
		return
	}
	manifest, err := a.loadDraftManifest(r.Context(), resource)
	if err != nil {
		jsonError(w, 409, "draft_missing", "no saved draft for this resource; upload a draft first", nil)
		return
	}
	version := manifestVersion(manifest)
	plans, planErr := a.publicationPlans(r.Context(), cfg.Targets, manifest.Downloads)
	if planErr != nil {
		jsonError(w, 400, "invalid_targets", planErr.Error(), nil)
		return
	}
	cfgJSON, _ := json.Marshal(cfg)
	var submissionID string
	err = a.db.WithTx(r.Context(), func(tx pgx.Tx) error {
		if err := tx.QueryRow(r.Context(), `INSERT INTO resource_submissions(resource_id,title,version,creator_id,status,config) VALUES($1,$2,$3,$4,'waiting_review',$5) ON CONFLICT(resource_id,version) DO UPDATE SET status='waiting_review',title=EXCLUDED.title,config=EXCLUDED.config,updated_at=now() RETURNING id`, resource, manifest.Item.Tagline, version, u.ID, cfgJSON).Scan(&submissionID); err != nil {
			return err
		}
		for _, plan := range plans {
			planConfig, _ := json.Marshal(plan.Config)
			if _, err := tx.Exec(r.Context(), `INSERT INTO publications(submission_id,provider,category_id,config,state) VALUES($1,$2,$3,$4,'pending') ON CONFLICT(submission_id,provider,category_id) DO UPDATE SET config=EXCLUDED.config,state='pending',updated_at=now()`, submissionID, plan.Provider, plan.CategoryID, planConfig); err != nil {
				return err
			}
		}
		_, err := tx.Exec(r.Context(), `INSERT INTO review_cases(submission_id,state) VALUES($1,'pending') ON CONFLICT(submission_id) DO UPDATE SET state='pending',updated_at=now()`, submissionID)
		return err
	})
	if err != nil {
		jsonError(w, 500, "publish_failed", err.Error(), nil)
		return
	}
	prNumber := 0
	prURL := ""
	if a.cfg.Gitea.APIURL != "" && a.cfg.Gitea.ClientSecret != "" {
		owner, catalog := splitGiteaRepo(a.cfg.Gitea.CatalogRepo)
		if owner == "" || catalog == "" {
			jsonError(w, 500, "gitea_config_invalid", "GITEA_CATALOG_REPO must be owner/repository", nil)
			return
		}
		client := syndication.GiteaClient{BaseURL: a.cfg.Gitea.APIURL, Token: a.cfg.Gitea.ClientSecret}
		repoName := "oronbox-resource-" + resource
		commit, _ := client.GetBranchCommit(r.Context(), owner, repoName, "main")
		shortCommit := commit
		if len(shortCommit) > 7 {
			shortCommit = shortCommit[:7]
		}
		restype := strings.TrimSpace(manifest.Item.Restype)
		if restype == "" {
			_ = a.db.Pool.QueryRow(r.Context(), `SELECT COALESCE(restype,'') FROM resource_interactions WHERE resource_id=$1`, resource).Scan(&restype)
		}
		icon := strings.TrimSpace(manifest.Item.Icon)
		if icon == "" {
			icon = "media/icon.webp"
		}
		name := manifest.Item.Name
		if strings.TrimSpace(name) == "" {
			name = manifest.Item.Tagline
		}
		vendors := ""
		if devices, derr := a.fetchDevices(r.Context()); derr == nil {
			vendors = syndication.DeviceVendors(manifest.Downloads, devices)
		}
		var authorNames []string
		for _, a := range manifest.Item.Author {
			if strings.TrimSpace(a.Name) != "" {
				authorNames = append(authorNames, strings.TrimSpace(a.Name))
			}
		}
		authorStr := strings.Join(authorNames, ";")
		if authorStr == "" {
			authorStr = u.Username
		}
		repoPath := owner + "/" + repoName
		row := syndication.CatalogRow{
			ID:             resource,
			Name:           name,
			Restype:        restype,
			Author:         authorStr,
			Repo:           repoPath,
			RepoCommitHash: shortCommit,
			Icon:           icon,
			Cover:          strings.TrimSpace(manifest.Item.Cover),
			Tags:           strings.Join(cfg.Catalog.Tags, ";"),
			DeviceVendors:  vendors,
			Devices:        strings.Join(sortedDeviceKeys(manifest.Downloads), ";"),
			PaidType:       paid,
		}
		coordinator := syndication.SubmissionCoordinator{Gitea: client, Owner: owner, CatalogRepo: catalog}
		n, branch, submitErr := coordinator.SubmitIndex(r.Context(), resource, version, row)
		if submitErr != nil {
			_, _ = a.db.Pool.Exec(r.Context(), `UPDATE resource_submissions SET status='syndication_failed',updated_at=now() WHERE id=$1`, submissionID)
			jsonError(w, 502, "gitea_submit_failed", submitErr.Error(), nil)
			return
		}
		prNumber = n
		prURL = strings.TrimRight(a.cfg.GiteaPublicURL, "/") + "/" + owner + "/" + catalog + "/pulls/" + strconv.Itoa(n)
		_, _ = a.db.Pool.Exec(r.Context(), `UPDATE resource_submissions SET pr_number=$1,pr_url=$2,branch=$3,repo_commit_hash=$4,updated_at=now() WHERE id=$5`, prNumber, prURL, branch, commit, submissionID)
	}
	_, _ = a.db.Pool.Exec(r.Context(), `INSERT INTO user_messages(user_id,kind,event,data,ref) VALUES($1,'review','review.pr_opened',jsonb_build_object('resource_id',$2::text,'version',$3::text,'pr_number',$4::int,'pr_url',$5::text),$2)`, u.ID, resource, version, prNumber, prURL)
	jsonResponse(w, 200, map[string]any{"status": "waiting_review", "submission_id": submissionID, "pr_number": prNumber, "pr_url": prURL, "resource_id": resource})
}

// validateSubmissionConfig enforces the paid↔purchase coupling for 米坛: a paid
// resource cannot be mirrored to BandBBS without an external purchase link/price.
func validateSubmissionConfig(cfg syndication.SubmissionConfig, paid string) error {
	needsBandBBS := false
	for _, t := range cfg.Targets {
		if t == "bandbbs" {
			needsBandBBS = true
		}
	}
	if needsBandBBS && paid != "" {
		p := cfg.BandBBS.Purchase
		if p == nil || strings.TrimSpace(p.Link) == "" || p.Price <= 0 {
			return fmt.Errorf("bandbbs external purchase (link + price) is required for paid resources")
		}
	}
	return nil
}

// fetchDevices reads devices.json from the central repo (used for device_vendors).
func (a *application) fetchDevices(ctx context.Context) (map[string]syndication.DeviceSpec, error) {
	if a.cfg.Gitea.APIURL == "" || a.cfg.Gitea.ClientSecret == "" {
		return nil, fmt.Errorf("gitea is not configured")
	}
	owner, catalog := splitGiteaRepo(a.cfg.Gitea.CatalogRepo)
	if owner == "" || catalog == "" {
		return nil, fmt.Errorf("GITEA_CATALOG_REPO must be owner/repository")
	}
	client := syndication.GiteaClient{BaseURL: a.cfg.Gitea.APIURL, Token: a.cfg.Gitea.ClientSecret}
	raw, err := client.ReadFile(ctx, owner, catalog, "devices.json", "main")
	if err != nil {
		return nil, err
	}
	devices := map[string]syndication.DeviceSpec{}
	if err := json.Unmarshal(raw, &devices); err != nil {
		return nil, err
	}
	return devices, nil
}

// loadDraftManifest reads the manifest.json saved in the private resource repo.
func (a *application) loadDraftManifest(ctx context.Context, resource string) (syndication.Manifest, error) {
	if a.cfg.Gitea.APIURL == "" || a.cfg.Gitea.ClientSecret == "" {
		return syndication.Manifest{}, fmt.Errorf("gitea is not configured")
	}
	owner, _ := splitGiteaRepo(a.cfg.Gitea.CatalogRepo)
	if owner == "" {
		return syndication.Manifest{}, fmt.Errorf("GITEA_CATALOG_REPO must be owner/repository")
	}
	client := syndication.GiteaClient{BaseURL: a.cfg.Gitea.APIURL, Token: a.cfg.Gitea.ClientSecret}
	raw, err := client.ReadFile(ctx, owner, "oronbox-resource-"+resource, "manifest.json", "main")
	if err != nil {
		return syndication.Manifest{}, err
	}
	var m syndication.Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return syndication.Manifest{}, err
	}
	return m, nil
}

// manifestVersion resolves the submission version from the per-device downloads
// entries (the manifest has no top-level version).
func manifestVersion(m syndication.Manifest) string {
	keys := make([]string, 0, len(m.Downloads))
	for k := range m.Downloads {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if v := strings.TrimSpace(m.Downloads[k].Version); v != "" {
			return v
		}
	}
	return "0.0.1"
}

// publicationPlans derives concrete publication tasks from the creator's chosen
// platforms. bandbbs fans out per 米坛 category using the central repo's
// devices.json + publish.json, so category_id is never left at 0.
func (a *application) publicationPlans(ctx context.Context, targets []string, downloads map[string]syndication.DownloadEntry) ([]syndication.PublicationPlan, error) {
	needsBandBBS := false
	for _, t := range targets {
		if t == "bandbbs" {
			needsBandBBS = true
		}
	}
	if !needsBandBBS {
		return syndication.PlanPublications(targets, downloads, nil, syndication.PublishConfig{})
	}
	owner, catalog := splitGiteaRepo(a.cfg.Gitea.CatalogRepo)
	if owner == "" || catalog == "" {
		return nil, fmt.Errorf("GITEA_CATALOG_REPO must be owner/repository")
	}
	devices, err := a.fetchDevices(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not read devices.json: %w", err)
	}
	client := syndication.GiteaClient{BaseURL: a.cfg.Gitea.APIURL, Token: a.cfg.Gitea.ClientSecret}
	publish := syndication.PublishConfig{}
	raw, err := client.ReadFile(ctx, owner, catalog, "publish.json", "main")
	if err != nil {
		return nil, fmt.Errorf("could not read publish.json: %w", err)
	}
	if err := json.Unmarshal(raw, &publish); err != nil {
		return nil, fmt.Errorf("publish.json is invalid: %w", err)
	}
	return syndication.PlanPublications(targets, downloads, devices, publish)
}

// sortedDeviceKeys returns the manifest download device ids in stable order.
func sortedDeviceKeys(downloads map[string]syndication.DownloadEntry) []string {
	keys := make([]string, 0, len(downloads))
	for k := range downloads {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (a *application) pushBundleToGitea(ctx context.Context, resource, branch string, bundle syndication.Bundle, private bool) error {
	if a.cfg.Gitea.APIURL == "" || a.cfg.Gitea.ClientSecret == "" {
		return nil
	}
	owner, _ := splitGiteaRepo(a.cfg.Gitea.CatalogRepo)
	if owner == "" {
		return fmt.Errorf("GITEA_CATALOG_REPO must be owner/repository")
	}
	client := syndication.GiteaClient{BaseURL: a.cfg.Gitea.APIURL, Token: a.cfg.Gitea.ClientSecret}
	repo := "oronbox-resource-" + resource
	if err := client.CreateRepository(ctx, owner, repo, private); err != nil && !strings.Contains(strings.ToLower(err.Error()), "already exists") {
		return err
	}
	return pushBundleFiles(ctx, client, owner, repo, branch, bundle)
}

func pushBundleFiles(ctx context.Context, client syndication.GiteaClient, owner, repo, branch string, bundle syndication.Bundle) error {
	files := map[string][]byte{}
	for name, content := range bundle.Files {
		if name == "manifest.json" && branch != "main" {
			continue
		}
		files[name] = content
	}
	// One atomic tree commit for the whole bundle (strict mirror: extra files vanish).
	_, err := client.CommitFilesTree(ctx, owner, repo, branch, "save resource bundle", files)
	return err
}

func splitGiteaRepo(value string) (string, string) {
	parts := strings.SplitN(strings.Trim(value, "/"), "/", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}

func (a *application) listSyndications(w http.ResponseWriter, r *http.Request, resource string) {
	rows, err := a.db.Pool.Query(r.Context(), `SELECT p.id,p.provider,p.category_id,p.state,COALESCE(p.external_id,''),COALESCE(p.external_url,''),COALESCE(p.error_message,'') FROM publications p JOIN resource_submissions s ON s.id=p.submission_id WHERE s.resource_id=$1 ORDER BY p.provider,p.category_id`, resource)
	if err != nil {
		jsonError(w, 500, "syndications_failed", err.Error(), nil)
		return
	}
	defer rows.Close()
	groups := map[string][]any{}
	for rows.Next() {
		var id, provider, state, externalID, externalURL, message string
		var category int
		if err := rows.Scan(&id, &provider, &category, &state, &externalID, &externalURL, &message); err != nil {
			jsonError(w, 500, "syndications_failed", err.Error(), nil)
			return
		}
		groups[provider] = append(groups[provider], map[string]any{"task_id": id, "category_id": category, "state": state, "external_id": externalID, "external_url": externalURL, "error": message})
	}
	jsonResponse(w, 200, map[string]any{"resource_id": resource, "groups": groups})
}

func (a *application) retryPublication(w http.ResponseWriter, r *http.Request, resource, task string) {
	res, err := a.db.Pool.Exec(r.Context(), `UPDATE publications p SET state='dispatching',attempts=attempts+1,next_attempt_at=now(),updated_at=now() FROM resource_submissions s WHERE p.submission_id=s.id AND s.resource_id=$1 AND p.id=$2 AND p.state IN ('failed','cancelled')`, resource, strings.Trim(task, "/"))
	if err != nil {
		jsonError(w, 500, "retry_failed", err.Error(), nil)
		return
	}
	if res.RowsAffected() == 0 {
		jsonError(w, 409, "publication_not_retryable", "publication is not retryable", nil)
		return
	}
	jsonResponse(w, 200, map[string]string{"task_id": strings.Trim(task, "/"), "state": "dispatching"})
}

func (a *application) creatorReview(w http.ResponseWriter, r *http.Request, u userContext, parts []string) {
	if len(parts) == 0 && r.Method == http.MethodGet {
		// 创作者自身的审核队列/提审记录列表
		statusFilter := strings.TrimSpace(r.URL.Query().Get("status"))
		q := `SELECT s.id,s.resource_id,s.title,s.version,s.status,COALESCE(s.pr_number,0),COALESCE(s.pr_url,''),COALESCE(c.state,s.status),s.updated_at
			FROM resource_submissions s
			LEFT JOIN review_cases c ON c.submission_id=s.id
			WHERE s.creator_id=$1 OR s.resource_id IN (SELECT resource_id FROM resource_collaborators WHERE user_id=$1)`
		args := []any{u.ID}
		if statusFilter != "" {
			q += ` AND (s.status=$2 OR c.state=$2)`
			args = append(args, statusFilter)
		}
		q += ` ORDER BY s.updated_at DESC LIMIT 50`
		rows, err := a.db.Pool.Query(r.Context(), q, args...)
		if err != nil {
			jsonError(w, 500, "reviews_failed", err.Error(), nil)
			return
		}
		defer rows.Close()
		reviews := []any{}
		for rows.Next() {
			var subID, resID, title, ver, subStatus, cState string
			var prNum int
			var prURL string
			var updated time.Time
			if err := rows.Scan(&subID, &resID, &title, &ver, &subStatus, &prNum, &prURL, &cState, &updated); err == nil {
				reviews = append(reviews, map[string]any{
					"id":            subID,
					"submission_id": subID,
					"resource_id":   resID,
					"resource_name": title,
					"revision_id":   ver,
					"resource_kind": "quick_app",
					"title":         title,
					"version":       ver,
					"status":        cState,
					"state":         cState,
					"pr_number":     prNum,
					"pr_url":        prURL,
					"updated_at":    updated.Format(time.RFC3339),
					"need_fixes":    a.reviewNeedFixes(r.Context(), subID),
				})
			}
		}
		jsonResponse(w, 200, map[string]any{"items": reviews, "reviews": reviews, "total": len(reviews)})
		return
	}

	if len(parts) == 2 && parts[1] == "appeal" && r.Method == http.MethodPost {
		var in struct {
			Message string `json:"message"`
		}
		if !readJSON(w, r, &in) || strings.TrimSpace(in.Message) == "" {
			jsonError(w, 400, "message_required", "appeal message is required", nil)
			return
		}
		_, err := a.db.Pool.Exec(r.Context(), `INSERT INTO review_appeals(user_id,subject_type,subject_id,message) VALUES($1,'resource_review',$2,$3)`, u.ID, parts[0], in.Message)
		if err != nil {
			jsonError(w, 500, "appeal_failed", err.Error(), nil)
			return
		}
		jsonResponse(w, 201, map[string]string{"status": "open"})
		return
	}
	if len(parts) != 1 || r.Method != http.MethodGet {
		jsonError(w, 404, "not_found", "review endpoint not found", nil)
		return
	}
	var state, submissionID, resID, title, ver, note string
	var pr int
	var updated, createdAt time.Time
	err := a.db.Pool.QueryRow(r.Context(), `SELECT COALESCE(c.state,s.status),COALESCE(s.pr_number,0),s.id,s.resource_id,s.title,s.version,COALESCE(c.note,''),s.created_at,s.updated_at FROM resource_submissions s LEFT JOIN review_cases c ON c.submission_id=s.id WHERE (s.id::text=$1 OR s.resource_id=$1) AND (s.creator_id=$2 OR s.resource_id IN (SELECT resource_id FROM resource_collaborators WHERE user_id=$2)) ORDER BY s.updated_at DESC LIMIT 1`, parts[0], u.ID).Scan(&state, &pr, &submissionID, &resID, &title, &ver, &note, &createdAt, &updated)
	if err != nil {
		jsonError(w, 404, "review_not_found", "review case not found", nil)
		return
	}
	var restype string
	_ = a.db.Pool.QueryRow(r.Context(), `SELECT restype FROM resource_interactions WHERE resource_id=$1`, resID).Scan(&restype)
	if restype == "" {
		restype = "quick_app"
	}
	needFixes := a.reviewNeedFixes(r.Context(), submissionID)
	jsonResponse(w, 200, map[string]any{
		"submission_id": submissionID,
		"status":        state,
		"state":         state,
		"pr_number":     pr,
		"need_fixes":    needFixes,
		"resource_id":   resID,
		"title":         title,
		"version":       ver,
		"resource": map[string]any{
			"id":         resID,
			"slug":       resID,
			"draft_name": title,
			"kind":       restype,
			"updated_at": updated.Format(time.RFC3339),
		},
		"review": map[string]any{
			"id":            submissionID,
			"submission_id": submissionID,
			"resource_id":   resID,
			"resource_name": title,
			"state":         state,
			"status":        state,
			"note":          note,
			"pr_number":     pr,
			"created_at":    createdAt.Format(time.RFC3339),
			"updated_at":    updated.Format(time.RFC3339),
			"need_fixes":    needFixes,
		},
	})
}

// reviewNeedFixes reads the structured fix list from our database (the source of
// truth); the Gitea PR comment is only a mirror.
func (a *application) reviewNeedFixes(ctx context.Context, submissionID string) []map[string]any {
	items := []map[string]any{}
	rows, err := a.db.Pool.Query(ctx, `SELECT e.id,e.note,e.created_at FROM review_case_events e JOIN review_cases c ON c.id=e.case_id WHERE c.submission_id=$1 AND e.event='changes_requested' ORDER BY e.created_at`, submissionID)
	if err != nil {
		return items
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var note string
		var created time.Time
		if rows.Scan(&id, &note, &created) == nil {
			items = append(items, map[string]any{"id": strconv.FormatInt(id, 10), "message": note, "fixed": false, "created_at": created})
		}
	}
	return items
}

func (a *application) submitFixed(w http.ResponseWriter, r *http.Request, resource string) {
	if err := r.ParseMultipartForm(syndication.MaxUploadBytes + 2<<20); err != nil {
		jsonError(w, 400, "invalid_multipart", err.Error(), nil)
		return
	}
	file, _, err := r.FormFile("bundle")
	if err != nil {
		jsonError(w, 400, "bundle_required", "bundle.zip is required", nil)
		return
	}
	defer file.Close()
	data, err := io.ReadAll(http.MaxBytesReader(w, file, syndication.MaxUploadBytes+1))
	if err != nil {
		jsonError(w, 413, "bundle_too_large", "bundle exceeds upload limit", nil)
		return
	}
	bundle, err := syndication.ValidateBundle(data, resource)
	if err != nil {
		jsonError(w, 400, "invalid_bundle", err.Error(), nil)
		return
	}
	if err := a.pushBundleToGitea(r.Context(), resource, "main", bundle, true); err != nil {
		jsonError(w, 502, "gitea_fix_failed", err.Error(), nil)
		return
	}
	_, err = a.db.Pool.Exec(r.Context(), `UPDATE resource_submissions SET status='fixed_waiting',updated_at=now() WHERE resource_id=$1 AND status IN ('changes_requested','rejected','waiting_review')`, resource)
	if err != nil {
		jsonError(w, 500, "fix_submit_failed", err.Error(), nil)
		return
	}
	jsonResponse(w, 200, map[string]string{"status": "fixed_waiting"})
}

func (a *application) takedown(w http.ResponseWriter, r *http.Request, resource string, u userContext) {
	if !a.resourceAccess(r, resource, u.ID, true) {
		jsonError(w, 403, "owner_required", "only the owner can request takedown", nil)
		return
	}
	_, err := a.db.Pool.Exec(r.Context(), `UPDATE resource_submissions SET status='takedown_requested',updated_at=now() WHERE resource_id=$1; UPDATE publications p SET state='cancelled',updated_at=now() FROM resource_submissions s WHERE p.submission_id=s.id AND s.resource_id=$1`, resource)
	if err != nil {
		jsonError(w, 500, "takedown_failed", err.Error(), nil)
		return
	}
	jsonResponse(w, 200, map[string]string{"status": "takedown_requested"})
}

func (a *application) collaborators(w http.ResponseWriter, r *http.Request, resource string, u userContext, parts []string) {
	if len(parts) == 0 && r.Method == http.MethodGet {
		rows, err := a.db.Pool.Query(r.Context(), `SELECT c.user_id,u.username,c.role,c.status,c.invited_by,c.updated_at FROM resource_collaborators c JOIN users u ON u.id=c.user_id WHERE c.resource_id=$1 ORDER BY c.updated_at DESC`, resource)
		if err != nil {
			jsonError(w, 500, "collaborators_failed", err.Error(), nil)
			return
		}
		defer rows.Close()
		items := []any{}
		for rows.Next() {
			var id, username, role, status string
			var invitedBy *string
			var updated time.Time
			if rows.Scan(&id, &username, &role, &status, &invitedBy, &updated) == nil {
				items = append(items, map[string]any{"user_id": id, "username": username, "role": role, "status": status, "invited_by": invitedBy, "updated_at": updated})
			}
		}
		jsonResponse(w, 200, map[string]any{"items": items})
		return
	}
	if len(parts) == 0 && r.Method == http.MethodPost {
		if !a.resourceAccess(r, resource, u.ID, true) {
			jsonError(w, 403, "owner_required", "only the owner can invite collaborators", nil)
			return
		}
		var in struct{ Username, Role string }
		if !readJSON(w, r, &in) || in.Username == "" {
			jsonError(w, 400, "username_required", "username is required", nil)
			return
		}
		if in.Role == "" {
			in.Role = "collaborator"
		}
		var invited string
		if err := a.db.Pool.QueryRow(r.Context(), `SELECT id FROM users WHERE username=$1`, in.Username).Scan(&invited); err != nil || invited == u.ID {
			jsonError(w, 404, "user_not_found", "collaborator account not found", nil)
			return
		}
		_, err := a.db.Pool.Exec(r.Context(), `INSERT INTO resource_collaborators(resource_id,user_id,role,status,invited_by) VALUES($1,$2,$3,'pending',$4) ON CONFLICT(resource_id,user_id) DO UPDATE SET role=EXCLUDED.role,status='pending',invited_by=EXCLUDED.invited_by,updated_at=now()`, resource, invited, in.Role, u.ID)
		if err != nil {
			jsonError(w, 500, "collaborator_invite_failed", err.Error(), nil)
			return
		}
		_, _ = a.db.Pool.Exec(r.Context(), `INSERT INTO user_messages(user_id,kind,event,ref) VALUES($1,'creator','collab.invited',$2)`, invited, resource)
		jsonResponse(w, 201, map[string]string{"status": "pending", "user_id": invited})
		return
	}
	if len(parts) == 1 && r.Method == http.MethodDelete {
		if !a.resourceAccess(r, resource, u.ID, true) {
			jsonError(w, 403, "owner_required", "only the owner can remove collaborators", nil)
			return
		}
		_, err := a.db.Pool.Exec(r.Context(), `UPDATE resource_collaborators SET status=CASE WHEN status='pending' THEN 'revoked' ELSE 'removed' END,updated_at=now() WHERE resource_id=$1 AND user_id=$2`, resource, parts[0])
		if err != nil {
			jsonError(w, 500, "collaborator_remove_failed", err.Error(), nil)
			return
		}
		jsonResponse(w, 200, map[string]string{"status": "removed"})
		return
	}
	jsonError(w, 405, "method_not_allowed", "method not allowed", nil)
}

func (a *application) creatorCollaboration(w http.ResponseWriter, r *http.Request, u userContext, parts []string) {
	if len(parts) != 2 || (parts[1] != "accept" && parts[1] != "reject" && parts[1] != "leave") || r.Method != http.MethodPost {
		jsonError(w, 404, "not_found", "collaboration endpoint not found", nil)
		return
	}
	state := map[string]string{"accept": "accepted", "reject": "rejected", "leave": "left"}[parts[1]]
	_, err := a.db.Pool.Exec(r.Context(), `UPDATE resource_collaborators SET status=$1,updated_at=now() WHERE resource_id=$2 AND user_id=$3`, state, parts[0], u.ID)
	if err != nil {
		jsonError(w, 500, "collaboration_update_failed", err.Error(), nil)
		return
	}
	jsonResponse(w, 200, map[string]string{"status": state})
}
