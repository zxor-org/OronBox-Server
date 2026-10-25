package httpx

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/zxor-org/OronBox-Server/internal/observability"
)

func WriteJSON(w http.ResponseWriter, status int, value any) {
	if status >= http.StatusInternalServerError {
		if body, ok := value.(map[string]string); ok {
			code := body["error"]
			message := body["message"]
			observability.For("http").Error(
				"request handler failed",
				"request_id", w.Header().Get("X-Request-ID"),
				"code", code,
				"status", status,
				"error", message,
			)
			value = ErrorBody(code, "The server could not complete the request")
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func ErrorBody(code, message string) map[string]string {
	return map[string]string{"error": code, "message": message}
}

func DecodeJSON(r *http.Request, destination any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}

func PositiveInt(raw string, fallback int) int {
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		return fallback
	}
	return value
}

func WantsJSON(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "application/json") || r.URL.Query().Get("format") == "json"
}

func EditVersion(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := strings.Trim(r.Header.Get("If-Match"), "\"")
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v < 0 {
		WriteJSON(w, http.StatusPreconditionRequired, ErrorBody("edit_version_required", "If-Match must contain the workspace edit_version"))
		return 0, false
	}
	return v, true
}

func ReadJSON(r *http.Request, dest any) error {
	return json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(dest)
}
