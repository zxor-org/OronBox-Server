package observability

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

type contextKey struct{}

// Configure installs the process-wide structured logger. The text format is a
// fixed-column, AstroBox-style line readable in systemd and a terminal; JSON
// is available for log collectors through LOG_FORMAT=json.
func Configure(levelName, format string) *slog.Logger {
	level := new(slog.LevelVar)
	switch strings.ToLower(strings.TrimSpace(levelName)) {
	case "debug":
		level.Set(slog.LevelDebug)
	case "warn", "warning":
		level.Set(slog.LevelWarn)
	case "error":
		level.Set(slog.LevelError)
	default:
		level.Set(slog.LevelInfo)
	}
	options := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if strings.EqualFold(strings.TrimSpace(format), "json") {
		handler = slog.NewJSONHandler(os.Stdout, options)
	} else {
		handler = newTextHandler(os.Stdout, level)
	}
	logger := slog.New(handler)
	slog.SetDefault(logger)
	return logger
}

// For returns the default logger tagged with a component name.
func For(component string) *slog.Logger {
	return slog.Default().With("component", component)
}

func From(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(contextKey{}).(*slog.Logger); ok {
		return logger
	}
	return slog.Default()
}

func With(ctx context.Context, attrs ...any) context.Context {
	return context.WithValue(ctx, contextKey{}, From(ctx).With(attrs...))
}

const (
	componentWidth = 20
	valueMaxLength = 300
)

// textHandler renders one line per record:
//
//	2026-07-24 08:34:10.858 INFO  [component] message key=value
type textHandler struct {
	level     *slog.LevelVar
	mu        *sync.Mutex
	out       io.Writer
	component string
	attrs     string
}

func newTextHandler(out io.Writer, level *slog.LevelVar) *textHandler {
	return &textHandler{level: level, mu: new(sync.Mutex), out: out}
}

func (h *textHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level.Level()
}

func (h *textHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := *h
	for _, attr := range attrs {
		next.component, next.attrs = appendAttr(next.component, next.attrs, attr)
	}
	return &next
}

// WithGroup is unsupported: attribute groups are flattened into key=value.
func (h *textHandler) WithGroup(string) slog.Handler { return h }

func (h *textHandler) Handle(_ context.Context, record slog.Record) error {
	component, attrs := h.component, h.attrs
	record.Attrs(func(attr slog.Attr) bool {
		component, attrs = appendAttr(component, attrs, attr)
		return true
	})
	if component == "" {
		component = "-"
	}
	component = truncate(component, componentWidth-1)
	line := fmt.Sprintf("%s %-5s [%s] %s%s\n",
		record.Time.Local().Format("2006-01-02 15:04:05.000"),
		levelName(record.Level), component, record.Message, attrs)
	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := io.WriteString(h.out, line)
	return err
}

func levelName(level slog.Level) string {
	switch {
	case level >= slog.LevelError:
		return "ERROR"
	case level >= slog.LevelWarn:
		return "WARN"
	case level >= slog.LevelInfo:
		return "INFO"
	default:
		return "DEBUG"
	}
}

// appendAttr folds the "component" attribute into the column and renders
// everything else as a key=value pair. Multi-line values (stack traces) are
// emitted on their own indented lines.
func appendAttr(component, attrs string, attr slog.Attr) (string, string) {
	value := attr.Value.Resolve()
	if attr.Equal(slog.Attr{}) {
		return component, attrs
	}
	if attr.Key == "component" && value.Kind() == slog.KindString {
		return value.String(), attrs
	}
	text := formatValue(value)
	if strings.Contains(text, "\n") {
		return component, attrs + "\n    " + attr.Key + ":\n    " + strings.ReplaceAll(text, "\n", "\n    ")
	}
	return component, attrs + " " + attr.Key + "=" + quote(text)
}

func formatValue(value slog.Value) string {
	var text string
	switch value.Kind() {
	case slog.KindString:
		text = value.String()
	case slog.KindDuration:
		return value.Duration().String()
	case slog.KindAny:
		if err, ok := value.Any().(error); ok {
			text = err.Error()
		} else {
			text = fmt.Sprint(value.Any())
		}
	default:
		return value.String()
	}
	return truncate(text, valueMaxLength)
}

