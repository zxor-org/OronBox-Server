package creator

import (
	"context"
	"errors"
	"fmt"
	"math"
)

const (
	mediaMaxDimension       = 1500
	maximumPurchasePriceCNY = 9999999999.99
)

func validPurchasePriceCNY(value float64) bool {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0.01 || value > maximumPurchasePriceCNY {
		return false
	}
	scaled := value * 100
	tolerance := math.Max(1e-6, math.Abs(scaled)*1e-15)
	return math.Abs(scaled-math.Round(scaled)) <= tolerance
}

func publishesToAstroBox(publications []PublicationRequest) bool {
	for _, publication := range publications {
		if publication.Target == "astrobox" {
			return true
		}
	}
	return false
}

func (s *Service) Publish(ctx context.Context, ownerID, resourceID string, bundle []byte) (Workspace, error) {
	return s.saveBundle(ctx, ownerID, resourceID, bundle, true, nil)
}

func (s *Service) SaveDraft(ctx context.Context, ownerID, resourceID string, bundle []byte) (Workspace, error) {
	return s.saveBundle(ctx, ownerID, resourceID, bundle, false, nil)
}

// Guarded writes are used by the v1 interface; internal imports/tests may use
// the existing methods when they already serialize their own operation.
func (s *Service) SaveDraftAt(ctx context.Context, uid, rid string, bundle []byte, expected int64) (Workspace, error) {
	w, err := s.saveBundle(ctx, uid, rid, bundle, false, &expected)
	return w, editWriteError(err)
}

func (s *Service) PublishAt(ctx context.Context, uid, rid string, bundle []byte, expected int64) (Workspace, error) {
	w, err := s.saveBundle(ctx, uid, rid, bundle, true, &expected)
	return w, editWriteError(err)
}

func editWriteError(err error) error {
	var state interface{ SQLState() string }
	if errors.As(err, &state) && (state.SQLState() == "40001" || state.SQLState() == "40P01") {
		return fmt.Errorf("%w: revision_conflict", ErrConflict)
	}
	return err
}
