package admin

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/httpx"
)

func writeList(w http.ResponseWriter, items any, total, page, perPage int, err error, code string) {
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody(code, err.Error()))
		return
	}
	totalPages := 0
	if perPage > 0 && total > 0 {
		totalPages = (total + perPage - 1) / perPage
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "page": page, "per_page": perPage, "total_pages": totalPages})
}

func TimeRange(query url.Values) (*time.Time, *time.Time) {
	parse := func(value string, endOfDay bool) *time.Time {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil
		}
		if parsed, err := time.Parse(time.RFC3339, value); err == nil {
			return &parsed
		}
		parsed, err := time.ParseInLocation("2006-01-02", value, time.Local)
		if err != nil {
			return nil
		}
		if endOfDay {
			parsed = parsed.Add(24*time.Hour - time.Nanosecond)
		}
		return &parsed
	}
	return parse(query.Get("from"), false), parse(query.Get("to"), true)
}