func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}

func quote(value string) string {
	if value == "" {
		return `""`
	}
	if strings.IndexFunc(value, func(r rune) bool {
		return unicode.IsSpace(r) || !unicode.IsPrint(r) || r == '"' || r == '\''
	}) >= 0 {
		return strconv.Quote(value)
	}
	return value
}

type responseRecorder struct {
	http.ResponseWriter
	status int
	bytes  int64
	body   []byte
}

func (w *responseRecorder) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseRecorder) Write(payload []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	if w.status >= 400 && len(w.body) < 4096 {
		remaining := 4096 - len(w.body)
		if len(payload) < remaining {
			remaining = len(payload)
		}
		w.body = append(w.body, payload[:remaining]...)
	}
	n, err := w.ResponseWriter.Write(payload)
	w.bytes += int64(n)
	return n, err
}

func (w *responseRecorder) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *responseRecorder) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// HTTP records one completion event per request. It intentionally logs paths,
// not raw URLs, so OAuth codes and other query credentials never reach logs.
func HTTP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		requestID := cleanRequestID(r.Header.Get("X-Request-ID"))
		if requestID == "" {
			requestID = newRequestID()
		}
		w.Header().Set("X-Request-ID", requestID)
		logger := For("http").With(
			"request_id", requestID,
			"method", r.Method,
			"path", r.URL.Path,
		)
		if value := strings.TrimSpace(r.Header.Get("X-OronBox-App-Id")); value != "" {
			logger = logger.With("app_id", value)
		}
		if value := strings.TrimSpace(r.Header.Get("X-OronBox-Version")); value != "" {
			logger = logger.With("app_version", value)
		}
		if value := strings.TrimSpace(r.Header.Get("X-OronBox-Platform")); value != "" {
			logger = logger.With("platform", value)
		}
		r = r.WithContext(context.WithValue(r.Context(), contextKey{}, logger))
		recorder := &responseRecorder{ResponseWriter: w}
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.Error("http handler panic", "error", recovered, "stack", string(debug.Stack()))
				if recorder.status == 0 {
					http.Error(recorder, "internal server error", http.StatusInternalServerError)
				}
			}
			logHTTPCompletion(logger, r, recorder, started)
		}()
		next.ServeHTTP(recorder, r)
	})
}

func logHTTPCompletion(logger *slog.Logger, r *http.Request, recorder *responseRecorder, started time.Time) {
	if recorder.status == 0 {
		recorder.status = http.StatusOK
	}
	attrs := []any{
		"status", recorder.status,
		"duration_ms", time.Since(started).Milliseconds(),
	}
	if recorder.status >= 400 && len(recorder.body) != 0 {
		attrs = append(attrs, "error", errorSummary(recorder.body))
	}
	switch {
	case recorder.status >= 500:
		logger.Error("http request failed", attrs...)
	case recorder.status >= 400:
		logger.Warn("http request rejected", attrs...)
	default:
		logger.Debug("http request completed", attrs...)
	}
}

func cleanRequestID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 80 {
		return ""
	}
	for _, r := range value {
		if !(r == '-' || r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return ""
		}
	}
	return value
}

func newRequestID() string {
	var value [8]byte
	if _, err := io.ReadFull(rand.Reader, value[:]); err == nil {
		return hex.EncodeToString(value[:])
	}
	return time.Now().UTC().Format("20060102T150405.000000000")
}

func errorSummary(payload []byte) string {
	var body map[string]any
	if json.Unmarshal(payload, &body) == nil {
		code, _ := body["error"].(string)
		message, _ := body["message"].(string)
		if nested, ok := body["error"].(map[string]any); ok {
			code, _ = nested["code"].(string)
			message, _ = nested["message"].(string)
		}
		if code != "" || message != "" {
			return strings.TrimSpace(code + ": " + message)
		}
	}
	return "non-JSON error response"
}
