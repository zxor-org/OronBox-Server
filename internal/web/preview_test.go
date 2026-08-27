package web

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWritePreview renders the remaining server-side pages so they can be
// inspected in a browser without a database. It is opt-in because it is a
// tool, not a check.
//
//	ADMIN_PREVIEW_DIR=/tmp/preview go test ./internal/web/ -run TestWritePreview
func TestWritePreview(t *testing.T) {
	directory := os.Getenv("ADMIN_PREVIEW_DIR")
	if directory == "" {
		t.Skip("ADMIN_PREVIEW_DIR is not set")
	}
	assets := filepath.Join(directory, "assets")
	if err := os.MkdirAll(assets, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, asset := range []struct{ name, body string }{
		{"app.css", CSS}, {"theme.js", ThemeJS}, {"transition.js", TransitionJS},
	} {
		if err := os.WriteFile(filepath.Join(assets, asset.name), []byte(asset.body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	templates := NewTemplates()
	written := 0
	for name, data := range renderableTemplateCases() {
		if values, ok := data.(map[string]any); ok {
			values["CSRFToken"] = "preview-token"
		}
		file, err := os.Create(filepath.Join(directory, name+".html"))
		if err != nil {
			t.Fatal(err)
		}
		if err := templates.t.ExecuteTemplate(file, name, data); err != nil {
			file.Close()
			t.Fatalf("render %s: %v", name, err)
		}
		file.Close()
		written++
	}
	t.Logf("wrote %d pages to %s", written, directory)
}
