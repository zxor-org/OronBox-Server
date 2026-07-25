package resource

import "time"

type Platform string
type Kind string

const (
	VelaOS Platform = "vela_os"
	ZeppOS Platform = "zepp_os"

	QuickApp  Kind = "quickapp"
	Watchface Kind = "watchface"
	ZeppApp   Kind = "zepp_app"
	Firmware  Kind = "firmware"
)

type Resource struct {
	ID                string    `json:"id"`
	OwnerID           string    `json:"owner_id"`
	Slug              string    `json:"slug"`
	CurrentRevisionID string    `json:"current_revision_id,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type Revision struct {
	ID          string    `json:"id"`
	ResourceID  string    `json:"resource_id"`
	RevisionNo  int       `json:"revision_no"`
	Name        string    `json:"name"`
	Summary     string    `json:"summary"`
	Platform    Platform  `json:"platform"`
	Kind        Kind      `json:"kind"`
	ReviewState string    `json:"review_state"`
	CreatedAt   time.Time `json:"created_at"`
}

func ValidProfile(platform Platform, kind Kind) bool {
	switch platform {
	case VelaOS:
		return kind == QuickApp || kind == Watchface
	case ZeppOS:
		return kind == ZeppApp || kind == Watchface
	default:
		return false
	}
}

type Artifact struct {
	ID            string         `json:"id"`
	RevisionID    string         `json:"revision_id"`
	BlobSHA256    string         `json:"sha256"`
	OriginalName  string         `json:"original_name"`
	Platform      Platform       `json:"platform"`
	Kind          Kind           `json:"kind"`
	PackageFormat string         `json:"package_format"`
	PackageID     string         `json:"package_id"`
	Version       string         `json:"version"`
	Analysis      map[string]any `json:"analysis"`
}

type PublicationRequest struct {
	Target string         `json:"target"`
	Config map[string]any `json:"config"`
}

type SubmitRequest struct {
	Publications []PublicationRequest `json:"publications"`
}
