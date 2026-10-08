package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/zxor-org/OronBox-Server/internal/syndication"
)

func (a *application) adminReviews(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) == 4 {
		var state, submission, note string
		var pr int
		if err := a.db.Pool.QueryRow(r.Context(), `SELECT c.state,c.note,c.submission_id,COALESCE(s.pr_number,0) FROM review_cases c JOIN resource_submissions s ON s.id=c.submission_id WHERE c.submission_id=$1`, parts[3]).Scan(&state, &note, &submission, &pr); err != nil {
			jsonError(w, 404, "review_not_found", "review case not found", nil)
			return
		}
		jsonResponse(w, 200, map[string]any{"submission_id": submission, "status": state, "note": note, "pr_number": pr, "need_fixes": []any{}})
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	query := `SELECT c.submission_id,c.state,c.priority,s.resource_id,s.title,s.version,s.pr_number,s.created_at FROM review_cases c JOIN resource_submissions s ON s.id=c.submission_id`
	args := []any{}
	if status != "" {
		query += ` WHERE c.state=$1`
		args = append(args, status)
	}
	query += ` ORDER BY c.priority DESC,c.created_at ASC LIMIT 200`
	rows, err := a.db.Pool.Query(r.Context(), query, args...)
	if err != nil {
		jsonError(w, 500, "reviews_failed", err.Error(), nil)
		return
	}
	defer rows.Close()
	items := []any{}
	for rows.Next() {
		var id, state, resource, title, version string
		var priority, pr int
		var created time.Time
		if rows.Scan(&id, &state, &priority, &resource, &title, &version, &pr, &created) == nil {
			items = append(items, map[string]any{"submission_id": id, "status": state, "priority": priority, "resource_id": resource, "title": title, "version": version, "pr_number": pr, "created_at": created})
		}
	}
	jsonResponse(w, 200, map[string]any{"items": items})
}

func (a *application) adminResources(w http.ResponseWriter, r *http.Request) {
	owner, _ := splitGiteaRepo(a.cfg.Gitea.CatalogRepo)
	if owner == "" {
		owner = "OronBoxBot"
	}
	rows, err := a.db.Pool.Query(r.Context(), `
		SELECT
			ri.resource_id,
			COALESCE(NULLIF(ri.title, ''), ri.resource_id) AS title,
			ri.restype,
			COALESCE(u.username, '') AS author,
			COALESCE(s.status, 'published') AS status,
			ri.updated_at
		FROM resource_interactions ri
		LEFT JOIN users u ON u.id = ri.owner_id
		LEFT JOIN LATERAL (
			SELECT status FROM resource_submissions WHERE resource_id = ri.resource_id ORDER BY updated_at DESC LIMIT 1
		) s ON true
		ORDER BY ri.updated_at DESC
		LIMIT 200
	`)
	if err != nil {
		jsonError(w, 500, "resources_failed", err.Error(), nil)
		return
	}
	defer rows.Close()
	items := []any{}
	for rows.Next() {
		var id, title, restype, author, status string
		var updated time.Time
		if rows.Scan(&id, &title, &restype, &author, &status, &updated) == nil {
			items = append(items, map[string]any{
				"id":         id,
				"name":       title,
				"type":       restype,
				"author":     author,
				"repo":       owner + "/oronbox-resource-" + id,
				"status":     status,
				"updated_at": updated.Format(time.RFC3339),
			})
		}
	}
	jsonResponse(w, 200, map[string]any{"items": items})
}

