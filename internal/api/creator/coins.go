package creator

import (
	"github.com/zxor-org/OronBox-Server/internal/httputil"
	"github.com/zxor-org/OronBox-Server/internal/store"
	"net/http"
)

// HandleCreatorCoinStats is a compatibility read model for older creator UI.
// Values remain in the server's smallest unit (10 units = 1 displayed coin).
func HandleCreatorCoinStats(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid, err := httputil.AuthenticateUser(r.Context(), s, httputil.ExtractBearerToken(r))
		if err != nil {
			httputil.Error(w, 401, "unauthorized", "未登录或Token失效")
			return
		}
		var balance int
		_ = s.Pool.QueryRow(r.Context(), `SELECT COALESCE(balance,0) FROM user_coin_accounts WHERE user_uid=$1`, uid).Scan(&balance)
		var sent, received int
		_ = s.Pool.QueryRow(r.Context(), `SELECT COALESCE(SUM(CASE WHEN delta_units<0 THEN -delta_units ELSE 0 END),0),COALESCE(SUM(CASE WHEN delta_units>0 THEN delta_units ELSE 0 END),0) FROM coin_ledger WHERE user_uid=$1`, uid).Scan(&sent, &received)
		httputil.JSON(w, 200, map[string]any{"balance_units": balance, "balance": float64(balance) / 10, "sent_units": sent, "received_units": received})
	}
}
