package blob

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestSHA256KeyUsesCanonicalFanout(t *testing.T) {
	digest := "ce6d799d5ecea482ca521222e3a09be28a11f344f29049c03d716c1e868f1170"
	if got, want := SHA256Key(digest), "sha256/ce/6d/"+digest; got != want {
		t.Fatalf("SHA256Key() = %q, want %q", got, want)
	}
}

func TestLocalDeduplicatesByContent(t *testing.T) {
	store, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.Put(context.Background(), strings.NewReader("OronBox"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Put(context.Background(), strings.NewReader("OronBox"))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("deduplicated objects differ: %#v %#v", first, second)
	}
	reader, err := store.Open(context.Background(), first.Key)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil || string(data) != "OronBox" {
		t.Fatalf("read = %q, %v", data, err)
	}
}
