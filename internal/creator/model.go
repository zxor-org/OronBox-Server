package creator

import (
	"fmt"
	"strings"
	"time"
)

type ResourceKind string

const (
	QuickApp  ResourceKind = "quickapp"
	Watchface ResourceKind = "watchface"
)

func (kind ResourceKind) Valid() bool {
	return kind == QuickApp || kind == Watchface
}

type ResourcePaidType string

const (
	ResourcePaidFree  ResourcePaidType = "free"
	ResourcePaid      ResourcePaidType = "paid"
	ResourceForcePaid ResourcePaidType = "force_paid"
)

func (paidType ResourcePaidType) Valid() bool {
	return paidType == ResourcePaidFree || paidType == ResourcePaid || paidType == ResourceForcePaid
}

type RevisionState string

const (
	RevisionDraft      RevisionState = "draft"
	RevisionSubmitted  RevisionState = "submitted"
	RevisionApproved   RevisionState = "approved"
	RevisionRejected   RevisionState = "rejected"
	RevisionSuperseded RevisionState = "superseded"
)

type ReviewState string

const (
	ReviewPending    ReviewState = "pending"
	ReviewApproved   ReviewState = "approved"
	ReviewRejected   ReviewState = "rejected"
	ReviewSuperseded ReviewState = "superseded"
)

type MediaRole string

const (
	MediaPreview MediaRole = "preview"
	MediaIcon    MediaRole = "icon"
	MediaCover   MediaRole = "cover"
)

type PublicationTarget string

const (
	PublishOronBox  PublicationTarget = "oronbox"
	PublishBandBBS  PublicationTarget = "bandbbs"
	PublishAstroBox PublicationTarget = "astrobox"
)

type PublicationState string

const (
	PublicationPending   PublicationState = "pending"
	PublicationRunning   PublicationState = "running"
	PublicationPublished PublicationState = "published"
	PublicationReviewing PublicationState = "reviewing"
	PublicationFailed    PublicationState = "failed"
	PublicationCancelled PublicationState = "cancelled"
)

type Resource struct {
	ID                 string       `json:"id"`
	OwnerID            string       `json:"owner_id"`
	Slug               string       `json:"slug"`
	DraftName          string       `json:"draft_name"`
	Kind               ResourceKind `json:"kind"`
	CurationGrade      string       `json:"curation_grade"`
	CollectionID       string       `json:"collection_id,omitempty"`
	CollectionPosition int          `json:"collection_position"`
	ModerationState    string       `json:"moderation_state"`
	ModerationBy       string       `json:"moderation_by,omitempty"`
	ModerationReason   string       `json:"moderation_reason,omitempty"`
	ModerationAt       *time.Time   `json:"moderation_at,omitempty"`
	DownloadCount      int          `json:"download_count"`
	CurrentRevisionID  string       `json:"current_revision_id,omitempty"`
	CreatedAt          time.Time    `json:"created_at"`
	UpdatedAt          time.Time    `json:"updated_at"`
}

type Revision struct {
	ID               string           `json:"id"`
	ResourceID       string           `json:"resource_id"`
	Number           int              `json:"number"`
	Name             string           `json:"name"`
	Summary          string           `json:"summary"`
	PaidType         ResourcePaidType `json:"paid_type"`
	PurchaseLink     string           `json:"purchase_link,omitempty"`
	PurchasePrice    *float64         `json:"purchase_price,omitempty"`
	PurchaseCurrency string           `json:"purchase_currency,omitempty"`
	Attributes       []string         `json:"attributes,omitempty"`
	State            RevisionState    `json:"state"`
	// PublicationPlan is the saved publish intent (target+config list); it is
	// data for the editor, never a dispatchable job.
	PublicationPlan []PublicationRequest `json:"publication_plan"`
	CreatedAt       time.Time            `json:"created_at"`
}

type ResourceLink struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

type ResourceAttribute struct {
	ID          string  `json:"id"`
	NameZH      string  `json:"name_zh"`
	NameEN      string  `json:"name_en"`
	Coefficient float64 `json:"coefficient"`
	Enabled     bool    `json:"enabled"`
	Position    int     `json:"position"`
	UsageCount  int     `json:"usage_count,omitempty"`
}

type Blob struct {
	SHA256    string    `json:"sha256"`
	SizeBytes int64     `json:"size_bytes"`
	MediaType string    `json:"media_type"`
	LocalKey  string    `json:"local_key"`
	CreatedAt time.Time `json:"created_at"`
}

type Artifact struct {
	ID            string         `json:"id"`
	BlobSHA256    string         `json:"sha256"`
	OriginalName  string         `json:"original_name"`
	PackageFormat string         `json:"package_format"`
	PackageID     string         `json:"package_id,omitempty"`
	Version       string         `json:"package_version,omitempty"`
	SizeBytes     int64          `json:"size_bytes"`
	Analysis      map[string]any `json:"analysis"`
	DeviceIDs     []string       `json:"device_ids"`
}

