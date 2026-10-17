package resources

import (
	"errors"
	"time"
)

var (
	ErrAdminResourceNotFound = errors.New("resource not found")
	ErrAdminResourceConflict = errors.New("resource action is not allowed")
)

type AdminResourceQuery struct {
	ResourceID           string
	Device               string
	Search               string
	Owner                string
	Kind                 string
	Moderation           string
	RevisionState        string
	CurrentRevisionState string
	ReviewState          string
	PublicationTarget    string
	PublicationState     string
	Sort                 string
	Page                 int
	PerPage              int
}

type AdminPublication struct {
	ID            string         `json:"id"`
	Target        string         `json:"target"`
	State         string         `json:"state"`
	Config        map[string]any `json:"config"`
	StatusDetail  map[string]any `json:"status_detail"`
	ExternalID    string         `json:"external_id"`
	ExternalURL   string         `json:"external_url"`
	ErrorMessage  string         `json:"error_message"`
	Attempts      int            `json:"attempts"`
	NextAttemptAt time.Time      `json:"next_attempt_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

type AdminResourceItem struct {
	ID                    string
	OwnerID               string
	Owner                 string
	Slug                  string
	Platform              string
	Kind                  string
	ModerationState       string
	ModerationBy          string
	ModerationReason      string
	ModerationAt          *time.Time
	CurrentRevisionID     string
	CurrentRevisionNumber int
	CurrentRevisionName   string
	CurrentRevisionState  string
	LatestRevisionID      string
	LatestRevisionNumber  int
	LatestRevisionName    string
	LatestRevisionState   string
	LatestReviewState     string
	Publications          []AdminPublication
	Name                  string
	RevisionNo            int
	RevisionState         string
	ReviewState           string
	Targets               []string
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

type AdminResourcePage struct {
	Items      []AdminResourceItem
	Total      int
	Page       int
	PerPage    int
	TotalPages int
	Query      AdminResourceQuery
}

type AdminRevision struct {
	Description       string
	DescriptionFormat string
	ID                string
	Number            int
	Name              string
	Summary           string
	PaidType          string
	Attributes        []string
	PublicationPlan   []byte
	State             string
	CreatedBy         string
	CreatedVia        string
	BaseRevisionID    string
	ReviewID          string
	ReviewState       string
	ReviewNote        string
	ReviewItems       []string
	Reviewer          string
	ArtifactCount     int
	MediaCount        int
	Publications      []AdminPublication
	CreatedAt         time.Time
	ReviewUpdatedAt   *time.Time
}

type AdminExternalBinding struct {
	Provider    string
	ExternalID  string
	ExternalURL string
	Repository  string
	Entries     []AdminExternalBindingEntry
	CreatedAt   time.Time
}

type AdminExternalBindingEntry struct {
	Label string
	Value string
	URL   string
}

type AdminResourceEvent struct {
	ID        int64
	Actor     string
	EventType string
	Payload   map[string]any
	CreatedAt time.Time
}

type AdminResourceDetail struct {
	Resource     AdminResourceItem
	Revisions    []AdminRevision
	Publications []AdminPublication
	Artifacts    []AdminArtifact
	Media        []AdminMedia
	Bindings     []AdminExternalBinding
	Events       []AdminResourceEvent
	Snapshot     string
}

type AdminLink struct {
	Position int
	Title    string
	URL      string
}

type AdminRevisionDetail struct {
	Resource  AdminResourceItem
	Revision  AdminRevision
	Artifacts []AdminArtifact
	Media     []AdminMedia
	Links     []AdminLink
}

type AdminArtifact struct {
	ID             string
	SHA256         string
	OriginalName   string
	PackageFormat  string
	PackageID      string
	Version        string
	SizeBytes      int64
	Analysis       map[string]any
	Devices        []string
	DeviceBindings []AdminArtifactDevice
	DuplicateCount int
}

type AdminArtifactDevice struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Codename    string `json:"codename"`
	Platform    string `json:"platform"`
}

type AdminMedia struct {
	ID        string
	SHA256    string
	Role      string
	Position  int
	Width     int
	Height    int
	SizeBytes int64
}

type AdminResourceActionResult struct {
	ID                 string
	Slug               string
	OwnerID            string
	PreviousModeration string
	ModerationState    string
	Reason             string
	Deleted            bool
}
