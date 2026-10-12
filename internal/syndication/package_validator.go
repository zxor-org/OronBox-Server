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

// ValidateQuickAppPackage validates an actual Vela RPK. Its manifest uses
// package/versionName, unlike the community repository bundle manifest.
func ValidateQuickAppPackage(data []byte, expectedID string) error {
	if len(data) == 0 || len(data) > MaxUploadBytes {
		return fmt.Errorf("invalid package size")
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("invalid rpk archive")
	}
	if len(zr.File) > MaxFiles {
		return fmt.Errorf("too many package files")
	}
	seen := map[string]bool{}
	var manifest []byte
	var expanded int64
	for _, entry := range zr.File {
		name := path.Clean(strings.ReplaceAll(entry.Name, "\\", "/"))
		if name == ".." || strings.HasPrefix(name, "../") || strings.HasPrefix(name, "/") {
			return fmt.Errorf("unsafe package path")
		}
		if entry.Mode()&0o120000 != 0 {
			return fmt.Errorf("package symlink is not allowed")
		}
		if entry.FileInfo().IsDir() {
			continue
		}
		if seen[name] {
			return fmt.Errorf("duplicate package path")
		}
		seen[name] = true
		if entry.UncompressedSize64 > uint64(MaxExpandedBytes-expanded) {
			return fmt.Errorf("expanded package exceeds limit")
		}
		reader, err := entry.Open()
		if err != nil {
			return err
		}
		var size int64
		if name == "manifest.json" {
			manifest, err = io.ReadAll(io.LimitReader(reader, (1<<20)+1))
			size = int64(len(manifest))
			if size > 1<<20 {
				err = fmt.Errorf("package manifest is too large")
			}
		} else {
			size, err = io.Copy(io.Discard, io.LimitReader(reader, MaxExpandedBytes-expanded+1))
		}
		_ = reader.Close()
		if err != nil {
			return err
		}
		expanded += size
		if expanded > MaxExpandedBytes {
			return fmt.Errorf("expanded package exceeds limit")
		}
	}
	var metadata struct {
		Package      string `json:"package"`
		PackageName  string `json:"packageName"`
		PackageSnake string `json:"package_name"`
	}
	if len(manifest) == 0 || json.Unmarshal(manifest, &metadata) != nil {
		return fmt.Errorf("package manifest.json is required")
	}
	id := metadata.Package
	if id == "" {
		id = metadata.PackageName
	}
	if id == "" {
		id = metadata.PackageSnake
	}
	if id == "" || id != expectedID {
		return fmt.Errorf("package id does not match resource")
	}
	return nil
}
