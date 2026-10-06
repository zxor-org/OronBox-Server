package syndication

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"
)

const (
	MaxUploadBytes   = 100 << 20
	MaxExpandedBytes = 200 << 20
	MaxFiles         = 100
)

// Manifest mirrors community-repository-spec.md §3.1. The resource model carries
// NO package checksums; `downloads` is an object keyed by device codename. The
// manifest is the pure artifact definition: it does NOT carry tags/paid_type/
// purchase (those ride the submission config).
type Manifest struct {
	Item struct {
		ID          string   `json:"id"`
		Restype     string   `json:"restype"`
		Name        string   `json:"name"`
		Tagline     string   `json:"tagline"`
		Description string   `json:"description"`
		Preview     []string `json:"preview"`
		Icon        string   `json:"icon"`
		Cover       string   `json:"cover"`
		Author      []Author `json:"author"`
	} `json:"item"`
	Links     []Link                   `json:"links"`
	Downloads map[string]DownloadEntry `json:"downloads"`
}

// Author is one entry of manifest.item.author (community-repository-spec §3.1).
type Author struct {
	Name          string `json:"name"`
	Link          string `json:"link,omitempty"`
	Role          string `json:"role,omitempty"`
	UserID        int64  `json:"user_id,omitempty"`
	BindABAccount bool   `json:"bindABAccount,omitempty"`
}

// Link is one entry of manifest.links.
type Link struct {
	Title string `json:"title"`
	URL   string `json:"url"`
	Icon  string `json:"icon,omitempty"`
}

// Changelog mirrors changelog.json (community-repository-spec §3.2). Optional.
type Changelog struct {
	Releases []Release `json:"releases"`
}
type Release struct {
	Version string   `json:"version"`
	Content string   `json:"content"`
	Devices []string `json:"devices,omitempty"`
}

type DownloadEntry struct {
	Version     string `json:"version"`
	FileName    string `json:"file_name"`
	VersionCode int    `json:"versionCode"`
}
type Bundle struct {
	Manifest Manifest
	Files    map[string][]byte
}

func ValidateBundle(data []byte, expectedID string) (Bundle, error) {
	if len(data) > MaxUploadBytes {
		return Bundle{}, fmt.Errorf("bundle exceeds %d bytes", MaxUploadBytes)
	}
	zr, e := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if e != nil {
		return Bundle{}, fmt.Errorf("invalid zip: %w", e)
	}
	if len(zr.File) > MaxFiles {
		return Bundle{}, fmt.Errorf("bundle contains too many files")
	}
	out := Bundle{Files: map[string][]byte{}}
	var expanded int64
	for _, f := range zr.File {
		name := path.Clean(strings.ReplaceAll(f.Name, "\\", "/"))
		if f.FileInfo().IsDir() || strings.HasPrefix(name, "__MACOSX/") || path.Base(name) == ".DS_Store" {
			continue
		}
		if name == ".." || strings.HasPrefix(name, "../") || strings.HasPrefix(name, "/") {
			return Bundle{}, fmt.Errorf("unsafe path %q", f.Name)
		}
		if f.Mode()&0o120000 != 0 {
			return Bundle{}, fmt.Errorf("symlink %q is not allowed", f.Name)
		}
		rc, e := f.Open()
		if e != nil {
			return Bundle{}, e
		}
		content, e := io.ReadAll(io.LimitReader(rc, MaxExpandedBytes-expanded+1))
		_ = rc.Close()
		if e != nil {
			return Bundle{}, e
		}
		expanded += int64(len(content))
		if expanded > MaxExpandedBytes {
			return Bundle{}, fmt.Errorf("expanded bundle exceeds %d bytes", MaxExpandedBytes)
		}
		out.Files[name] = content
	}
	manifestRaw, ok := out.Files["manifest.json"]
	if !ok {
		return Bundle{}, fmt.Errorf("manifest.json is required")
	}
	if e := json.Unmarshal(manifestRaw, &out.Manifest); e != nil {
		return Bundle{}, fmt.Errorf("invalid manifest: %w", e)
	}
	if expectedID != "" && out.Manifest.Item.ID != expectedID {
		return Bundle{}, fmt.Errorf("manifest item id does not match route")
	}
	// tagline is a display-only subtitle: only the length is constrained.
	if n := len([]rune(strings.TrimSpace(out.Manifest.Item.Tagline))); n < 2 || n > 80 {
		return Bundle{}, fmt.Errorf("tagline must be 2-80 characters")
	}
	icon, ok := out.Files["media/icon.webp"]
	if !ok {
		return Bundle{}, fmt.Errorf("media/icon.webp is required")
	}
	if w, h, e := webpSize(icon); e != nil || w != 512 || h != 512 {
		return Bundle{}, fmt.Errorf("media/icon.webp must be 512x512")
	}
	for device, entry := range out.Manifest.Downloads {
		if strings.TrimSpace(entry.FileName) == "" {
			return Bundle{}, fmt.Errorf("downloads[%s] is missing file_name", device)
		}
		if _, ok := out.Files[path.Clean(entry.FileName)]; !ok {
			return Bundle{}, fmt.Errorf("declared download %q for device %q is missing", entry.FileName, device)
		}
	}
	return out, nil
}

func webpSize(b []byte) (int, int, error) {
	if len(b) < 16 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WEBP" {
		return 0, 0, fmt.Errorf("not webp")
	}
	switch string(b[12:16]) {
	case "VP8X": // extended format
		if len(b) < 30 {
			return 0, 0, fmt.Errorf("truncated vp8x")
		}
		w := 1 + (int(b[24]) | int(b[25])<<8 | int(b[26])<<16)
		h := 1 + (int(b[27]) | int(b[28])<<8 | int(b[29])<<16)
		return w, h, nil
	case "VP8 ": // lossy: 14-bit dimensions after the 3-byte start code
		if len(b) < 30 {
			return 0, 0, fmt.Errorf("truncated vp8")
		}
		w := int(b[26]) | int(b[27])<<8
		h := int(b[28]) | int(b[29])<<8
		return w & 0x3FFF, h & 0x3FFF, nil
	case "VP8L": // lossless: 14-bit (dim-1) pairs bit-packed from byte 21
		if len(b) < 25 {
			return 0, 0, fmt.Errorf("truncated vp8l")
		}
		bits := uint32(b[21]) | uint32(b[22])<<8 | uint32(b[23])<<16 | uint32(b[24])<<24
		return int(bits&0x3FFF) + 1, int((bits>>14)&0x3FFF) + 1, nil
	}
	return 0, 0, fmt.Errorf("unsupported webp chunk")
}
