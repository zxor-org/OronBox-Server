package consoleupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

func (m *Manager) installedConsole() (*Metadata, string) {
	raw, err := os.ReadFile(filepath.Join(m.root, "current.json"))
	if err != nil {
		return nil, ""
	}
	var p struct {
		Meta      Metadata `json:"metadata"`
		Directory string   `json:"directory"`
	}
	if json.Unmarshal(raw, &p) != nil || p.Directory == "" || filepath.Base(p.Directory) != p.Directory {
		return nil, ""
	}
	if _, err := uuid.Parse(p.Directory); err != nil {
		return nil, ""
	}
	root := filepath.Join(m.root, "versions", p.Directory)
	if _, err := validateConsolePackage(root, p.Meta.Commit); err != nil {
		return nil, ""
	}
	return &p.Meta, root
}

func (m *Manager) installConsole(ctx context.Context, expected int64) error {
	if err := m.checkConsole(ctx, true); err != nil {
		return err
	}
	m.state.mu.RLock()
	latest, download := m.state.latest, m.state.download
	m.state.mu.RUnlock()
	if latest == nil || latest.AssetID != expected {
		return fmt.Errorf("Console release changed; check again")
	}
	res, err := m.consoleAsset(ctx, download)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	versions := filepath.Join(m.root, "versions")
	if err = os.MkdirAll(versions, 0755); err != nil {
		return err
	}
	candidate, err := os.MkdirTemp(versions, "candidate-")
	if err != nil {
		return err
	}
	candidateRoot := candidate
	keep := false
	defer func() {
		if !keep {
			os.RemoveAll(candidateRoot)
		}
	}()
	archive, err := os.CreateTemp(m.root, "download-*.tgz")
	if err != nil {
		return err
	}
	defer os.Remove(archive.Name())
	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(archive, hash), io.LimitReader(res.Body, (50<<20)+1))
	sum := hex.EncodeToString(hash.Sum(nil))
	if err != nil || size != latest.Size || size > 50<<20 || (latest.SHA256 != "" && sum != latest.SHA256) {
		archive.Close()
		return fmt.Errorf("Console package size or checksum mismatch")
	}
	if latest.SHA256 == "" {
		latest.SHA256 = sum
	}
	if _, err = archive.Seek(0, 0); err == nil {
		err = extractConsole(archive, candidate)
	}
	archive.Close()
	if err != nil {
		return err
	}
	if _, err = os.Stat(filepath.Join(candidate, "index.html")); err != nil {
		nested := filepath.Join(candidate, "dist")
		if _, err = os.Stat(filepath.Join(nested, "index.html")); err != nil {
			return fmt.Errorf("Console package missing index.html")
		}
		candidate = nested
	}
	commit, err := validateConsolePackage(candidate, latest.Commit)
	if err != nil {
		return err
	}
	finalName := uuid.NewString()
	final := filepath.Join(versions, finalName)
	if err = os.Rename(candidate, final); err != nil {
		return err
	}
	defer func() {
		if !keep {
			_ = os.RemoveAll(final)
		}
		_ = os.RemoveAll(candidateRoot)
	}()
	meta := *latest
	meta.Commit = commit
	meta.SHA256 = hex.EncodeToString(hash.Sum(nil))
	meta.SyncedAt = time.Now().UTC().Format(time.RFC3339)
	previous := ""
	if raw, err := os.ReadFile(filepath.Join(m.root, "current.json")); err == nil {
		var state struct {
			Directory string `json:"directory"`
		}
		if json.Unmarshal(raw, &state) == nil {
			if _, err = uuid.Parse(state.Directory); err == nil {
				previous = state.Directory
			}
		}
	}
	if err = syncConsoleDirectory(final); err != nil {
		return err
	}
	if err = syncConsoleDirectory(versions); err != nil {
		return err
	}
	raw, err := json.Marshal(map[string]any{"metadata": meta, "directory": finalName, "previous_directory": previous})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(m.root, "current-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(raw); err == nil {
		err = tmp.Sync()
	}
	tmp.Close()
	if err != nil {
		return err
	}
	if err = os.Rename(tmp.Name(), filepath.Join(m.root, "current.json")); err != nil {
		return err
	}
	keep = true
	_ = syncConsoleDirectory(m.root)
	m.state.mu.Lock()
	m.state.lastError = ""
	m.state.mu.Unlock()
	return nil
}
