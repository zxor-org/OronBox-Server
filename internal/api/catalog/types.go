package catalog

import "encoding/json"

// CatalogItem strictly adheres to server-api-spec.md §3.1
type CatalogItem struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Kind          string          `json:"kind"`
	Summary       string          `json:"summary"`
	Author        string          `json:"author"`
	Authors       json.RawMessage `json:"authors"`
	OwnerUID      int             `json:"owner_uid"`
	Owner         string          `json:"owner"`
	Devices       []string        `json:"devices"`
	Tags          []string        `json:"tags"`
	Attributes    []string        `json:"attributes,omitempty"`
	PaidType      string          `json:"paid_type"`
	IconSHA256    string          `json:"icon_sha256,omitempty"`
	CoverSHA256   string          `json:"cover_sha256,omitempty"`
	IconURL       string          `json:"icon_url"`
	CoverURL      string          `json:"cover_url"`
	Version       string          `json:"version"`
	DownloadCount int             `json:"download_count"`
	CoinCount     int             `json:"coin_count"`
	Rating        float64         `json:"rating"`
	RatingCount   int             `json:"rating_count"`
	CurationGrade string          `json:"curation_grade"`
	UpdatedAt     string          `json:"updated_at"`
}

type CatalogListResponse struct {
	Items   []CatalogItem `json:"items"`
	Total   int           `json:"total"`
	HasMore bool          `json:"has_more"`
}

type DownloadTarget struct {
	DownloadURL string `json:"download_url"`
	SHA256      string `json:"sha256"`
	SizeBytes   int64  `json:"size_bytes"`
	FileName    string `json:"file_name"`
	VersionCode int    `json:"version_code"`
}

type ResourceDetailResponse struct {
	CatalogItem
	Description string                    `json:"description"`
	PreviewURLs []string                  `json:"preview_urls"`
	Changelog   string                    `json:"changelog"`
	Downloads   map[string]DownloadTarget `json:"downloads"`
}

type TagItem struct {
	ID          int     `json:"id"`
	Name        string  `json:"name"`
	Coefficient float64 `json:"coefficient"`
	UsageCount  int     `json:"usage_count"`
}

type ReleaseItem struct {
	Version        string `json:"version"`
	MinimumVersion string `json:"minimum_version"`
	NotesZH        string `json:"notes_zh"`
	NotesEN        string `json:"notes_en"`
	PublishedAt    string `json:"published_at"`
}
