package server

import (
	"archive/zip"
	"bytes"
	"testing"
)

func buildPluginPackage(t *testing.T, files map[string]string) []byte {
	t.Helper()
	buffer := &bytes.Buffer{}
	writer := zip.NewWriter(buffer)
	for name, content := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func validPluginFiles() map[string]string {
	return map[string]string{
		"manifest.json": `{
			"id": "com.example.counter",
			"name": "Counter",
			"version": "1.0.0",
			"author": "tester",
			"description": "counts",
			"api_level": 1,
			"runtime": "js",
			"entry": "main.js",
			"permissions": ["ui", "network"],
			"icon": "icon.png"
		}`,
		"main.js":  "globalThis.activate = async () => {};",
		"icon.png": "\x89PNG\r\n\x1a\n",
	}
}

func TestParsePluginPackageValid(t *testing.T) {
	manifest, icon, err := parsePluginPackage(buildPluginPackage(t, validPluginFiles()))
	if err != nil {
		t.Fatalf("valid package rejected: %v", err)
	}
	if manifest.ID != "com.example.counter" || manifest.Name != "Counter" || manifest.Runtime != "js" {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}
	if len(icon) == 0 {
		t.Fatal("icon bytes were not extracted")
	}
}

func TestParsePluginPackageRejects(t *testing.T) {
	cases := map[string]map[string]string{
		"no manifest": {
			"main.js": "x",
		},
		"legacy runtime": {
			"manifest.json": `{"name":"x","version":"1.0.0","api_level":1,"entry":"main.js"}`,
			"main.js":       "x",
		},
		"bad id": {
			"manifest.json": `{"id":"Bad ID","name":"x","version":"1.0.0","api_level":1,"runtime":"js","entry":"main.js"}`,
			"main.js":       "x",
		},
		"missing entry": {
			"manifest.json": `{"id":"com.example.x","name":"x","version":"1.0.0","api_level":1,"runtime":"js","entry":"gone.js"}`,
			"main.js":       "x",
		},
		"unknown permission": {
			"manifest.json": `{"id":"com.example.x","name":"x","version":"1.0.0","api_level":1,"runtime":"js","entry":"main.js","permissions":["root"]}`,
			"main.js":       "x",
		},
		"js entry not javascript": {
			"manifest.json": `{"id":"com.example.x","name":"x","version":"1.0.0","api_level":1,"runtime":"js","entry":"main.txt"}`,
			"main.txt":      "x",
		},
		"unsafe path": {
			"manifest.json":   `{"id":"com.example.x","name":"x","version":"1.0.0","api_level":1,"runtime":"js","entry":"main.js"}`,
			"../evil/main.js": "x",
			"main.js":         "x",
		},
	}
	for name, files := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, err := parsePluginPackage(buildPluginPackage(t, files)); err == nil {
				t.Fatal("invalid package was accepted")
			}
		})
	}
}

func TestParsePluginPackageWasm(t *testing.T) {
	files := map[string]string{
		"manifest.json": `{"id":"com.example.wasm","name":"x","version":"1.0.0","api_level":1,"runtime":"wasm","entry":"main.wasm"}`,
		"main.wasm":     "\x00asm\x01\x00\x00\x00",
	}
	if _, _, err := parsePluginPackage(buildPluginPackage(t, files)); err != nil {
		t.Fatalf("valid wasm package rejected: %v", err)
	}
	files["main.wasm"] = "not wasm"
	if _, _, err := parsePluginPackage(buildPluginPackage(t, files)); err == nil {
		t.Fatal("wasm package with bad header was accepted")
	}
}