func (a *application) adminResourceUpdate(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 4 {
		jsonError(w, 400, "invalid_id", "resource id is required", nil)
		return
	}
	resourceID := parts[3]
	var in struct {
		Title   string `json:"title"`
		Restype string `json:"restype"`
		Status  string `json:"status"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	_, err := a.db.Pool.Exec(r.Context(), `
		UPDATE resource_interactions 
		SET title = CASE WHEN $1 <> '' THEN $1 ELSE title END,
		    restype = CASE WHEN $2 <> '' THEN $2 ELSE restype END,
		    updated_at = now()
		WHERE resource_id = $3
	`, in.Title, in.Restype, resourceID)
	if err != nil {
		jsonError(w, 500, "resource_update_failed", err.Error(), nil)
		return
	}
	if in.Status != "" {
		_, _ = a.db.Pool.Exec(r.Context(), `
			UPDATE resource_submissions
			SET status = $1, updated_at = now()
			WHERE id = (SELECT id FROM resource_submissions WHERE resource_id = $2 ORDER BY updated_at DESC LIMIT 1)
		`, in.Status, resourceID)
	}
	jsonResponse(w, 200, map[string]any{"status": "updated"})
}

func (a *application) adminReviewDetail(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 4 {
		jsonError(w, 404, "submission_not_found", "submission id is required", nil)
		return
	}
	submissionID := parts[3]
	var resourceID, title, version, status, creatorID string
	var prNumber int
	var configBytes []byte
	var createdAt, updatedAt time.Time
	err := a.db.Pool.QueryRow(r.Context(), `
		SELECT s.resource_id, s.title, s.version, s.status, COALESCE(s.pr_number, 0), s.config, s.creator_id, s.created_at, s.updated_at
		FROM resource_submissions s
		WHERE s.id = $1
	`, submissionID).Scan(&resourceID, &title, &version, &status, &prNumber, &configBytes, &creatorID, &createdAt, &updatedAt)
	if err != nil {
		jsonError(w, 404, "review_not_found", "review submission not found", nil)
		return
	}

	var cfg syndication.SubmissionConfig
	if len(configBytes) > 0 {
		_ = json.Unmarshal(configBytes, &cfg)
	}

	manifest, _ := a.loadDraftManifest(r.Context(), resourceID)
	owner, _ := splitGiteaRepo(a.cfg.Gitea.CatalogRepo)
	if owner == "" {
		owner = "OronBoxBot"
	}

	var changelog syndication.Changelog
	if a.cfg.Gitea.APIURL != "" && a.cfg.Gitea.ClientSecret != "" {
		client := syndication.GiteaClient{BaseURL: a.cfg.Gitea.APIURL, Token: a.cfg.Gitea.ClientSecret}
		if clBytes, err := client.ReadFile(r.Context(), owner, "oronbox-resource-"+resourceID, "changelog.json", "main"); err == nil {
			_ = json.Unmarshal(clBytes, &changelog)
		}
	}

	downloadBase := ""
	if a.cfg.Gitea.APIURL != "" {
		downloadBase = fmt.Sprintf("%s/%s/oronbox-resource-%s/raw/branch/main", strings.TrimRight(a.cfg.Gitea.APIURL, "/api/v1"), owner, resourceID)
	}

	jsonResponse(w, 200, map[string]any{
		"submission_id": submissionID,
		"resource_id":   resourceID,
		"title":         title,
		"version":       version,
		"status":        status,
		"pr_number":     prNumber,
		"creator_id":    creatorID,
		"config":        cfg,
		"manifest":      manifest,
		"changelog":     changelog,
		"created_at":    createdAt,
		"updated_at":    updatedAt,
		"repo":          owner + "/oronbox-resource-" + resourceID,
		"download_url":  downloadBase,
		"icon_url":      fmt.Sprintf("%s/%s", downloadBase, manifest.Item.Icon),
		"cover_url":     fmt.Sprintf("%s/%s", downloadBase, manifest.Item.Cover),
	})
}

func (a *application) adminReviewMutation(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 4 {
		jsonError(w, 404, "review_not_found", "submission id is required", nil)
		return
	}
	var in struct {
		Action    string `json:"action"`
		Note      string `json:"note"`
		Message   string `json:"message"`
		Checklist any    `json:"checklist"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if strings.HasSuffix(r.URL.Path, "/checklist") {
		_, err := a.db.Pool.Exec(r.Context(), `UPDATE review_cases SET checklist=$1,note=$2,updated_at=now() WHERE submission_id=$3`, in.Checklist, in.Note, parts[3])
		if err != nil {
			jsonError(w, 500, "checklist_failed", err.Error(), nil)
			return
		}
		jsonResponse(w, 200, map[string]string{"status": "saved"})
		return
	}
	state := "approved"
	submissionState := "merged"
	switch in.Action {
	case "request_changes":
		state, submissionState = "changes_requested", "changes_requested"
	case "reject":
		state, submissionState = "rejected", "rejected"
	case "approve":
	default:
		jsonError(w, 400, "invalid_decision", "action must be approve, request_changes, or reject", nil)
		return
	}
	note := in.Note
	if note == "" {
		note = in.Message
	}
	var prNumber int
	var resourceID string
	if err := a.db.Pool.QueryRow(r.Context(), `SELECT COALESCE(pr_number,0),resource_id FROM resource_submissions WHERE id=$1`, parts[3]).Scan(&prNumber, &resourceID); err != nil {
		jsonError(w, 404, "review_not_found", "submission not found", nil)
		return
	}
	err := a.db.WithTx(r.Context(), func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(), `UPDATE review_cases SET state=$1,note=$2,updated_at=now() WHERE submission_id=$3`, state, note, parts[3]); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `UPDATE resource_submissions SET status=$1,updated_at=now() WHERE id=$2`, submissionState, parts[3]); err != nil {
			return err
		}
		var caseID string
		if err := tx.QueryRow(r.Context(), `SELECT id FROM review_cases WHERE submission_id=$1`, parts[3]).Scan(&caseID); err == nil {
			if _, err := tx.Exec(r.Context(), `INSERT INTO review_case_events(case_id,event,note) VALUES($1,$2,$3)`, caseID, state, note); err != nil {
				return err
			}
		}
		if submissionState == "rejected" {
			if _, err := tx.Exec(r.Context(), `UPDATE publications SET state='cancelled',updated_at=now() WHERE submission_id=$1 AND state IN ('pending','dispatching')`, parts[3]); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		jsonError(w, 500, "decision_failed", err.Error(), nil)
		return
	}

	// Drive Gitea directly (no webhook): on approve merge the PR and flip the
	// resource repo public so the worker can dispatch downstream publications.
	if a.cfg.Gitea.APIURL != "" && a.cfg.Gitea.ClientSecret != "" {
		owner, catalog := splitGiteaRepo(a.cfg.Gitea.CatalogRepo)
		client := syndication.GiteaClient{BaseURL: a.cfg.Gitea.APIURL, Token: a.cfg.Gitea.ClientSecret}
		if submissionState == "merged" && prNumber > 0 {
			_ = client.MergePullRequest(r.Context(), owner, catalog, prNumber)
			_ = client.SetPrivate(r.Context(), owner, "oronbox-resource-"+resourceID, false)
		}
		if state == "changes_requested" && prNumber > 0 && note != "" {
			id := parts[3]
			if len(id) > 8 {
				id = id[:8]
			}
			body := fmt.Sprintf("<!-- oronbox-meta: {\"role\":\"reviewer\",\"tag\":\"NEEDFIX\",\"id\":%q} -->\n%s", id, note)
			_ = client.CreateIssueComment(r.Context(), owner, catalog, prNumber, body)
		}
	}

	jsonResponse(w, 200, map[string]string{"status": state})
}

