package community

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/zxor-org/OronBox-Server/internal/httputil"
	"github.com/zxor-org/OronBox-Server/internal/moderation"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

type CommentItem struct {
	ID        int64  `json:"id"`
	UserUID   int    `json:"user_uid"`
	Username  string `json:"username"`
	AvatarURL string `json:"avatar_url"`
	Content   string `json:"content"`
	ParentID  *int64 `json:"parent_id,omitempty"`
	CreatedAt string `json:"created_at"`
}

// HandleGetComments implements GET /api/v1/resources/:id/comments
func HandleGetComments(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) < 4 {
			httputil.Error(w, http.StatusBadRequest, "invalid_path", "路径错误")
			return
		}
		resID := parts[3]
		page := httputil.ParseInt(r.URL.Query().Get("page"), 1)
		size := httputil.ParseInt(r.URL.Query().Get("size"), 50)
		if page < 1 {
			page = 1
		}
		if size < 1 {
			size = 1
		}
		if size > 100 {
			size = 100
		}
		offset := (page - 1) * size

		rows, err := s.Pool.Query(r.Context(), `
			SELECT c.id, c.user_uid, u.username, COALESCE(u.avatar_url, ''), c.content, c.parent_id,
			       to_char(c.created_at, 'YYYY-MM-DD"T"HH24:MI:SSZ')
			FROM resource_comments c
			JOIN users u ON c.user_uid = u.bandbbs_uid
			WHERE c.resource_id = $1 AND c.state = 'visible' AND c.is_deleted = false
			ORDER BY c.created_at ASC
			LIMIT $2 OFFSET $3
		`, resID, size, offset)
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		defer rows.Close()

		var comments []CommentItem
		for rows.Next() {
			var it CommentItem
			if err := rows.Scan(&it.ID, &it.UserUID, &it.Username, &it.AvatarURL, &it.Content, &it.ParentID, &it.CreatedAt); err == nil {
				comments = append(comments, it)
			}
		}
		if comments == nil {
			comments = []CommentItem{}
		}

		var total int
		_ = s.Pool.QueryRow(r.Context(), `
			SELECT count(*) FROM resource_comments 
			WHERE resource_id = $1 AND state = 'visible' AND is_deleted = false
		`, resID).Scan(&total)

		httputil.JSON(w, http.StatusOK, map[string]any{
			"comments": comments,
			"total":    total,
			"page":     page,
			"size":     size,
		})
	}
}

type PostCommentRequest struct {
	Content  string `json:"content"`
	ParentID *int64 `json:"parent_id"`
}

