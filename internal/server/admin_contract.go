package server

import (
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
	rows, err := a.db.Pool.Query(r.Context(), `SELECT id,resource_id,user_id,content,state,is_deleted,created_at FROM resource_comments ORDER BY created_at DESC LIMIT 200`)
	if err != nil {
		jsonError(w, 500, "comments_failed", err.Error(), nil)
		return
	}
	defer rows.Close()
	items := []any{}
	for rows.Next() {
		var id, resource, user, body, state string
		var deleted bool
		var created time.Time
		if rows.Scan(&id, &resource, &user, &body, &state, &deleted, &created) == nil {
			items = append(items, map[string]any{"id": id, "resource_id": resource, "user_id": user, "content": body, "state": state, "is_deleted": deleted, "created_at": created})
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
		if err := a.db.Pool.QueryRow(r.Context(), `SELECT id,bandbbs_uid,username,COALESCE(avatar_url,''),role,banned,created_at FROM users WHERE id=$1`, parts[3]).Scan(&id, &uid, &username, &avatar, &role, &banned, &created); err != nil {
			jsonError(w, 404, "user_not_found", "user not found", nil)
			return
		}
		jsonResponse(w, 200, map[string]any{"id": id, "bandbbs_uid": uid, "username": username, "avatar_url": avatar, "role": role, "banned": banned, "created_at": created})
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
	rows, err := a.db.Pool.Query(r.Context(), `SELECT id,bandbbs_uid,username,COALESCE(avatar_url,''),role,banned,created_at FROM users ORDER BY created_at DESC LIMIT $1 OFFSET $2`, perPage, (page-1)*perPage)
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
		if rows.Scan(&id, &uid, &username, &avatar, &role, &banned, &created) == nil {
			items = append(items, map[string]any{"id": id, "bandbbs_uid": uid, "username": username, "avatar_url": avatar, "role": role, "banned": banned, "created_at": created})
		}
	}
	jsonResponse(w, 200, map[string]any{"items": items, "page": page, "per_page": perPage})
}

func (a *application) adminTickets(w http.ResponseWriter, r *http.Request) {
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	query := `SELECT id,user_id,COALESCE(target_source,''),title,status,created_at,updated_at FROM feedback_tickets`
	args := []any{}
	if status != "" {
		query += ` WHERE status=$1`
		args = append(args, status)
	}
	query += ` ORDER BY updated_at DESC LIMIT 200`
	rows, err := a.db.Pool.Query(r.Context(), query, args...)
	if err != nil {
		jsonError(w, 500, "tickets_failed", err.Error(), nil)
		return
	}
	defer rows.Close()
	items := []any{}
	for rows.Next() {
		var id, user, targetSource, title, state string
		var created, updated time.Time
		if rows.Scan(&id, &user, &targetSource, &title, &state, &created, &updated) == nil {
			items = append(items, map[string]any{"id": id, "user_id": user, "target_source": targetSource, "title": title, "status": state, "created_at": created, "updated_at": updated})
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
