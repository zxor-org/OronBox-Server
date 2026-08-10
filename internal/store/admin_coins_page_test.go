package store

import (
	"testing"
	"time"
)

func TestAdminCoinQueryNormalized(t *testing.T) {
	from := time.Now()
	to := from.Add(-time.Hour)
	q := (AdminCoinQuery{Search: " x ", User: " u ", Kind: " k ", ReferenceType: " r ", Sort: "bad", From: &from, To: &to, Page: -1, PerPage: 999}).normalized()
	if q.Search != "x" || q.User != "u" || q.Kind != "k" || q.ReferenceType != "r" {
		t.Fatalf("filters = %#v", q)
	}
	if q.Sort != "newest" || q.Page != 1 || q.PerPage != 100 {
		t.Fatalf("pagination = %#v", q)
	}
	if q.From == nil || q.To == nil || !q.From.Equal(to) || !q.To.Equal(from) {
		t.Fatalf("range not normalized")
	}
}

func TestAdminCoinOrder(t *testing.T) {
	if got := adminCoinOrder("delta_desc"); got != "l.delta_units DESC,l.created_at DESC" {
		t.Fatalf("order = %q", got)
	}
	if got := adminCoinOrder("bad"); got != "l.created_at DESC,l.id DESC" {
		t.Fatalf("default = %q", got)
	}
}
