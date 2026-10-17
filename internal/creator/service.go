package creator

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"regexp"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/blob"
	"github.com/zxor-org/OronBox-Server/internal/observability"
)

var (
	ErrNotFound = errors.New("creator resource not found")
	ErrConflict = errors.New("creator resource changed")
	ErrInvalid  = errors.New("invalid creator resource")
)

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{1,63}$`)

// maxDraftResources caps how many never-submitted resources one owner may
// keep; submitted resources (approved or rejected) stop counting.
const maxDraftResources = 10

type Limits struct {
	UploadMaxBytes  int64
	PreviewMaxBytes int64
	PreviewMaxCount int
}

type Service struct {
	publicationCatalog        publicationCatalogState
	RequirePublicationCatalog bool
	VerifyImportedBinding     func(context.Context, string, ImportedBinding) (ImportedBinding, error)
	db                        *sql.DB
	blobs                     blob.Store
	limits                    Limits
	now                       func() time.Time
	// Ranking tunes the recommendation score multipliers. A zero value keeps
	// the built-in defaults.
	Ranking Ranking
	// BandBBSDelete synchronously deletes the given BandBBS resource ids on
	// behalf of ownerID. Delete aborts when it reports an error.
	BandBBSDelete func(ctx context.Context, ownerID string, resourceIDs []string) error
	// AstroBoxRemove submits an AstroBox catalog removal PR for the given item
	// id on behalf of ownerID and returns the PR URL, or an empty string when
	// the item was not listed. Delete aborts when it reports an error.
	AstroBoxRemove        func(ctx context.Context, ownerID, itemID, name string) (string, error)
	AstroBoxRemovalStatus func(ctx context.Context, ownerID, prURL string) (string, error)
}

func New(db *sql.DB, blobs blob.Store, limits Limits) *Service {
	return &Service{db: db, blobs: blobs, limits: limits, now: func() time.Time { return time.Now().UTC() }}
}

// log keeps request-scoped fields (request_id, path) but files the event
// under the creator component.
func log(ctx context.Context) *slog.Logger {
	return observability.From(ctx).With("component", "creator")
}