// HandlePostComment implements POST /api/v1/resources/:id/comments
func HandlePostComment(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := httputil.ExtractBearerToken(r)
		userUID, err := httputil.AuthenticateUser(r.Context(), s, token)
		if err != nil {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未登录或Token失效")
			return
		}

		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) < 4 {
			httputil.Error(w, http.StatusBadRequest, "invalid_path", "路径错误")
			return
		}
		resID := parts[3]

		var req PostCommentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Content) == "" {
			httputil.Error(w, http.StatusBadRequest, "invalid_body", "评论内容不能为空")
			return
		}
		content := strings.TrimSpace(req.Content)
		if len([]rune(content)) > 5000 {
			httputil.Error(w, http.StatusBadRequest, "content_too_long", "评论内容不能超过5000字")
			return
		}
		var resourceVisible bool
		if err := s.Pool.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM resources WHERE resource_id = $1 AND moderation_state = 'visible' AND current_revision_id IS NOT NULL)`, resID).Scan(&resourceVisible); err != nil || !resourceVisible {
			httputil.Error(w, http.StatusNotFound, "resource_not_found", "资源不存在或不可评论")
			return
		}
		if req.ParentID != nil {
			var parentResource string
			if err := s.Pool.QueryRow(r.Context(), `SELECT resource_id FROM resource_comments WHERE id = $1 AND state = 'visible' AND is_deleted = false`, *req.ParentID).Scan(&parentResource); err != nil || parentResource != resID {
				httputil.Error(w, http.StatusBadRequest, "invalid_parent", "回复目标不属于当前资源")
				return
			}
		}
		allow, action, reason, model, reviewErr := moderation.Review(r.Context(), s, content)
		if reviewErr != nil {
			httputil.Error(w, http.StatusServiceUnavailable, "moderation_unavailable", "内容审核服务暂时不可用")
			return
		}
		state := "visible"
		status := "published"
		if !allow {
			state = "hidden"
			status = "rejected"
		}

		var commentID int64
		err = s.Pool.QueryRow(r.Context(), `
			INSERT INTO resource_comments (resource_id, user_uid, content, parent_id, state)
			VALUES ($1, $2, $3, $4, $5)
			RETURNING id
		`, resID, userUID, content, req.ParentID, state).Scan(&commentID)
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		_, _ = s.Pool.Exec(r.Context(), `UPDATE resource_comments SET ai_action = $2, ai_reason = $3, ai_model = $4 WHERE id = $1`, commentID, action, reason, model)
		if state == "visible" && req.ParentID != nil {
			var parentUID int
			if s.Pool.QueryRow(r.Context(), `SELECT user_uid FROM resource_comments WHERE id = $1`, *req.ParentID).Scan(&parentUID) == nil && parentUID != userUID {
				data, _ := json.Marshal(map[string]any{"comment_id": commentID, "resource_id": resID, "target_source": "comment", "target_id": commentID, "body": content})
				_, _ = s.Pool.Exec(r.Context(), `INSERT INTO user_messages (user_uid,kind,event,title,body,ref,data) VALUES ($1,'system','comment.replied','评论收到新回复',$2,$3,$4)`, parentUID, content, resID, data)
			}
		}
		if state == "visible" && req.ParentID == nil {
			var ownerUID int
			if s.Pool.QueryRow(r.Context(), `SELECT owner_uid FROM resources WHERE resource_id = $1`, resID).Scan(&ownerUID) == nil && ownerUID != userUID {
				data, _ := json.Marshal(map[string]any{"comment_id": commentID, "resource_id": resID, "target_source": "resource", "target_id": resID, "body": content})
				_, _ = s.Pool.Exec(r.Context(), `INSERT INTO user_messages (user_uid,kind,event,title,body,ref,data) VALUES ($1,'system','comment.created','资源收到新评论',$2,$3,$4)`, ownerUID, content, resID, data)
			}
		}
		if state == "hidden" {
			data, _ := json.Marshal(map[string]any{"automated": true, "reason": reason, "comment_id": commentID, "resource_id": resID})
			_, _ = s.Pool.Exec(r.Context(), `INSERT INTO user_messages (user_uid,kind,event,title,body,ref,data) VALUES ($1,'system','comment.hidden','评论未通过审核',$2,$3,$4)`, userUID, reason, resID, data)
		}

		httputil.JSON(w, http.StatusOK, map[string]any{
			"comment_id": commentID,
			"status":     status,
		})
	}
}

// HandleDeleteComment implements DELETE /api/v1/comments/:id for the comment
// author. Moderators use the separate admin comment-state endpoint.
func HandleDeleteComment(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := httputil.ExtractBearerToken(r)
		userUID, err := httputil.AuthenticateUser(r.Context(), s, token)
		if err != nil || userUID <= 0 {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未登录或Token失效")
			return
		}
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		commentID := int64(0)
		for i, part := range parts {
			if part == "comments" && i+1 < len(parts) {
				commentID = int64(httputil.ParseInt(parts[i+1], 0))
				break
			}
		}
		if commentID <= 0 {
			httputil.Error(w, http.StatusBadRequest, "invalid_id", "评论ID无效")
			return
		}
		result, err := s.Pool.Exec(r.Context(), `
			UPDATE resource_comments SET is_deleted = true, state = 'hidden'
			WHERE id = $1 AND user_uid = $2
		`, commentID, userUID)
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		if result.RowsAffected() == 0 {
			httputil.Error(w, http.StatusNotFound, "comment_not_found", "评论不存在或无权删除")
			return
		}
		httputil.JSON(w, http.StatusOK, map[string]any{"status": "deleted", "comment_id": commentID})
	}
}

// HandleCoinVote implements POST /api/v1/resources/:id/coins
// Spec §8.2: 打赏创作者；amount 校验为 10 的正整数倍；
// 90% 燃烧通缩（tip_burn），10% 归创作者收益（tip_receive），扣减打赏者（tip_send）
func HandleCoinVote(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := httputil.ExtractBearerToken(r)
		userUID, err := httputil.AuthenticateUser(r.Context(), s, token)
		if err != nil {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未登录或Token失效")
			return
		}

		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) < 4 {
			httputil.Error(w, http.StatusBadRequest, "invalid_path", "路径错误")
			return
		}
		resID := parts[3]

		var req struct {
			Amount int `json:"amount"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httputil.Error(w, http.StatusBadRequest, "invalid_body", "参数错误")
			return
		}

		if req.Amount < 10 || req.Amount%10 != 0 || req.Amount > 1_000_000 {
			httputil.Error(w, http.StatusBadRequest, "invalid_amount", "投币数量必须为 10 的正整数倍")
			return
		}

		err = s.WithTx(r.Context(), func(tx pgx.Tx) error {
			// 1. Check sender balance
			var senderBalance int
			if err := tx.QueryRow(r.Context(), `
				SELECT balance FROM user_coin_accounts WHERE user_uid = $1 FOR UPDATE
			`, userUID).Scan(&senderBalance); err != nil {
				return err
			}
			if senderBalance < req.Amount {
				return fmt.Errorf("insufficient_balance")
			}

			// 2. Query resource owner
			var ownerUID int
			var resTitle string
			if err := tx.QueryRow(r.Context(), `
				SELECT owner_uid, title FROM resources WHERE resource_id = $1
			`, resID).Scan(&ownerUID, &resTitle); err != nil {
				return err
			}

			// 3. Sender deduct (tip_send)
			senderAfter := senderBalance - req.Amount
			_, err = tx.Exec(r.Context(), `
				UPDATE user_coin_accounts SET balance = $1, updated_at = now() WHERE user_uid = $2
			`, senderAfter, userUID)
			if err != nil {
				return err
			}

			if _, err = tx.Exec(r.Context(), `
				INSERT INTO coin_ledger (user_uid, delta_units, kind, reference_type, reference_id, note, balance_after)
				VALUES ($1, $2, 'tip_send', 'resource', $3, '资源投币打赏', $4)
			`, userUID, -req.Amount, resID, senderAfter); err != nil {
				return err
			}

			// 4. Creator reward (10% = req.Amount / 10), 90% burned
			creatorReward := req.Amount / 10
			if creatorReward > 0 && ownerUID != userUID {
				var ownerBal int
				if err = tx.QueryRow(r.Context(), `
					SELECT balance FROM user_coin_accounts WHERE user_uid = $1 FOR UPDATE
				`, ownerUID).Scan(&ownerBal); err != nil && err != pgx.ErrNoRows {
					return err
				}

				ownerAfter := ownerBal + creatorReward
				if _, err = tx.Exec(r.Context(), `
					INSERT INTO user_coin_accounts (user_uid, balance, updated_at)
					VALUES ($1, $2, now())
					ON CONFLICT (user_uid) DO UPDATE SET balance = $2, updated_at = now()
				`, ownerUID, ownerAfter); err != nil {
					return err
				}

				if _, err = tx.Exec(r.Context(), `
					INSERT INTO coin_ledger (user_uid, delta_units, kind, reference_type, reference_id, note, balance_after)
					VALUES ($1, $2, 'tip_receive', 'resource', $3, '获得用户打赏分成', $4)
				`, ownerUID, creatorReward, resID, ownerAfter); err != nil {
					return err
				}

				// Notify creator
				msgBody := fmt.Sprintf("你的资源《%s》收到了来自用户的打赏，已获得 %d 厘硬币分成", resTitle, creatorReward)
				_, _ = tx.Exec(r.Context(), `
					INSERT INTO user_messages (user_uid, kind, event, title, body, ref, data)
					VALUES ($1, 'coin', 'coin.tip_received', '收到资源打赏分成', $2, $3, '{"kind":"tip_receive"}'::jsonb)
				`, ownerUID, msgBody, resID)
			}

			// 5. Update resource coin_count
			// coin_count is stored in the schema's 0.1-coin units, the same unit
			// used by coin_ledger.delta_units and the request amount.
			coinsToAdd := req.Amount
			_, err = tx.Exec(r.Context(), `
				UPDATE resources SET coin_count = coin_count + $1 WHERE resource_id = $2
			`, coinsToAdd, resID)
			if err != nil {
				return err
			}
			_, err = tx.Exec(r.Context(), `
			UPDATE resource_collections c
			SET coin_count = COALESCE((
				SELECT SUM(r.coin_count)
				FROM resource_collection_revisions cr
				CROSS JOIN LATERAL jsonb_to_recordset(CASE WHEN jsonb_typeof(cr.items) = 'array' THEN cr.items ELSE '[]'::jsonb END) AS item(resource_id text)
				JOIN resources r ON r.resource_id = item.resource_id
				WHERE cr.id = c.current_revision_id
			), 0), updated_at = now()
			WHERE c.current_revision_id IS NOT NULL
		`)
			if err != nil {
				return err
			}

			// 6. Record vote
			_, err = tx.Exec(r.Context(), `
				INSERT INTO resource_coin_votes (resource_id, user_uid, amount, updated_at)
				VALUES ($1, $2, $3, now())
				ON CONFLICT (resource_id, user_uid) DO UPDATE 
				SET amount = resource_coin_votes.amount + EXCLUDED.amount, updated_at = now()
			`, resID, userUID, req.Amount)
			return err
		})

		if err != nil {
			httputil.Error(w, http.StatusBadRequest, "vote_failed", err.Error())
			return
		}

		httputil.JSON(w, http.StatusOK, map[string]any{
			"status": "success",
			"amount": req.Amount,
		})
	}
}