func (a *application) adminComments(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Pool.Query(r.Context(), `SELECT c.id,c.resource_id,c.user_id,COALESCE(u.username,''),c.content,c.state,c.is_deleted,COALESCE(c.moderation_reason,''),COALESCE(c.ai_action,'pass'),COALESCE(c.ai_reason,''),c.created_at FROM resource_comments c LEFT JOIN users u ON u.id=c.user_id ORDER BY c.created_at DESC LIMIT 200`)
	if err != nil {
		jsonError(w, 500, "comments_failed", err.Error(), nil)
		return
	}
	defer rows.Close()
	items := []any{}
	for rows.Next() {
		var id, resource, user, username, body, state, modReason, aiAction, aiReason string
		var deleted bool
		var created time.Time
		if rows.Scan(&id, &resource, &user, &username, &body, &state, &deleted, &modReason, &aiAction, &aiReason, &created) == nil {
			items = append(items, map[string]any{
				"id":                id,
				"resource_id":       resource,
				"user_id":           user,
				"username":          username,
				"content":           body,
				"state":             state,
				"is_deleted":        deleted,
				"moderation_reason": modReason,
				"ai_action":         aiAction,
				"ai_reason":         aiReason,
				"created_at":        created,
			})
		}
	}
	jsonResponse(w, 200, map[string]any{"items": items})
}

