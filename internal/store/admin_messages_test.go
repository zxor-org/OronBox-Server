package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAdminMessageQueryNormalized(t *testing.T) {
	from := time.Now().Add(time.Hour)
	to := time.Now()
	got := (AdminMessageQuery{Search: " hello ", User: " alice ", Kind: "bad", Read: "bad", From: &from, To: &to, Page: -1, PerPage: 1000}).normalized()
	if got.Search != "hello" || got.User != "alice" || got.Kind != "" || got.Read != "" || got.Page != 1 || got.PerPage != 100 || got.From != nil || got.To != nil {
		t.Fatalf("normalized query = %#v", got)
	}
	valid := (AdminMessageQuery{Kind: "admin_message", Read: "unread", Page: 2, PerPage: 50}).normalized()
	if valid.Kind != "admin_message" || valid.Read != "unread" || valid.Page != 2 || valid.PerPage != 50 {
		t.Fatalf("valid query = %#v", valid)
	}
}

func TestAdminMessageRejectsInvalidID(t *testing.T) {
	_, err := (&Store{}).AdminMessage(context.Background(), "invalid")
	if !errors.Is(err, ErrAdminMessageNotFound) {
		t.Fatalf("AdminMessage error = %v, want %v", err, ErrAdminMessageNotFound)
	}
}
