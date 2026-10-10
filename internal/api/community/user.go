package community

import (
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/zxor-org/OronBox-Server/internal/httputil"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

type UserMeResponse struct {
	BandBBSUID  int    `json:"bandbbs_uid"`
	Username    string `json:"username"`
	AvatarURL   string `json:"avatar_url"`
	Role        string `json:"role"`
	Banned      bool   `json:"banned"`
	CoinUnits   int    `json:"coin_units"`
	CoinCount   int    `json:"coin_count"`
	CoinBalance int    `json:"coin_balance"`
	GitHubBound bool   `json:"github_bound"`
	GitHubLogin string `json:"github_login"`
}

// HandleGetMe implements GET /api/v1/users/me (Spec §6.1)
func HandleGetMe(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := httputil.ExtractBearerToken(r)
		userUID, err := httputil.AuthenticateUser(r.Context(), s, token)
		if err != nil {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未登录或Token失效")
			return
		}

		var res UserMeResponse
		err = s.Pool.QueryRow(r.Context(), `
			SELECT bandbbs_uid, username, COALESCE(avatar_url, ''), role, banned
			FROM users WHERE bandbbs_uid = $1
		`, userUID).Scan(&res.BandBBSUID, &res.Username, &res.AvatarURL, &res.Role, &res.Banned)
		if err == pgx.ErrNoRows {
			httputil.Error(w, http.StatusNotFound, "user_not_found", "用户不存在")
			return
		} else if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}

		var balance int
		_ = s.Pool.QueryRow(r.Context(), `
			SELECT balance FROM user_coin_accounts WHERE user_uid = $1
		`, userUID).Scan(&balance)
		res.CoinUnits = balance
		res.CoinCount = balance / 10
		res.CoinBalance = balance

		var ghLogin string
		_ = s.Pool.QueryRow(r.Context(), `
			SELECT login FROM github_grants WHERE user_uid = $1
		`, userUID).Scan(&ghLogin)
		res.GitHubBound = ghLogin != ""
		res.GitHubLogin = ghLogin

		httputil.JSON(w, http.StatusOK, res)
	}
}