type Media struct {
	ID         string    `json:"id"`
	BlobSHA256 string    `json:"sha256"`
	Role       MediaRole `json:"role"`
	Position   int       `json:"position"`
	Width      int       `json:"width"`
	Height     int       `json:"height"`
	SizeBytes  int64     `json:"size_bytes"`
}

type ReviewCase struct {
	ID         string      `json:"id"`
	RevisionID string      `json:"revision_id"`
	State      ReviewState `json:"state"`
	Note       string      `json:"note,omitempty"`
	CreatedAt  time.Time   `json:"created_at"`
	UpdatedAt  time.Time   `json:"updated_at"`
}

type Publication struct {
	ID           string            `json:"id"`
	RevisionID   string            `json:"revision_id"`
	Target       PublicationTarget `json:"target"`
	State        PublicationState  `json:"state"`
	Config       map[string]any    `json:"config"`
	ExternalID   string            `json:"external_id,omitempty"`
	ExternalURL  string            `json:"external_url,omitempty"`
	ErrorMessage string            `json:"error_message,omitempty"`
	StatusDetail map[string]any    `json:"status_detail,omitempty"`
	UpdatedAt    time.Time         `json:"updated_at"`
}

type ExternalBinding struct {
	Provider    string            `json:"provider"`
	ExternalID  string            `json:"external_id"`
	ExternalURL string            `json:"external_url"`
	Meta        map[string]string `json:"meta,omitempty"`
}

type Workspace struct {
	Resource        Resource          `json:"resource"`
	CurrentRevision *Revision         `json:"current_revision,omitempty"`
	Revisions       []Revision        `json:"revisions"`
	Artifacts       []Artifact        `json:"artifacts"`
	Media           []Media           `json:"media"`
	Links           []ResourceLink    `json:"links"`
	Review          *ReviewCase       `json:"review,omitempty"`
	Publications    []Publication     `json:"publications"`
	Bindings        []ExternalBinding `json:"bindings"`
}

type CollectionRevision struct {
	ID                       string        `json:"id"`
	CollectionID             string        `json:"collection_id"`
	Number                   int           `json:"number"`
	Name                     string        `json:"name"`
	Summary                  string        `json:"summary"`
	State                    RevisionState `json:"state"`
	ReviewNote               string        `json:"review_note,omitempty"`
	Enabled                  bool          `json:"enabled"`
	RepresentativeResourceID string        `json:"representative_resource_id,omitempty"`
	ResourceIDs              []string      `json:"resource_ids"`
	CreatedVia               string        `json:"created_via"`
	BaseRevisionID           string        `json:"base_revision_id,omitempty"`
	CreatedAt                time.Time     `json:"created_at"`
	UpdatedAt                time.Time     `json:"updated_at"`
}

type Collection struct {
	ID                       string              `json:"id"`
	OwnerID                  string              `json:"owner_id"`
	Slug                     string              `json:"slug"`
	Platform                 string              `json:"platform"`
	Kind                     ResourceKind        `json:"kind"`
	CurrentRevisionID        string              `json:"current_revision_id,omitempty"`
	RepresentativeResourceID string              `json:"representative_resource_id,omitempty"`
	Enabled                  bool                `json:"enabled"`
	CurrentRevision          *CollectionRevision `json:"current_revision,omitempty"`
	PendingRevision          *CollectionRevision `json:"pending_revision,omitempty"`
	ResourceCount            int                 `json:"resource_count"`
	TotalCoins               int64               `json:"total_coins"`
	CreatedAt                time.Time           `json:"created_at"`
	UpdatedAt                time.Time           `json:"updated_at"`
}

type Collaborator struct {
	UserID     string     `json:"user_id"`
	Username   string     `json:"username"`
	AvatarURL  string     `json:"avatar_url"`
	AcceptedAt *time.Time `json:"accepted_at,omitempty"`
}

type CollaborationInvitation struct {
	ResourceID   string    `json:"resource_id"`
	ResourceName string    `json:"resource_name"`
	Owner        string    `json:"owner"`
	InvitedAt    time.Time `json:"invited_at"`
}

type ResourceSource struct {
	AuthorName        string `json:"author_name"`
	SourceURL         string `json:"source_url"`
	LicenseName       string `json:"license_name"`
	AuthorizationNote string `json:"authorization_note"`
}

type Device struct {
	ID         string `json:"id"`
	Codename   string `json:"codename"`
	Name       string `json:"name"`
	Platform   string `json:"platform"`
	AstroBoxID string `json:"astrobox_id,omitempty"`
	Vendor     string `json:"vendor,omitempty"`
}

type PublicationRequest struct {
	Target PublicationTarget `json:"target"`
	Config map[string]any    `json:"config"`
}

func validBandBBSConfig(value map[string]any) bool {
	agreement, _ := value["agreement"].(bool)
	if !agreement {
		return false
	}
	targets, ok := value["targets"].([]any)
	if !ok || len(targets) == 0 {
		return false
	}
	for _, item := range targets {
		target, ok := item.(map[string]any)
		if !ok {
			return false
		}
		category, _ := target["category_id"].(float64)
		if category == 0 {
			if number, ok := target["category_id"].(int); ok {
				category = float64(number)
			}
		}
		pkg, _ := target["package_id"].(string)
		if category <= 0 || strings.TrimSpace(pkg) == "" {
			return false
		}
	}
	return true
}

