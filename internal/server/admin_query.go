package server

import (
	"net/url"
	"strings"
	"time"
)

func adminTimeRange(query url.Values) (*time.Time, *time.Time) {
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
