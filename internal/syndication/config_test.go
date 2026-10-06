package syndication

import "testing"

func TestNormalizePaidType(t *testing.T) {
	cases := map[string]string{"": "", "free": "", "Free": "", "paid": "paid", "force_paid": "force_paid", "bogus": ""}
	for in, want := range cases {
		if got := NormalizePaidType(in); got != want {
			t.Fatalf("NormalizePaidType(%q) = %q, want %q", in, got, want)
		}
	}
	if Paid("") || !Paid("paid") {
		t.Fatal("Paid() mismatch")
	}
}

func TestDeviceVendorsDedup(t *testing.T) {
	downloads := map[string]DownloadEntry{"xmb9p": {}, "xmws4": {}}
	devices := map[string]DeviceSpec{
		"n67": {ID: "xmb9p", Vendor: "xiaomi"},
		"o62": {ID: "xmws4", Vendor: "xiaomi"},
	}
	if got := DeviceVendors(downloads, devices); got != "xiaomi" {
		t.Fatalf("got %q", got)
	}
}
