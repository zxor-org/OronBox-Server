package syndication

import (
	"archive/zip"
	"bytes"
	"context"
	"testing"
	"time"
)

func bundle(t *testing.T) []byte {
	t.Helper()
	return zipBytes(t, `{"item":{"id":"com.example.app","tagline":"A valid app"},"downloads":{"xmb9p":{"file_name":"downloads/a.rpk","version":"1.0.0"}}}`, map[string][]byte{
		"media/icon.webp": webp(),
		"downloads/a.rpk": []byte("payload"),
	})
}
func webp() []byte {
	b := make([]byte, 30)
	copy(b, "RIFFxxxxWEBPVP8X")
	b[24], b[25], b[26] = 255, 1, 0
	b[27], b[28], b[29] = 255, 1, 0
	return b
}

func TestValidateBundle(t *testing.T) {
	if _, e := ValidateBundle(bundle(t), "com.example.app"); e != nil {
		t.Fatal(e)
	}
	if _, e := ValidateBundle(bundle(t), "other"); e == nil {
		t.Fatal("wrong package accepted")
	}
}
func TestValidateBundleRejectsUnsafePath(t *testing.T) {
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	w, _ := z.Create("../bad")
	_, _ = w.Write([]byte("x"))
	_ = z.Close()
	if _, e := ValidateBundle(b.Bytes(), ""); e == nil {
		t.Fatal("unsafe path accepted")
	}
}
func TestValidateBundleRejectsBadZip(t *testing.T) {
	if _, e := ValidateBundle([]byte("bad"), ""); e == nil {
		t.Fatal("bad zip accepted")
	}
}

func TestValidateBundleRejectsTaglineAndMissingDownload(t *testing.T) {
	cases := map[string]struct {
		manifest string
		files    map[string][]byte
	}{
		"tagline too short": {`{"item":{"id":"com.example.app","tagline":"x"}}`, map[string][]byte{"media/icon.webp": webp()}},
		"tagline too long":  {`{"item":{"id":"com.example.app","tagline":"` + longString(81) + `"}}`, map[string][]byte{"media/icon.webp": webp()}},
		"download missing":  {`{"item":{"id":"com.example.app","tagline":"ok tagline"},"downloads":{"xmb9p":{"file_name":"downloads/missing.rpk"}}}`, map[string][]byte{"media/icon.webp": webp()}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ValidateBundle(zipBytes(t, tc.manifest, tc.files), "com.example.app"); err == nil {
				t.Fatal("invalid bundle accepted")
			}
		})
	}
}

func TestValidateBundleAcceptsMarkdownSymbolsInTagline(t *testing.T) {
	// tagline is display-only; markdown-looking characters must NOT be rejected.
	m := `{"item":{"id":"com.example.app","tagline":"**bold** [link] <tag> ` + "`code`" + `"},"downloads":{}}`
	if _, err := ValidateBundle(zipBytes(t, m, map[string][]byte{"media/icon.webp": webp()}), "com.example.app"); err != nil {
		t.Fatalf("symbols in tagline rejected: %v", err)
	}
}

func TestValidateBundleRejectsAbsoluteAndSymlinkPaths(t *testing.T) {
	for _, name := range []string{"/absolute", "../parent"} {
		var b bytes.Buffer
		z := zip.NewWriter(&b)
		w, _ := z.Create(name)
		_, _ = w.Write([]byte("bad"))
		_ = z.Close()
		if _, err := ValidateBundle(b.Bytes(), ""); err == nil {
			t.Fatalf("path %q accepted", name)
		}
	}
}

func TestWebpSizeParsesAllFormats(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
	}{{"vp8x", webp()}, {"vp8", webpVP8(512, 512)}, {"vp8l", webpVP8L(512, 512)}} {
		w, h, err := webpSize(tc.data)
		if err != nil || w != 512 || h != 512 {
			t.Fatalf("%s: %dx%d err=%v", tc.name, w, h, err)
		}
	}
	if _, _, err := webpSize([]byte("RIFFxxxxWEBPXXXX")); err == nil {
		t.Fatal("unsupported chunk accepted")
	}
	if _, _, err := webpSize([]byte("short")); err == nil {
		t.Fatal("short webp accepted")
	}
}
func webpVP8(w, h int) []byte {
	b := make([]byte, 30)
	copy(b, "RIFFxxxxWEBPVP8 ")
	b[26], b[27] = byte(w&0xff), byte((w>>8)&0x3f)
	b[28], b[29] = byte(h&0xff), byte((h>>8)&0x3f)
	return b
}
func webpVP8L(w, h int) []byte {
	b := make([]byte, 25)
	copy(b, "RIFFxxxxWEBPVP8L")
	b[20] = 0x2f
	bits := uint32(w-1) | uint32(h-1)<<14
	b[21], b[22], b[23], b[24] = byte(bits), byte(bits>>8), byte(bits>>16), byte(bits>>24)
	return b
}

func zipBytes(t *testing.T, manifest string, files map[string][]byte) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	w, err := z.Create("manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte(manifest))
	for name, value := range files {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write(value)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func longString(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'a'
	}
	return string(b)
}
func TestRetryWorkerHonorsAttempts(t *testing.T) {
	attempts := 0
	w := RetryWorker{MaxAttempts: 3, BaseDelay: time.Nanosecond, Run: func(context.Context, Task) error { attempts++; return context.Canceled }}
	got := w.Execute(context.Background(), Task{})
	if attempts != 3 || got.Attempts != 3 {
		t.Fatalf("attempts=%d task=%+v", attempts, got)
	}
}

func TestRetryWorkerSuccessAndCancellation(t *testing.T) {
	called := 0
	success := RetryWorker{Run: func(context.Context, Task) error { called++; return nil }}
	if got := success.Execute(context.Background(), Task{}); got.Attempts != 0 || called != 1 {
		t.Fatalf("success task=%+v called=%d", got, called)
	}
	ctx, cancel := context.WithCancel(context.Background())
	failed := RetryWorker{MaxAttempts: 3, BaseDelay: time.Hour, Run: func(context.Context, Task) error { return context.Canceled }}
	cancel()
	if got := failed.Execute(ctx, Task{}); got.Attempts != 1 {
		t.Fatalf("cancel task=%+v", got)
	}
}
