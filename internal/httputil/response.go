package httputil

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Detail  any    `json:"detail"`
}

func JSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func Error(w http.ResponseWriter, status int, code, message string) {
	JSON(w, status, APIError{
		Code:    code,
		Message: message,
		Detail:  map[string]any{},
	})
}

func ErrorWithDetail(w http.ResponseWriter, status int, code, message string, detail any) {
	if detail == nil {
		detail = map[string]any{}
	}
	JSON(w, status, APIError{
		Code:    code,
		Message: message,
		Detail:  detail,
	})
}

func ExtractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	if cookie, err := r.Cookie("oronbox_token"); err == nil && cookie.Value != "" {
		return cookie.Value
	}
	return ""
}

func ParseInt(str string, fallback int) int {
	if str == "" {
		return fallback
	}
	val, err := strconv.Atoi(str)
	if err != nil {
		return fallback
	}
	return val
}
