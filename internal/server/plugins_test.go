package server

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"testing"
)

func createTestPluginZip(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", name, err)
		}
		if _, err := w.Write(content); err != nil {
			t.Fatalf("write zip entry %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	return buf.Bytes()
}

func TestParsePluginPackageValid(t *testing.T) {
	manifest := `{
		"api_level": 1,
		"id": "org.zxor.sample",
		"name": "Sample Plugin",
		"version": "1.0.0",
		"author": "Alice",
		"description": "A test plugin",
		"runtime": "js",
		"entry": "main.js",
		"icon": "icon.png",
		"permissions": ["ui", "network"]
	}`

	raw := createTestPluginZip(t, map[string][]byte{
		"manifest.json": []byte(manifest),
		"main.js":       []byte("console.log('hello');"),
		"icon.png":      []byte("\x89PNG\r\n\x1a\n"),
	})

	m, files, err := parsePluginPackage(raw)
	if err != nil {
		t.Fatalf("expected valid package, got error: %v", err)
	}
	if m.ID != "org.zxor.sample" {
		t.Errorf("expected ID org.zxor.sample, got %s", m.ID)
	}
	if m.Name != "Sample Plugin" {
		t.Errorf("expected Name Sample Plugin, got %s", m.Name)
	}
	if len(files) != 3 {
		t.Errorf("expected 3 files, got %d", len(files))
	}
}

func TestParsePluginPackageErrors(t *testing.T) {
	tests := []struct {
		name     string
		files    map[string][]byte
		errMatch string
	}{
		{
			name:     "missing manifest",
			files:    map[string][]byte{"main.js": []byte("var a = 1;")},
			errMatch: "manifest.json is missing",
		},
		{
			name: "invalid api_level",
			files: map[string][]byte{
				"manifest.json": []byte(`{"api_level": 2, "id": "org.zxor.test", "name": "t", "version": "1.0", "author": "a", "runtime": "js"}`),
				"main.js":       []byte("ok"),
			},
			errMatch: "unsupported plugin API level",
		},
		{
			name: "invalid id format",
			files: map[string][]byte{
				"manifest.json": []byte(`{"api_level": 1, "id": "Invalid-ID", "name": "t", "version": "1.0", "author": "a", "runtime": "js"}`),
				"main.js":       []byte("ok"),
			},
			errMatch: "invalid plugin id",
		},
		{
			name: "missing entry file",
			files: map[string][]byte{
				"manifest.json": []byte(`{"api_level": 1, "id": "org.zxor.test", "name": "t", "version": "1.0", "author": "a", "runtime": "js", "entry": "missing.js"}`),
			},
			errMatch: "plugin entry is missing",
		},
		{
			name: "missing icon file",
			files: map[string][]byte{
				"manifest.json": []byte(`{"api_level": 1, "id": "org.zxor.test", "name": "t", "version": "1.0", "author": "a", "runtime": "js", "icon": "nonexistent.png"}`),
				"main.js":       []byte("ok"),
			},
			errMatch: "plugin icon is missing",
		},
		{
			name: "invalid permission",
			files: map[string][]byte{
				"manifest.json": []byte(`{"api_level": 1, "id": "org.zxor.test", "name": "t", "version": "1.0", "author": "a", "runtime": "js", "permissions": ["root_access"]}`),
				"main.js":       []byte("ok"),
			},
			errMatch: "unsupported plugin permission",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := createTestPluginZip(t, tt.files)
			_, _, err := parsePluginPackage(raw)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.errMatch)
			}
			if !bytes.Contains([]byte(err.Error()), []byte(tt.errMatch)) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.errMatch)
			}
		})
	}
}

func TestPendingPluginStorage(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "plugins-pending-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	os.Setenv("PLUGIN_PENDING_DIR", tempDir)
	defer os.Unsetenv("PLUGIN_PENDING_DIR")

	id := "org.zxor.testplugin"
	data := []byte("dummy obp package bytes")

	if err := savePendingPlugin(id, data); err != nil {
		t.Fatalf("savePendingPlugin failed: %v", err)
	}

	read, err := readPendingPlugin(id)
	if err != nil {
		t.Fatalf("readPendingPlugin failed: %v", err)
	}
	if !bytes.Equal(read, data) {
		t.Fatalf("read data does not match saved data")
	}

	deletePendingPlugin(id)
	if _, err := readPendingPlugin(id); !os.IsNotExist(err) && !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected file to be deleted, got err: %v", err)
	}
}

func TestNormalizePluginPath(t *testing.T) {
	good := []string{"main.js", "app/main.js", "assets/images/icon.png"}
	for _, p := range good {
		if _, err := normalizePluginPath(p); err != nil {
			t.Errorf("expected %q to be safe, got: %v", p, err)
		}
	}

	bad := []string{"/root.js", "../escape.js", "a/../../b.js", "c:\\file.js"}
	for _, p := range bad {
		if _, err := normalizePluginPath(p); err == nil {
			t.Errorf("expected %q to be rejected as unsafe", p)
		}
	}
}