// HandlePostRating implements POST /api/v1/resources/:id/ratings
func HandlePostRating(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := httputil.ExtractBearerToken(r)
		userUID, err := httputil.AuthenticateUser(r.Context(), s, token)
		if err != nil {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未登录或Token失效")
			return
		}

		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) < 4 {
			httputil.Error(w, http.StatusBadRequest, "invalid_path", "路径错误")
			return
		}
		resID := parts[3]

		var req struct {
			Rating int `json:"rating"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Rating < 1 || req.Rating > 5 {
			httputil.Error(w, http.StatusBadRequest, "invalid_rating", "评分必须在 1 到 5 之间")
			return
		}

		err = s.WithTx(r.Context(), func(tx pgx.Tx) error {
			// Upsert rating
			_, err := tx.Exec(r.Context(), `
				INSERT INTO resource_ratings (resource_id, user_uid, rating, updated_at)
				VALUES ($1, $2, $3, now())
				ON CONFLICT (resource_id, user_uid) DO UPDATE 
				SET rating = EXCLUDED.rating, updated_at = now()
			`, resID, userUID, req.Rating)
			if err != nil {
				return err
			}

			// Recalculate average rating & count
			var avgRating float64
			var count int
			_ = tx.QueryRow(r.Context(), `
				SELECT COALESCE(AVG(rating), 5.0), count(*) 
				FROM resource_ratings WHERE resource_id = $1
			`, resID).Scan(&avgRating, &count)

			_, err = tx.Exec(r.Context(), `
				UPDATE resources 
				SET rating = $1, rating_count = $2, updated_at = now()
				WHERE resource_id = $3
			`, avgRating, count, resID)
			return err
		})

		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}

		httputil.JSON(w, http.StatusOK, map[string]any{"status": "rated", "rating": req.Rating})
	}
}

// HandleGetCoinVote implements GET /api/v1/resources/:id/coins (Spec §8.1)
func HandleGetCoinVote(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := httputil.ExtractBearerToken(r)
		userUID, err := httputil.AuthenticateUser(r.Context(), s, token)
		if err != nil {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未登录或Token失效")
			return
		}
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) < 4 {
			httputil.Error(w, http.StatusBadRequest, "invalid_path", "路径错误")
			return
		}
		resID := parts[3]

		var amount int
		err = s.Pool.QueryRow(r.Context(), `
			SELECT amount FROM resource_coin_votes WHERE resource_id = $1 AND user_uid = $2
		`, resID, userUID).Scan(&amount)

		if err != nil || amount <= 0 {
			httputil.JSON(w, http.StatusOK, map[string]any{
				"voted":  false,
				"amount": 0,
			})
			return
		}

		httputil.JSON(w, http.StatusOK, map[string]any{
			"voted":  true,
			"amount": amount,
		})
	}
}
