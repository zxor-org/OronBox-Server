package moderation

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestReviewParsesJSONVerdict(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"action\":\"pass\",\"categories\":[],\"reason\":\"ok\"}"}}]}`))
	}))
	defer server.Close()
	service := New(Endpoint{Name: "primary", BaseURL: server.URL, APIKey: "key", Model: "model"}, Endpoint{}, time.Second)
	verdict, err := service.Review(context.Background(), "prompt", "text")
	if err != nil {
		t.Fatal(err)
	}
	if verdict.Action != "pass" || verdict.Provider != "primary" || verdict.Model != "model" {
		t.Fatalf("unexpected verdict: %#v", verdict)
	}
}

func TestReviewFallsBack(t *testing.T) {
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "down", http.StatusBadGateway) }))
	defer primary.Close()
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"action\":\"review\",\"categories\":[\"spam\"],\"reason\":\"unclear\"}"}}]}`))
	}))
	defer fallback.Close()
	service := New(Endpoint{Name: "primary", BaseURL: primary.URL, APIKey: "a", Model: "a"}, Endpoint{Name: "fallback", BaseURL: fallback.URL, APIKey: "b", Model: "b"}, time.Second)
	verdict, err := service.Review(context.Background(), "prompt", "text")
	if err != nil {
		t.Fatal(err)
	}
	if verdict.Provider != "fallback" || verdict.Action != "review" {
		t.Fatalf("unexpected verdict: %#v", verdict)
	}
}

func TestReviewFailsClosedWhenProvidersFail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "down", http.StatusBadGateway) }))
	defer server.Close()
	service := New(Endpoint{Name: "primary", BaseURL: server.URL, APIKey: "a", Model: "a"}, Endpoint{}, time.Second)
	_, err := service.Review(context.Background(), "prompt", "text")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}
}
