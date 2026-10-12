package server

import (
	"net/http"
	"sync"
	"time"
)

var consoleSyncMu sync.Mutex

type consoleMetadata struct {
	Commit      string    `json:"commit,omitempty"`
	AssetID     int64     `json:"asset_id"`
	Size        int64     `json:"size,omitempty"`
	PublishedAt string    `json:"published_at,omitempty"`
	SyncedAt    time.Time `json:"synced_at,omitempty"`
}

type consoleState struct {
	mu             sync.RWMutex
	Syncing        bool             `json:"syncing"`
	SyncStart      time.Time        `json:"sync_start,omitempty"`
	LastError      string           `json:"last_error,omitempty"`
	ErrorDismissed bool             `json:"error_dismissed,omitempty"`
	Current        *consoleMetadata `json:"current,omitempty"`
	Latest         *consoleMetadata `json:"latest,omitempty"`
}

var globalConsoleState consoleState

var giteaSyncClient = &http.Client{
	Timeout: 60 * time.Second,
}

type consoleSyncResult struct {
	Synced   bool             `json:"synced"`
	Syncing  bool             `json:"syncing,omitempty"`
	Message  string           `json:"message,omitempty"`
	Metadata *consoleMetadata `json:"metadata,omitempty"`
}