func bandBBSTargetPackages(value map[string]any) []string {
	targets, _ := value["targets"].([]any)
	packages := make([]string, 0, len(targets))
	for _, item := range targets {
		if target, ok := item.(map[string]any); ok {
			packages = append(packages, fmt.Sprint(target["package_id"]))
		}
	}
	return packages
}

func validAstroBoxConfig(value map[string]any) bool {
	agreement, _ := value["agreement"].(bool)
	itemID := strings.TrimSpace(fmt.Sprint(value["item_id"]))
	repoName := strings.TrimSpace(fmt.Sprint(value["repo_name"]))
	tagCount := 0
	switch tags := value["tags"].(type) {
	case []any:
		tagCount = len(tags)
	case []string:
		tagCount = len(tags)
	}
	if strings.HasPrefix(itemID, "<nil>") {
		itemID = ""
	}
	if strings.HasPrefix(repoName, "<nil>") {
		repoName = ""
	}
	return agreement && itemID != "" && repoName != "" && tagCount > 0
}

type PublicResource struct {
	CardType           string           `json:"card_type"`
	ID                 string           `json:"id"`
	Slug               string           `json:"slug"`
	Name               string           `json:"name"`
	Summary            string           `json:"summary"`
	Owner              string           `json:"owner"`
	OwnerBandBBSUserID int64            `json:"owner_bandbbs_user_id"`
	OwnerAvatarURL     string           `json:"owner_avatar_url"`
	PreviewSHA256      string           `json:"preview_sha256"`
	IconSHA256         string           `json:"icon_sha256"`
	CoverSHA256        string           `json:"cover_sha256"`
	Kind               string           `json:"kind"`
	PaidType           ResourcePaidType `json:"paid_type"`
	Version            string           `json:"version"`
	Devices            []string         `json:"devices"`
	Attributes         []string         `json:"attributes,omitempty"`
	DownloadCount      int              `json:"download_count"`
	CurationGrade      string           `json:"curation_grade"`
	CoinCount          int64            `json:"coin_count"`
	CollectionID       string           `json:"collection_id,omitempty"`
	CollectionName     string           `json:"collection_name,omitempty"`
	ResourceCount      int              `json:"resource_count,omitempty"`
	PublishedAt        time.Time        `json:"published_at,omitempty"`
	UpdatedAt          time.Time        `json:"updated_at"`
}

type PublicCollection struct {
	ID                       string           `json:"id"`
	Slug                     string           `json:"slug"`
	Name                     string           `json:"name"`
	Summary                  string           `json:"summary"`
	Kind                     ResourceKind     `json:"kind"`
	Owner                    string           `json:"owner"`
	OwnerBandBBSUserID       int64            `json:"owner_bandbbs_user_id"`
	OwnerAvatarURL           string           `json:"owner_avatar_url"`
	RepresentativeResourceID string           `json:"representative_resource_id"`
	Representative           *PublicResource  `json:"representative,omitempty"`
	Resources                []PublicResource `json:"resources,omitempty"`
	ResourceCount            int              `json:"resource_count"`
	CoinCount                int64            `json:"coin_count"`
	UpdatedAt                time.Time        `json:"updated_at"`
}

type PublicResourceDetail struct {
	PublicResource
	Media            []Media         `json:"media"`
	Artifacts        []Artifact      `json:"artifacts"`
	Collaborators    []Collaborator  `json:"collaborators"`
	Source           *ResourceSource `json:"source,omitempty"`
	Links            []ResourceLink  `json:"links"`
	PurchaseLink     string          `json:"purchase_link,omitempty"`
	PurchasePrice    *float64        `json:"purchase_price,omitempty"`
	PurchaseCurrency string          `json:"purchase_currency,omitempty"`
}

type PublicQuery struct {
	Limit  int
	Offset int
	Search string
	Kind   string
	Sort   string
	// Seed keeps recommendation ordering stable for one feed session while
	// allowing the caller to rotate the exploration order explicitly.
	Seed          int64
	Devices       []string
	Attributes    []string
	HidePaid      bool
	HideForcePaid bool
	Featured      bool
	// SkipRecommendationScore avoids the expensive interaction and attribute
	// coefficient calculation for feeds that sort exclusively by publication
	// time, such as the home page's latest row.
	SkipRecommendationScore bool
}

// PublicHomeFeed is the de-duplicated resource portion of the application
// home page. The server owns the candidate selection so the three rows share
// one ordering seed and cannot repeat the same card across rows.
type PublicHomeFeed struct {
	Featured    []PublicResource `json:"featured"`
	Recommended []PublicResource `json:"recommended"`
	Latest      []PublicResource `json:"latest"`
}

type PublicHomeQuery struct {
	Seed       int64
	ExcludeIDs []string
	RowSize    int
}
