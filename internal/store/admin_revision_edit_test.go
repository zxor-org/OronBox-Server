package store

import (
	"encoding/json"
	"testing"
)

func TestAdminRevisionDraftInputNormalization(t *testing.T) {
	t.Parallel()
	input, err := (AdminRevisionDraftInput{
		Name: "  Resource  ", Summary: " summary ", PaidType: "paid",
		Attributes:      []string{"original", " original ", ""},
		Links:           []AdminLink{{Title: " Source ", URL: " https://example.com/resource "}},
		PublicationPlan: json.RawMessage(`[{"target":"oronbox","config":{}}]`),
	}).normalized()
	if err != nil {
		t.Fatalf("normalize draft: %v", err)
	}
	if input.Name != "Resource" || input.Summary != "summary" || len(input.Attributes) != 1 || input.Links[0].Position != 0 {
		t.Fatalf("normalized draft = %#v", input)
	}
}

func TestAdminRevisionDraftInputRejectsInvalidPublicationTargets(t *testing.T) {
	t.Parallel()
	_, err := (AdminRevisionDraftInput{Name: "Resource", PublicationPlan: json.RawMessage(`[{"target":"unknown","config":{}}]`)}).normalized()
	if err == nil {
		t.Fatal("invalid publication target was accepted")
	}
}
