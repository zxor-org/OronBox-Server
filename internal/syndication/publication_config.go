package syndication

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed data/devices.json data/publish.json
var publicationDefaults embed.FS

// PublicationConfigSource describes the server-owned source for publication
// mappings. Production callers should configure the Gitea repository; LocalDir
// and the embedded baseline exist for local development and tests only.
type PublicationConfigSource struct {
	Client   GiteaClient
	Owner    string
	Repo     string
	Ref      string
	LocalDir string
}

// SplitRepository parses the configured owner/repository form.
func SplitRepository(value string) (string, string, error) {
	parts := strings.Split(strings.Trim(strings.TrimSpace(value), "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid gitea repository %q", value)
	}
	return parts[0], parts[1], nil
}

// Load fetches the central mappings from Gitea. A configured Gitea endpoint is
// authoritative: failures are returned instead of silently using stale data.
// LocalDir is only considered when no Gitea endpoint is configured.
func (s PublicationConfigSource) Load(ctx context.Context) (map[string]DeviceSpec, PublishConfig, error) {
	if strings.TrimSpace(s.Client.BaseURL) != "" {
		if s.Owner == "" || s.Repo == "" {
			return nil, PublishConfig{}, fmt.Errorf("publication repository is not configured")
		}
		ref := s.Ref
		if ref == "" {
			ref = "main"
		}
		devices, err := s.Client.ReadFile(ctx, s.Owner, s.Repo, "devices.json", ref)
		if err != nil {
			return nil, PublishConfig{}, fmt.Errorf("fetch devices.json from gitea: %w", err)
		}
		publish, err := s.Client.ReadFile(ctx, s.Owner, s.Repo, "publish.json", ref)
		if err != nil {
			return nil, PublishConfig{}, fmt.Errorf("fetch publish.json from gitea: %w", err)
		}
		return parsePublicationConfig(devices, publish)
	}
	return LoadPublicationConfig(s.LocalDir)
}

// LoadPublicationConfig loads operator-owned mappings. If no directory is
// configured, the bundled baseline is used for local development and tests.
// Client-submitted category IDs and prefixes are deliberately ignored.
func LoadPublicationConfig(dir string) (map[string]DeviceSpec, PublishConfig, error) {
	read := func(name string) ([]byte, error) {
		if dir != "" {
			return os.ReadFile(filepath.Join(dir, name))
		}
		return publicationDefaults.ReadFile("data/" + name)
	}
	raw, err := read("devices.json")
	if err != nil {
		return nil, PublishConfig{}, fmt.Errorf("load devices mapping: %w", err)
	}
	devicesRaw := raw
	raw, err = read("publish.json")
	if err != nil {
		return nil, PublishConfig{}, fmt.Errorf("load publication mapping: %w", err)
	}
	return parsePublicationConfig(devicesRaw, raw)
}

func parsePublicationConfig(devicesRaw, publishRaw []byte) (map[string]DeviceSpec, PublishConfig, error) {
	devices := map[string]DeviceSpec{}
	publish := PublishConfig{}
	if err := json.Unmarshal(devicesRaw, &devices); err != nil {
		return nil, publish, fmt.Errorf("invalid devices mapping: %w", err)
	}
	if err := json.Unmarshal(publishRaw, &publish); err != nil {
		return nil, publish, fmt.Errorf("invalid publication mapping: %w", err)
	}
	if len(devices) == 0 || len(publish.BandBBS) == 0 || publish.ResourcePrefixes == nil {
		return nil, publish, fmt.Errorf("publication mappings are empty")
	}
	// Legacy community maps used app for quick_app.
	if publish.ResourcePrefixes["quick_app"] == 0 {
		publish.ResourcePrefixes["quick_app"] = publish.ResourcePrefixes["app"]
	}
	for key, category := range publish.BandBBS {
		if category.ThreadPrefixes == nil {
			category.ThreadPrefixes = map[string]int{}
		}
		if category.ThreadPrefixes["quick_app"] == 0 {
			category.ThreadPrefixes["quick_app"] = category.ThreadPrefixes["app"]
		}
		publish.BandBBS[key] = category
	}
	return devices, publish, nil
}