func (a *application) adminCommentsBulk(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs    []string `json:"comment_ids"`
		Action string   `json:"action"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	state := "visible"
	deleted := false
	if in.Action == "hide" {
		state, deleted = "hidden", true
	}
	count := 0
	for _, id := range in.IDs {
		if result, err := a.db.Pool.Exec(r.Context(), `UPDATE resource_comments SET state=$1,is_deleted=$2 WHERE id=$3`, state, deleted, id); err == nil {
			count += int(result.RowsAffected())
		}
	}
	jsonResponse(w, 200, map[string]any{"updated": count})
}

func (a *application) adminUsers(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) == 4 {
		var id, username, avatar, role string
		var uid int64
		var banned bool
		var created time.Time
		var coinBalance int64
		if err := a.db.Pool.QueryRow(r.Context(), `
			SELECT u.id, u.bandbbs_uid, u.username, COALESCE(u.avatar_url,''), u.role, u.banned, u.created_at, COALESCE(c.balance, 0)
			FROM users u
			LEFT JOIN user_coin_accounts c ON c.user_id = u.id
			WHERE u.id=$1
		`, parts[3]).Scan(&id, &uid, &username, &avatar, &role, &banned, &created, &coinBalance); err != nil {
			jsonError(w, 404, "user_not_found", "user not found", nil)
			return
		}
		jsonResponse(w, 200, map[string]any{"id": id, "bandbbs_uid": uid, "username": username, "avatar_url": avatar, "role": role, "banned": banned, "coins": coinBalance, "created_at": created})
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	if perPage < 1 || perPage > 200 {
		perPage = 50
	}
	rows, err := a.db.Pool.Query(r.Context(), `
		SELECT u.id, u.bandbbs_uid, u.username, COALESCE(u.avatar_url,''), u.role, u.banned, u.created_at, COALESCE(c.balance, 0)
		FROM users u
		LEFT JOIN user_coin_accounts c ON c.user_id = u.id
		ORDER BY u.created_at DESC
		LIMIT $1 OFFSET $2
	`, perPage, (page-1)*perPage)
	if err != nil {
		jsonError(w, 500, "users_failed", err.Error(), nil)
		return
	}
	defer rows.Close()
	items := []any{}
	for rows.Next() {
		var id, username, avatar, role string
		var uid int64
		var banned bool
		var created time.Time
		var coinBalance int64
		if rows.Scan(&id, &uid, &username, &avatar, &role, &banned, &created, &coinBalance) == nil {
			items = append(items, map[string]any{"id": id, "bandbbs_uid": uid, "username": username, "avatar_url": avatar, "role": role, "banned": banned, "coins": coinBalance, "created_at": created})
		}
	}
	jsonResponse(w, 200, map[string]any{"items": items, "page": page, "per_page": perPage})
}

func (a *application) adminTickets(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) == 4 {
		ticketID := parts[3]
		var id, user, username, targetSource, title, content, state string
		var created, updated time.Time
		if err := a.db.Pool.QueryRow(r.Context(), `SELECT t.id,t.user_id,COALESCE(u.username,''),COALESCE(t.target_source,''),t.title,t.content,t.status,t.created_at,t.updated_at FROM feedback_tickets t LEFT JOIN users u ON u.id=t.user_id WHERE t.id=$1`, ticketID).Scan(&id, &user, &username, &targetSource, &title, &content, &state, &created, &updated); err != nil {
			jsonError(w, 404, "ticket_not_found", "ticket not found", nil)
			return
		}
		rows, err := a.db.Pool.Query(r.Context(), `SELECT r.id,r.author_id,COALESCE(u.username,''),r.message,r.is_admin,r.created_at FROM feedback_replies r LEFT JOIN users u ON u.id=r.author_id WHERE r.ticket_id=$1 ORDER BY r.created_at`, ticketID)
		if err != nil {
			jsonError(w, 500, "replies_failed", err.Error(), nil)
			return
		}
		defer rows.Close()
		replies := []any{}
		for rows.Next() {
			var rid, author, aname, body string
			var isAdmin bool
			var at time.Time
			if rows.Scan(&rid, &author, &aname, &body, &isAdmin, &at) == nil {
				replies = append(replies, map[string]any{"id": rid, "author_id": author, "username": aname, "message": body, "is_admin": isAdmin, "created_at": at})
			}
		}
		jsonResponse(w, 200, map[string]any{"ticket": map[string]any{"id": id, "user_id": user, "username": username, "target_source": targetSource, "title": title, "content": content, "status": state, "created_at": created, "updated_at": updated}, "replies": replies})
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	query := `SELECT t.id,t.user_id,COALESCE(u.username,''),COALESCE(t.target_source,''),t.title,t.content,t.status,t.created_at,t.updated_at FROM feedback_tickets t LEFT JOIN users u ON u.id=t.user_id`
	args := []any{}
	if status != "" {
		query += ` WHERE t.status=$1`
		args = append(args, status)
	}
	query += ` ORDER BY t.updated_at DESC LIMIT 200`
	rows, err := a.db.Pool.Query(r.Context(), query, args...)
	if err != nil {
		jsonError(w, 500, "tickets_failed", err.Error(), nil)
		return
	}
	defer rows.Close()
	items := []any{}
	for rows.Next() {
		var id, user, username, targetSource, title, content, state string
		var created, updated time.Time
		if rows.Scan(&id, &user, &username, &targetSource, &title, &content, &state, &created, &updated) == nil {
			items = append(items, map[string]any{"id": id, "user_id": user, "username": username, "target_source": targetSource, "title": title, "content": content, "status": state, "created_at": created, "updated_at": updated})
		}
	}
	jsonResponse(w, 200, map[string]any{"items": items})
}

func (a *application) adminTicketMutation(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 4 {
		jsonError(w, 404, "ticket_not_found", "ticket not found", nil)
		return
	}
	var in struct {
		Action       string `json:"action"`
		Message      string `json:"message"`
		InternalNote string `json:"internal_note"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.Message != "" {
		_, _ = a.db.Pool.Exec(r.Context(), `INSERT INTO feedback_replies(ticket_id,author_id,message,is_admin) SELECT $1,user_id,$2,true FROM feedback_tickets WHERE id=$1`, parts[3], in.Message)
	}
	state := "open"
	if strings.Contains(in.Action, "resolve") {
		state = "resolved"
	}
	if strings.Contains(in.Action, "close") {
		state = "closed"
	}
	_, err := a.db.Pool.Exec(r.Context(), `UPDATE feedback_tickets SET status=$1,updated_at=now() WHERE id=$2`, state, parts[3])
	if err != nil {
		jsonError(w, 500, "ticket_update_failed", err.Error(), nil)
		return
	}
	jsonResponse(w, 200, map[string]string{"status": state})
}

