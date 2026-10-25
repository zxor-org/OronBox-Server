package consoleupdate

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/zxor-org/OronBox-Server/internal/gitea"
)

func extractConsole(reader io.Reader, dir string) error {
	gz, err := gzip.NewReader(reader)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var total int64
	seen := map[string]bool{}
	for count := 0; ; count++ {
		h, err := tr.Next()
		if err == io.EOF {
			_, err = io.Copy(io.Discard, gz)
			return err
		}
		if err != nil {
			return err
		}
		if count >= 5000 {
			return fmt.Errorf("Console archive has too many entries")
		}
		name := filepath.ToSlash(h.Name)
		if name == "./" || name == "." {
			continue
		}
		name = strings.TrimPrefix(name, "./")
		if !gitea.ValidPath(strings.TrimSuffix(name, "/")) || seen[name] {
			return fmt.Errorf("invalid or duplicate Console archive path")
		}
		seen[name] = true
		target := filepath.Join(dir, filepath.FromSlash(name))
		if h.Typeflag == tar.TypeDir {
			if err = os.MkdirAll(target, 0755); err != nil {
				return err
			}
			continue
		}
		if h.Typeflag != tar.TypeReg || h.Size < 0 || h.Size > 50<<20 {
			return fmt.Errorf("unsupported Console archive entry")
		}
		total += h.Size
		if total > 200<<20 {
			return fmt.Errorf("Console archive expands beyond limit")
		}
		if err = os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err != nil {
			return err
		}
		n, err := io.CopyN(f, tr, h.Size)
		syncErr := f.Sync()
		closeErr := f.Close()
		if err != nil || syncErr != nil || closeErr != nil || n != h.Size {
			return fmt.Errorf("incomplete Console archive entry")
		}
	}
}

var consoleAssetReference = regexp.MustCompile(`(?:src|href)=["'](/console/assets/[^"'<>[:space:]]+)["']`)

func validateConsolePackage(root, expectedCommit string) (string, error) {
	commit := expectedCommit
	raw, err := os.ReadFile(filepath.Join(root, "console.json"))
	if err == nil {
		var marker struct {
			Contract string `json:"admin_api_contract"`
			Commit   string `json:"commit"`
		}
		if json.Unmarshal(raw, &marker) == nil {
			if marker.Contract != "" && marker.Contract != consoleContract {
				return "", fmt.Errorf("Console package contract mismatch")
			}
			if marker.Commit != "" {
				commit = marker.Commit
			}
		}
	}
	index, err := os.ReadFile(filepath.Join(root, "index.html"))
	if err != nil || !strings.Contains(string(index), "__CSP_NONCE__") {
		return "", fmt.Errorf("Console entry lacks CSP nonce support")
	}
	script := false
	for _, match := range consoleAssetReference.FindAllStringSubmatch(string(index), -1) {
		name := strings.TrimPrefix(match[1], "/console/")
		if !gitea.ValidPath(name) {
			return "", fmt.Errorf("invalid Console static reference")
		}
		stat, err := os.Stat(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil || !stat.Mode().IsRegular() {
			return "", fmt.Errorf("Console entry references a missing static asset")
		}
		if strings.HasSuffix(name, ".js") {
			script = true
		}
	}
	if !script {
		return "", fmt.Errorf("Console entry missing its application script")
	}
	return commit, nil
}

func syncConsoleDirectory(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
