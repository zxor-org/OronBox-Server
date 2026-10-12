package catalog

import (
	"archive/zip"
	"bytes"
	"testing"
)

func testABP(t *testing.T, manifest string, files map[string]string) []byte {
	t.Helper()
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(content))
	}
	w, err := zw.Create("manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte(manifest))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestValidatePluginPackage(t *testing.T) {
	valid := `{"id":"com.example.clock","name":"Clock","version":"1.0.0","runtime":"js","api_level":1,"entry":"main.js","permissions":["ui"]}`
	meta, err := validatePluginPackage(testABP(t, valid, map[string]string{"main.js": "export default {};"}))
	if err != nil || meta.ID != "com.example.clock" {
		t.Fatalf("valid package rejected: %#v %v", meta, err)
	}

	tests := []struct {
		name, manifest string
		files          map[string]string
	}{
		{"api level", `{"id":"com.example.clock","name":"Clock","version":"1","runtime":"js","api_level":2,"entry":"main.js"}`, map[string]string{"main.js": "x"}},
		{"missing entry", `{"id":"com.example.clock","name":"Clock","version":"1","runtime":"js","api_level":1,"entry":"missing.js"}`, map[string]string{"main.js": "x"}},
		{"wrong extension", `{"id":"com.example.clock","name":"Clock","version":"1","runtime":"wasm","api_level":1,"entry":"main.js"}`, map[string]string{"main.js": "x"}},
		{"unknown permission", `{"id":"com.example.clock","name":"Clock","version":"1","runtime":"js","api_level":1,"entry":"main.js","permissions":["root"]}`, map[string]string{"main.js": "x"}},
		{"traversal", `{"id":"com.example.clock","name":"Clock","version":"1","runtime":"js","api_level":1,"entry":"main.js"}`, map[string]string{"../main.js": "x"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := validatePluginPackage(testABP(t, tc.manifest, tc.files)); err == nil {
				t.Fatal("invalid package accepted")
			}
		})
	}
}
