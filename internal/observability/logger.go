package observability

import (
	"context"
	"log/slog"
	"os"
	"regexp"
	"strings"
)

type contextKey string

const requestIDKey contextKey = "request_id"

func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

func RequestID(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}

var sensitiveValueRE = regexp.MustCompile(`(?i)(authorization|cookie|token|secret|api[_-]?key)=?[^\s,;]+`)
var bearerRE = regexp.MustCompile(`(?i)Bearer\s+[^\s,;]+`)

func SafeError(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.TrimSpace(err.Error())
	msg = sensitiveValueRE.ReplaceAllString(msg, "$1=[REDACTED]")
	msg = bearerRE.ReplaceAllString(msg, "Bearer [REDACTED]")
	if len(msg) > 500 {
		msg = msg[:500]
	}
	return msg
}

func NewLogger(level, format string) *slog.Logger {
	var l slog.Level
	switch strings.ToLower(level) {
	case "debug":
		l = slog.LevelDebug
	case "warn":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		l = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: l}
	var h slog.Handler
	if strings.EqualFold(format, "json") {
		h = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		h = slog.NewTextHandler(os.Stdout, opts)
	}
	return slog.New(h)
}