func (a *application) adminPublications(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Pool.Query(r.Context(), `SELECT id,submission_id,provider,category_id,state,attempts,next_attempt_at,external_url FROM publications ORDER BY updated_at DESC LIMIT 200`)
	if err != nil {
		jsonError(w, 500, "publications_failed", err.Error(), nil)
		return
	}
	defer rows.Close()
	items := []any{}
	for rows.Next() {
		var id, submission, provider, state, url string
		var category, attempts int
		var next *time.Time
		if rows.Scan(&id, &submission, &provider, &category, &state, &attempts, &next, &url) == nil {
			items = append(items, map[string]any{"id": id, "submission_id": submission, "provider": provider, "category_id": category, "state": state, "attempts": attempts, "next_attempt_at": next, "external_url": url})
		}
	}
	jsonResponse(w, 200, map[string]any{"items": items})
}

func (a *application) adminPublicationRetry(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 4 {
		jsonError(w, 404, "publication_not_found", "publication not found", nil)
		return
	}
	result, err := a.db.Pool.Exec(r.Context(), `UPDATE publications SET state='dispatching',attempts=attempts+1,next_attempt_at=now(),updated_at=now() WHERE id=$1 AND state IN ('failed','cancelled')`, parts[3])
	if err != nil {
		jsonError(w, 500, "publication_retry_failed", err.Error(), nil)
		return
	}
	if result.RowsAffected() == 0 {
		jsonError(w, 409, "publication_not_retryable", "publication is not retryable", nil)
		return
	}
	jsonResponse(w, 200, map[string]string{"status": "dispatching"})
}

func (a *application) adminCoins(w http.ResponseWriter, r *http.Request) {
	var balance, accounts int64
	_ = a.db.Pool.QueryRow(r.Context(), `SELECT COALESCE(SUM(balance),0),count(*) FROM user_coin_accounts`).Scan(&balance, &accounts)
	jsonResponse(w, 200, map[string]any{"total_balance": balance, "accounts": accounts})
}

func (a *application) adminLedger(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Pool.Query(r.Context(), `SELECT id,user_id,delta_units,kind,reference_type,reference_id,note,balance_after,created_at FROM coin_ledger ORDER BY created_at DESC LIMIT 100`)
	if err != nil {
		jsonError(w, 500, "ledger_failed", err.Error(), nil)
		return
	}
	defer rows.Close()
	items := []any{}
	for rows.Next() {
		var id, user, kind, referenceType, ref, note string
		var delta, balanceAfter int64
		var created time.Time
		if rows.Scan(&id, &user, &delta, &kind, &referenceType, &ref, &note, &balanceAfter, &created) == nil {
			items = append(items, map[string]any{"id": id, "user_id": user, "delta_units": delta, "kind": kind, "reference_type": referenceType, "reference_id": ref, "note": note, "balance_after": balanceAfter, "created_at": created})
		}
	}
	jsonResponse(w, 200, map[string]any{"items": items})
}

