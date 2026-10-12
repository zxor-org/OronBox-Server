package observability

import (
	"context"
	"errors"
	"testing"
)

func TestRequestIDContext(t *testing.T) {
	ctx := WithRequestID(context.Background(), "abc123")
	if got := RequestID(ctx); got != "abc123" {
		t.Fatalf("request id = %q", got)
	}
}

func TestSafeErrorRedactsSecrets(t *testing.T) {
	got := SafeError(errors.New("authorization=Bearer-secret api_key=sk-secret"))
	if got == "" || got == "authorization=Bearer-secret api_key=sk-secret" {
		t.Fatalf("secret was not redacted: %q", got)
	}
}
