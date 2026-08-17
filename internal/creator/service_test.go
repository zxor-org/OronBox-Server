package creator

import (
	"context"
	"errors"
	"math"
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

func TestValidPurchasePriceCNY(t *testing.T) {
	t.Parallel()
	for _, value := range []float64{0.01, 1, 100.25, maximumPurchasePriceCNY} {
		if !validPurchasePriceCNY(value) {
			t.Errorf("validPurchasePriceCNY(%v) = false, want true", value)
		}
	}
	for _, value := range []float64{0, 0.001, 1.001, maximumPurchasePriceCNY + 0.01, math.Inf(1), math.NaN()} {
		if validPurchasePriceCNY(value) {
			t.Errorf("validPurchasePriceCNY(%v) = true, want false", value)
		}
	}
}