func (a *application) adminAudit(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Pool.Query(r.Context(), `SELECT id,actor_user_id,action,result,ip,target_data,created_at FROM audit_logs ORDER BY created_at DESC LIMIT 100`)
	if err != nil {
		jsonError(w, 500, "audit_failed", err.Error(), nil)
		return
	}
	defer rows.Close()
	items := []any{}
	for rows.Next() {
		var id int64
		var actor *string
		var action, result string
		var ip *string
		var target any
		var created time.Time
		if rows.Scan(&id, &actor, &action, &result, &ip, &target, &created) == nil {
			items = append(items, map[string]any{"id": id, "actor_user_id": actor, "action": action, "result": result, "ip": ip, "target_data": target, "created_at": created})
		}
	}
	jsonResponse(w, 200, map[string]any{"items": items})
}

func (a *application) adminAnalytics(w http.ResponseWriter, r *http.Request) {
	var totalUsers, activeUsers7d, activeUsers30d int64
	_ = a.db.Pool.QueryRow(r.Context(), `SELECT count(*) FROM users`).Scan(&totalUsers)
	_ = a.db.Pool.QueryRow(r.Context(), `SELECT count(DISTINCT user_id) FROM sessions WHERE last_seen_at >= now() - interval '7 days'`).Scan(&activeUsers7d)
	_ = a.db.Pool.QueryRow(r.Context(), `SELECT count(DISTINCT user_id) FROM sessions WHERE last_seen_at >= now() - interval '30 days'`).Scan(&activeUsers30d)

	var totalDownloads, downloads7d int64
	_ = a.db.Pool.QueryRow(r.Context(), `SELECT count(*) FROM download_events`).Scan(&totalDownloads)
	_ = a.db.Pool.QueryRow(r.Context(), `SELECT count(*) FROM download_events WHERE created_at >= now() - interval '7 days'`).Scan(&downloads7d)

	var totalCoinsTipped, coinsTipped7d int64
	_ = a.db.Pool.QueryRow(r.Context(), `SELECT COALESCE(SUM(amount), 0) FROM resource_coin_votes`).Scan(&totalCoinsTipped)
	_ = a.db.Pool.QueryRow(r.Context(), `SELECT COALESCE(SUM(amount), 0) FROM resource_coin_votes WHERE created_at >= now() - interval '7 days'`).Scan(&coinsTipped7d)

	// Daily trend for the past 7 days
	type dailyPoint struct {
		Date      string `json:"date"`
		Downloads int64  `json:"downloads"`
		Tips      int64  `json:"tips"`
	}
	points := make([]dailyPoint, 0, 7)
	for i := 6; i >= 0; i-- {
		t := time.Now().UTC().AddDate(0, 0, -i)
		dateStr := t.Format("2006-01-02")
		var dl, tp int64
		_ = a.db.Pool.QueryRow(r.Context(), `SELECT count(*) FROM download_events WHERE created_at::date = $1::date`, dateStr).Scan(&dl)
		_ = a.db.Pool.QueryRow(r.Context(), `SELECT COALESCE(SUM(amount), 0) FROM resource_coin_votes WHERE created_at::date = $1::date`, dateStr).Scan(&tp)
		points = append(points, dailyPoint{Date: dateStr, Downloads: dl, Tips: tp})
	}

	jsonResponse(w, 200, map[string]any{
		"users": map[string]any{
			"total": totalUsers,
			"dau":   activeUsers7d / 7,
			"wau":   activeUsers7d,
			"mau":   activeUsers30d,
		},
		"downloads": map[string]any{
			"total":     totalDownloads,
			"past_7d":   downloads7d,
		},
		"coins": map[string]any{
			"total_tipped": totalCoinsTipped,
			"tipped_7d":    coinsTipped7d,
		},
		"trends": points,
	})
}

