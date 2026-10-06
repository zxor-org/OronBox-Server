package main

import "testing"

func TestFormatUUIDAndStringValue(t *testing.T) {
	b := [16]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}
	want := "01020304-0506-0708-090a-0b0c0d0e0f10"
	if got := formatUUID(b); got != want {
		t.Fatalf("formatUUID = %q, want %q", got, want)
	}
	if got := stringValue(map[string]any{"id": b}, "id"); got != want {
		t.Fatalf("stringValue = %q, want %q", got, want)
	}
	if got := normalizeValue(b); got != want {
		t.Fatalf("normalizeValue = %v, want %q", got, want)
	}
}
