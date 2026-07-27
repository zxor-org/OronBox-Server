package creator

import (
	"context"
	"errors"
	"testing"
)

func TestCreateRequiresDisplayName(t *testing.T) {
	service := New(nil, nil, Limits{})
	for _, name := range []string{"  ", "line one\nline two"} {
		_, err := service.Create(context.Background(), "owner", "draft-resource", name, QuickApp)
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("Create(name=%q) error = %v, want ErrInvalid", name, err)
		}
	}
}
