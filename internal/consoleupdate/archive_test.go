package consoleupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func consoleArchive(t *testing.T, entries []*tar.Header) *bytes.Reader {
	t.Helper()
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	for _, h := range entries {
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg && h.Size > 0 {
			if _, err := tw.Write(bytes.Repeat([]byte("x"), int(h.Size))); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(b.Bytes())
}
func TestConsoleArchiveRejectsUnsafeEntries(t *testing.T) {
	for _, tc := range []struct {
		name    string
		entries []*tar.Header
	}{
		{"traversal", []*tar.Header{{Name: "../escape", Typeflag: tar.TypeReg, Size: 1}}},
		{"absolute", []*tar.Header{{Name: "/escape", Typeflag: tar.TypeReg, Size: 1}}},
		{"symlink", []*tar.Header{{Name: "assets/link", Typeflag: tar.TypeSymlink, Linkname: "../../escape"}}},
		{"hardlink", []*tar.Header{{Name: "assets/link", Typeflag: tar.TypeLink, Linkname: "index.html"}}},
		{"duplicate", []*tar.Header{{Name: "index.html", Typeflag: tar.TypeReg, Size: 1}, {Name: "index.html", Typeflag: tar.TypeReg, Size: 1}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := extractConsole(consoleArchive(t, tc.entries), t.TempDir()); err == nil {
				t.Fatal("unsafe archive accepted")
			}
		})
	}
}
func TestConsoleArchiveExtractsOnlyRegularFiles(t *testing.T) {
	dir := t.TempDir()
	err := extractConsole(consoleArchive(t, []*tar.Header{{Name: "./", Typeflag: tar.TypeDir}, {Name: "./assets/", Typeflag: tar.TypeDir}, {Name: "./assets/main.js", Typeflag: tar.TypeReg, Size: 3}}), dir)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "assets/main.js"))
	if err != nil || string(raw) != "xxx" {
		t.Fatalf("file=%q err=%v", raw, err)
	}
}

func TestConsoleRejectsPreviousAuthenticationContract(t *testing.T) {
	root := t.TempDir()
	commit := strings.Repeat("a", 40)
	if err := os.Mkdir(filepath.Join(root, "assets"), 0755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{"index.html": `<meta name="csp-nonce" content="__CSP_NONCE__"><script src="/console/assets/app.js"></script>`, "assets/app.js": "void 0"}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	for _, contract := range []string{"oronbox-console-v1", consoleContract} {
		if err := os.WriteFile(filepath.Join(root, "console.json"), []byte(`{"admin_api_contract":"`+contract+`","commit":"`+commit+`"}`), 0644); err != nil {
			t.Fatal(err)
		}
		_, err := validateConsolePackage(root, commit)
		if contract == consoleContract && err != nil {
			t.Fatal(err)
		}
		if contract != consoleContract && err == nil {
			t.Fatal("old authentication frontend was accepted")
		}
	}
}
