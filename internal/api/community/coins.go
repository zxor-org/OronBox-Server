package community

import (
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/zxor-org/OronBox-Server/internal/httputil"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

func calculateStreak(r *http.Request, s *store.Store, userUID int) (int, bool) {
	rows, err := s.Pool.Query(r.Context(), `
		SELECT to_char(checkin_date, 'YYYY-MM-DD')
		FROM daily_coin_checkins
		WHERE user_uid = $1
		ORDER BY checkin_date DESC
		LIMIT 100
	`, userUID)
	if err != nil {
		return 0, false
	}
	defer rows.Close()

	var dates []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err == nil {
			dates = append(dates, d)
		}
	}
	if len(dates) == 0 {
		return 0, false
	}

	today := time.Now().Format("2006-01-02")
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")

	streak := 0
	checkedToday := false
	idx := 0

	if dates[0] == today {
		checkedToday = true
		streak = 1
		idx = 1
	} else if dates[0] == yesterday {
		checkedToday = false
		streak = 1
		idx = 1
	} else {
		return 0, false
	}

	currDate, _ := time.Parse("2006-01-02", dates[idx-1])
	for ; idx < len(dates); idx++ {
		prevDate, err := time.Parse("2006-01-02", dates[idx])
		if err != nil {
			break
		}
		if currDate.Sub(prevDate).Hours() == 24 {
			streak++
			currDate = prevDate
		} else {
			break
		}
	}
	return streak, checkedToday
}

// HandleGetCoinAccount implements GET /api/v1/coins/account (Spec §10.1)
func HandleGetCoinAccount(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := httputil.ExtractBearerToken(r)
		userUID, err := httputil.AuthenticateUser(r.Context(), s, token)
		if err != nil {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未登录或Token失效")
			return
		}

		var balance int
		_ = s.Pool.QueryRow(r.Context(), `
			SELECT balance FROM user_coin_accounts WHERE user_uid = $1
		`, userUID).Scan(&balance)

		streak, checkedToday := calculateStreak(r, s, userUID)

		httputil.JSON(w, http.StatusOK, map[string]any{
			"balance":           balance,
			"can_checkin_today": !checkedToday,
			"streak_days":       streak,
		})
	}
}

// HandlePostCheckin implements POST /api/v1/coins/checkin (Spec §10.2)
func HandlePostCheckin(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := httputil.ExtractBearerToken(r)
		userUID, err := httputil.AuthenticateUser(r.Context(), s, token)
		if err != nil {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未登录或Token失效")
			return
		}

		today := time.Now().Format("2006-01-02")
		reward := 10 // 10 units = 1 coin
		var newBal int

		err = s.WithTx(r.Context(), func(tx pgx.Tx) error {
			// Check if already checked in today
			var exists bool
			_ = tx.QueryRow(r.Context(), `
				SELECT true FROM daily_coin_checkins 
				WHERE user_uid = $1 AND checkin_date = CURRENT_DATE
			`, userUID).Scan(&exists)
			if exists {
				return fmt.Errorf("already_checked_in")
			}

			// Record checkin
			if _, err := tx.Exec(r.Context(), `
				INSERT INTO daily_coin_checkins (user_uid, checkin_date, coins_awarded)
				VALUES ($1, CURRENT_DATE, $2)
			`, userUID, reward); err != nil {
				return err
			}

			// Update coin account
			var curBal int
			_ = tx.QueryRow(r.Context(), `
				SELECT balance FROM user_coin_accounts WHERE user_uid = $1 FOR UPDATE
			`, userUID).Scan(&curBal)

			newBal = curBal + reward
			if _, err := tx.Exec(r.Context(), `
				INSERT INTO user_coin_accounts (user_uid, balance, last_checkin_date, updated_at)
				VALUES ($1, $2, CURRENT_DATE, now())
				ON CONFLICT (user_uid) DO UPDATE 
				SET balance = $2, last_checkin_date = CURRENT_DATE, updated_at = now()
			`, userUID, newBal); err != nil {
				return err
			}

			// Record ledger
			if _, err := tx.Exec(r.Context(), `
				INSERT INTO coin_ledger (user_uid, delta_units, kind, reference_type, reference_id, note, balance_after)
				VALUES ($1, $2, 'checkin', 'daily', $3, '每日签到奖励', $4)
			`, userUID, reward, today, newBal); err != nil {
				return err
			}

			return nil
		})

		if err != nil {
			httputil.Error(w, http.StatusBadRequest, "checkin_failed", "今日已签到或签到失败")
			return
		}

		streak, _ := calculateStreak(r, s, userUID)

		httputil.JSON(w, http.StatusOK, map[string]any{
			"coins_awarded": reward,
			"streak_days":   streak,
			"balance":       newBal,
		})
	}
}

// HandleGetCoinLedger implements GET /api/v1/coins/ledger
func HandleGetCoinLedger(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := httputil.ExtractBearerToken(r)
		userUID, err := httputil.AuthenticateUser(r.Context(), s, token)
		if err != nil {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未登录或Token失效")
			return
		}

		page := httputil.ParseInt(r.URL.Query().Get("page"), 1)
		size := httputil.ParseInt(r.URL.Query().Get("size"), 30)
		offset := (page - 1) * size

		rows, err := s.Pool.Query(r.Context(), `
			SELECT id, delta_units, kind, COALESCE(reference_type, ''), COALESCE(reference_id, ''), 
			       note, balance_after, to_char(created_at, 'YYYY-MM-DD"T"HH24:MI:SSZ')
			FROM coin_ledger
			WHERE user_uid = $1
			ORDER BY created_at DESC
			LIMIT $2 OFFSET $3
		`, userUID, size, offset)
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		defer rows.Close()

		type LedgerItem struct {
			ID            int64  `json:"id"`
			DeltaUnits    int    `json:"delta_units"`
			Kind          string `json:"kind"`
			ReferenceType string `json:"reference_type"`
			ReferenceID   string `json:"reference_id"`
			Note          string `json:"note"`
			BalanceAfter  int    `json:"balance_after"`
			CreatedAt     string `json:"created_at"`
		}

		var list []LedgerItem
		for rows.Next() {
			var it LedgerItem
			if err := rows.Scan(&it.ID, &it.DeltaUnits, &it.Kind, &it.ReferenceType, &it.ReferenceID, &it.Note, &it.BalanceAfter, &it.CreatedAt); err == nil {
				list = append(list, it)
			}
		}
		if list == nil {
			list = []LedgerItem{}
		}

		httputil.JSON(w, http.StatusOK, map[string]any{"items": list})
	}
}
