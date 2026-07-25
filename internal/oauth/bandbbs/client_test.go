package bandbbs

import (
	"strings"
	"testing"
)

func TestUpstreamErrorDoesNotExposeCredentialFields(t *testing.T) {
	err := upstreamError("token", 400, []byte(`{"error":"invalid_grant","error_description":"expired","access_token":"secret"}`))
	message := err.Error()
	if !strings.Contains(message, "invalid_grant") || strings.Contains(message, "secret") {
		t.Fatalf("unsafe error = %q", message)
	}
}