func (a *application) adminBlogs(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Pool.Query(r.Context(), `
		SELECT b.id, b.title, b.slug, b.summary, b.content, b.cover_url, b.category, b.status, b.view_count, b.created_at, b.updated_at, COALESCE(u.username, '')
		FROM blogs b
		LEFT JOIN users u ON u.id = b.author_id
		ORDER BY b.created_at DESC LIMIT 100
	`)
	if err != nil {
		jsonError(w, 500, "blogs_failed", err.Error(), nil)
		return
	}
	defer rows.Close()

	items := []any{}
	for rows.Next() {
		var id, title, slug, summary, content, cover, category, status, author string
		var viewCount int
		var created, updated time.Time
		if err := rows.Scan(&id, &title, &slug, &summary, &content, &cover, &category, &status, &viewCount, &created, &updated, &author); err == nil {
			items = append(items, map[string]any{
				"id":         id,
				"title":      title,
				"slug":       slug,
				"summary":    summary,
				"content":    content,
				"cover_url":  cover,
				"category":   category,
				"status":     status,
				"view_count": viewCount,
				"author":     author,
				"created_at": created.Format(time.RFC3339),
				"updated_at": updated.Format(time.RFC3339),
			})
		}
	}
	jsonResponse(w, 200, map[string]any{"items": items})
}

func (a *application) adminBlogMutation(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	// e.g. /admin/api/blogs (POST create) or /admin/api/blogs/{id} (PUT/DELETE)
	if r.Method == "POST" && len(parts) == 3 {
		var in struct {
			Title    string `json:"title"`
			Slug     string `json:"slug"`
			Summary  string `json:"summary"`
			Content  string `json:"content"`
			CoverURL string `json:"cover_url"`
			Category string `json:"category"`
			Status   string `json:"status"`
		}
		if !readJSON(w, r, &in) {
			return
		}
		if in.Title == "" {
			jsonError(w, 400, "invalid_title", "title is required", nil)
			return
		}
		if in.Slug == "" {
			in.Slug = fmt.Sprintf("post-%d", time.Now().Unix())
		}
		if in.Status == "" {
			in.Status = "published"
		}
		var id string
		err := a.db.Pool.QueryRow(r.Context(), `
			INSERT INTO blogs(title, slug, summary, content, cover_url, category, status)
			VALUES($1, $2, $3, $4, $5, $6, $7)
			RETURNING id
		`, in.Title, in.Slug, in.Summary, in.Content, in.CoverURL, in.Category, in.Status).Scan(&id)
		if err != nil {
			jsonError(w, 500, "blog_create_failed", err.Error(), nil)
			return
		}
		jsonResponse(w, 201, map[string]any{"id": id, "status": in.Status})
		return
	}

	if len(parts) < 4 {
		jsonError(w, 400, "id_required", "blog id is required", nil)
		return
	}
	id := parts[3]

	if r.Method == "DELETE" {
		_, err := a.db.Pool.Exec(r.Context(), `DELETE FROM blogs WHERE id = $1`, id)
		if err != nil {
			jsonError(w, 500, "blog_delete_failed", err.Error(), nil)
			return
		}
		jsonResponse(w, 200, map[string]any{"deleted": true})
		return
	}

	if r.Method == "PUT" || r.Method == "POST" {
		var in struct {
			Title    string `json:"title"`
			Summary  string `json:"summary"`
			Content  string `json:"content"`
			CoverURL string `json:"cover_url"`
			Category string `json:"category"`
			Status   string `json:"status"`
		}
		if !readJSON(w, r, &in) {
			return
		}
		_, err := a.db.Pool.Exec(r.Context(), `
			UPDATE blogs
			SET title = CASE WHEN $1 <> '' THEN $1 ELSE title END,
			    summary = CASE WHEN $2 <> '' THEN $2 ELSE summary END,
			    content = CASE WHEN $3 <> '' THEN $3 ELSE content END,
			    cover_url = CASE WHEN $4 <> '' THEN $4 ELSE cover_url END,
			    category = CASE WHEN $5 <> '' THEN $5 ELSE category END,
			    status = CASE WHEN $6 <> '' THEN $6 ELSE status END,
			    updated_at = now()
			WHERE id = $7
		`, in.Title, in.Summary, in.Content, in.CoverURL, in.Category, in.Status, id)
		if err != nil {
			jsonError(w, 500, "blog_update_failed", err.Error(), nil)
			return
		}
		jsonResponse(w, 200, map[string]any{"status": "updated"})
		return
	}

	jsonError(w, 405, "method_not_allowed", "method not allowed", nil)
}

