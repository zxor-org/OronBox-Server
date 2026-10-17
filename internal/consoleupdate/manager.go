package consoleupdate

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/config"
)

const consoleContract = "oronbox-legacy-v1"

type Metadata struct {
	Commit      string `json:"commit"`
	Tag         string `json:"tag"`
	ReleaseID   int64  `json:"release_id"`
	AssetID     int64  `json:"asset_id"`
	Size        int64  `json:"size"`
	PublishedAt string `json:"published_at"`
	SyncedAt    string `json:"synced_at,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
	Directory   string `json:"-"`
}

type lifecycle struct {
	mu         sync.RWMutex
	install    sync.Mutex
	check      sync.Mutex
	http       *http.Client
	checkError string
	latest     *Metadata
	download   string
	lastCheck  time.Time
	lastError  string
}

type Options struct {
	Directory             string
	Source                config.DeviceCatalogConfig
	Repository            string
	ContentSecurityPolicy string
}
type Manager struct {
	root       string
	source     config.DeviceCatalogConfig
	repository string
	csp        string
	state      lifecycle
}

func New(options Options) *Manager {
	return &Manager{root: options.Directory, source: options.Source, repository: options.Repository, csp: options.ContentSecurityPolicy}
}

var ErrBusy = errors.New("Console update already running")

type Status struct {
	Installed bool      `json:"installed"`
	Syncing   bool      `json:"syncing"`
	CanSync   bool      `json:"can_sync"`
	HasUpdate bool      `json:"has_update"`
	Current   *Metadata `json:"current"`
	Latest    *Metadata `json:"latest"`
	LastError string    `json:"last_error"`
}

func (m *Manager) Status(ctx context.Context, force bool) Status {
	err := m.checkConsole(ctx, force)
	current, _ := m.installedConsole()
	m.state.mu.RLock()
	latest, last := m.state.latest, m.state.lastError
	m.state.mu.RUnlock()
	if err != nil {
		last = err.Error()
	}
	syncing := !m.state.install.TryLock()
	if !syncing {
		m.state.install.Unlock()
	}
	return Status{Installed: current != nil, Syncing: syncing, CanSync: err == nil && latest != nil && !syncing, HasUpdate: err == nil && latest != nil && (current == nil || current.AssetID != latest.AssetID), Current: current, Latest: latest, LastError: last}
}
func (m *Manager) Install(ctx context.Context, assetID int64) (*Metadata, error) {
	if !m.state.install.TryLock() {
		return nil, ErrBusy
	}
	defer m.state.install.Unlock()
	if err := m.installConsole(ctx, assetID); err != nil {
		m.state.mu.Lock()
		m.state.lastError = err.Error()
		m.state.mu.Unlock()
		return nil, err
	}
	current, _ := m.installedConsole()
	return current, nil
}

func (m *Manager) Ensure(ctx context.Context) error {
	if current, _ := m.installedConsole(); current != nil {
		return nil
	}
	status := m.Status(ctx, true)
	if !status.CanSync {
		if status.LastError != "" {
			return errors.New(status.LastError)
		}
		return errors.New("no installable Console build")
	}
	_, err := m.Install(ctx, status.Latest.AssetID)
	return err
}
